package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/johndauphine/dmtx/internal/ai"
	"github.com/johndauphine/dmtx/internal/config"
	"github.com/johndauphine/dmtx/internal/secrets"
)

const PayloadAIAdvisory = "ai_advisory"

type aiAdvisoryPayload struct {
	Status   string      `json:"status"`
	Provider string      `json:"provider,omitempty"`
	Model    string      `json:"model,omitempty"`
	Advisory ai.Advisory `json:"advisory,omitempty"`
	Error    string      `json:"error,omitempty"`
}

type aiFacts struct {
	Source    endpointAIFacts `json:"source"`
	Target    endpointAIFacts `json:"target"`
	Migration struct {
		TargetMode string `json:"target_mode,omitempty"`
		Workers    int    `json:"workers,omitempty"`
		DryRun     bool   `json:"dry_run,omitempty"`
	} `json:"migration"`
}

type endpointAIFacts struct {
	Type   string `json:"type"`
	Schema string `json:"schema,omitempty"`
}

func executeAI(ctx context.Context, request Request) Outcome {
	return executeAIWith(ctx, request, func() (secrets.Config, error) {
		path, err := secrets.Path()
		if err != nil {
			return secrets.Config{}, err
		}
		return secrets.Load(path)
	})
}

func executeAIWith(ctx context.Context, request Request, load func() (secrets.Config, error)) Outcome {
	out := newOutcome(request.Command)
	if request.AITimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(request.AITimeout)*time.Second)
		defer cancel()
	}
	if request.AIAction != "config-review" {
		return out.failWith(ConfigurationError, "usage: dmtx ai config-review (--config migration.yaml | --profile NAME)")
	}
	data, origin, err := configurationData(request)
	if err != nil {
		return out.failWith(FileError, "read configuration: "+err.Error())
	}
	cfg, err := config.Parse(data)
	if err != nil {
		return out.failWith(ConfigurationError, "configuration: "+err.Error())
	}
	global, err := load()
	if err != nil {
		switch {
		case errors.Is(err, os.ErrNotExist):
			payload := aiAdvisoryPayload{Status: "unavailable", Error: "no protected AI provider configured"}
			_ = out.setPayload(PayloadAIAdvisory, payload)
			out.out("AI advisory unavailable: no protected provider configured")
			return out.done(Success)
		case errors.Is(err, secrets.ErrInsecurePermissions), errors.Is(err, secrets.ErrInsecureDirectory):
			return out.failWith(FileError, "AI secrets are not protected")
		default:
			return out.failWith(FileError, "AI secrets could not be loaded")
		}
	}
	client, err := ai.NewClient(cfg.AI, global)
	if err != nil {
		payload := aiAdvisoryPayload{Status: "unavailable", Error: "AI provider is unavailable"}
		_ = out.setPayload(PayloadAIAdvisory, payload)
		out.out("AI advisory unavailable: provider configuration is incomplete")
		return out.done(Success)
	}
	prompt, err := buildAIAdvisoryPrompt(origin, cfg, request)
	if err != nil {
		return out.failWith(ConfigurationError, "prepare AI advisory: "+err.Error())
	}
	text, err := client.Generate(ctx, prompt)
	if err != nil {
		status := "failed"
		if errors.Is(err, context.Canceled) {
			status = "cancelled"
		}
		payload := aiAdvisoryPayload{Status: status, Provider: client.ProviderName(), Model: client.Model(), Error: safeAIError(err)}
		_ = out.setPayload(PayloadAIAdvisory, payload)
		return out.failWith(CancelledOrConnection(status), "AI advisory unavailable: "+safeAIError(err))
	}
	advisory, err := ai.DecodeAdvisory(text)
	if err != nil {
		class := ai.ParseFailureClassOf(err)
		payload := aiAdvisoryPayload{
			Status:   "invalid_response",
			Provider: client.ProviderName(),
			Model:    client.Model(),
			Error:    "response_" + string(class),
		}
		if setErr := out.setPayload(PayloadAIAdvisory, payload); setErr != nil {
			return out.failWith(FileError, "write AI advisory: "+setErr.Error())
		}
		message := "AI advisory unavailable: response " + string(class)
		out.out(message)
		return out.failWith(ConfigurationError, message)
	}
	payload := aiAdvisoryPayload{Status: "ok", Provider: client.ProviderName(), Model: client.Model(), Advisory: advisory}
	if err := out.setPayload(PayloadAIAdvisory, payload); err != nil {
		return out.failWith(FileError, "write AI advisory: "+err.Error())
	}
	out.out("AI advisory: " + advisory.Summary)
	return out.done(Success)
}

func CancelledOrConnection(status string) int {
	if status == "cancelled" {
		return Cancelled
	}
	return ConnectionError
}

func safeAIError(err error) string {
	if err == nil {
		return "AI advisory failed"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "provider timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return "provider request failed"
}

func buildAIAdvisoryPrompt(origin string, cfg config.Config, request Request) (string, error) {
	facts := aiFacts{
		Source: endpointAIFacts{Type: cfg.Source.Type, Schema: cfg.Source.Schema},
		Target: endpointAIFacts{Type: cfg.Target.Type, Schema: cfg.Target.Schema},
	}
	facts.Migration.TargetMode = cfg.Migration.TargetMode
	facts.Migration.Workers = cfg.Migration.Workers
	facts.Migration.DryRun = request.DryRun
	encoded, err := json.Marshal(facts)
	if err != nil {
		return "", err
	}
	operatorRequest := strings.TrimSpace(request.AIRequest)
	if len(operatorRequest) > 4096 {
		return "", errors.New("operator request is too large")
	}
	return fmt.Sprintf("You provide DMTX advisory guidance only. Deterministic migration facts are authoritative. Never propose executing commands or changing configuration automatically. This is DMTX's display-only advisory schema, not DMT's richer config-review schema: do not emit patch_recommendations, runbook, commands, status, provider, model, or prompt metadata. Return exactly one JSON object, with no prose and no markdown fence, matching {\"summary\":string,\"findings\":[{\"category\":string,\"title\":string,\"summary\":string,\"action\":string}],\"warnings\":[string]}. Use empty arrays when there are no findings or warnings. Do not include credentials, secrets, raw SQL, rows, full logs, or file contents. Configuration origin kind: %s. Facts: %s. Operator request: %s", originKind(origin), encoded, operatorRequest), nil
}

func originKind(origin string) string {
	if strings.HasPrefix(origin, "profile ") {
		return "encrypted-profile"
	}
	return "file"
}
