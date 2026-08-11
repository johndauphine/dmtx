package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johndauphine/dmtx/internal/config"
	"github.com/johndauphine/dmtx/internal/secrets"
)

func TestAIArgumentsAcceptOnlyConfigReview(t *testing.T) {
	request, ok := aiArguments([]string{"config-review", "--config", "migration.yaml", "--timeout", "12"})
	if !ok {
		t.Fatal("config-review arguments were rejected")
	}
	if request.AIAction != "config-review" || request.ConfigPath != "migration.yaml" || request.AITimeout != 12 {
		t.Fatalf("unexpected request: %+v", request)
	}

	if _, ok := aiArguments([]string{"runbook", "--config", "migration.yaml"}); ok {
		t.Fatal("unsupported AI runbook action was accepted")
	}
}

func TestBuildAIAdvisoryPromptUsesDMTXSchema(t *testing.T) {
	prompt, err := buildAIAdvisoryPrompt("file", config.Config{}, Request{AIRequest: "benign review"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "DMTX's display-only advisory schema") {
		t.Fatal("prompt did not identify the DMTX advisory contract")
	}
	if !strings.Contains(prompt, "matching {\"summary\":string") {
		t.Fatal("prompt did not include the JSON schema example")
	}
	if strings.Contains(prompt, string(rune(92))) {
		t.Fatal("prompt schema contains unexpected escape characters")
	}
}

func TestBuildAIAdvisoryPromptOmitsConnectionValues(t *testing.T) {
	cfg := config.Config{
		Source: config.Endpoint{
			Type: "postgres", Host: "source.internal", Port: 5432,
			Database: "source_finance", User: "source_operator", Password: "source-password",
			Schema: "source_schema", SSLMode: "verify-full", TLSCAFile: "/private/source-ca.pem",
		},
		Target: config.Endpoint{
			Type: "mysql", Host: "target.internal", Port: 3306,
			Database: "target_finance", User: "target_operator", Password: "target-password",
			Schema: "target_schema", SSLMode: "verify-ca", TLSCAFile: "/private/target-ca.pem",
		},
	}
	prompt, err := buildAIAdvisoryPrompt("file", cfg, Request{AIRequest: "benign review"})
	if err != nil {
		t.Fatal(err)
	}
	for _, sensitive := range []string{
		"source.internal", "5432", "source_finance", "source_operator", "source-password", "/private/source-ca.pem",
		"target.internal", "3306", "target_finance", "target_operator", "target-password", "/private/target-ca.pem",
	} {
		if strings.Contains(prompt, sensitive) {
			t.Fatalf("prompt leaked connection value %q", sensitive)
		}
	}
	for _, allowed := range []string{"postgres", "mysql", "source_schema", "target_schema"} {
		if !strings.Contains(prompt, allowed) {
			t.Fatalf("prompt omitted safe structural fact %q", allowed)
		}
	}
}

func TestExecuteAIWithMockedOpenAIProducesAdvisory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %s", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer mock-openai-key" {
			t.Fatalf("authorization header = %q", request.Header.Get("Authorization"))
		}
		response := map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]string{
						"content": strings.Repeat(string(rune(96)), 3) + "json\n{\"summary\":\"mocked advisory\"}\n" + strings.Repeat(string(rune(96)), 3),
					},
				},
			},
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(response)
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "migration.yaml")
	configuration := "source:\n  type: sqlite\n  database: source.db\ntarget:\n  type: sqlite\n  database: target.db\nai:\n  provider: openai\n  protocol: openai\n  base_url: " + server.URL + "\n  model: gpt-5.6-luna\n  api_key: " + string(rune(36)) + "{secret:openai}\n  max_tokens: 256\n  max_requests: 1\n"
	if err := os.WriteFile(path, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}

	global := secrets.Config{
		AI: secrets.AIConfig{
			DefaultProvider: "openai",
			Providers: map[string]*secrets.Provider{
				"openai": {Protocol: "openai", APIKey: "mock-openai-key", Model: "gpt-5.6-luna", MaxTokens: 256, MaxRequests: 1},
			},
		},
	}
	outcome := executeAIWith(
		context.Background(),
		Request{Command: "ai", AIAction: "config-review", ConfigPath: path, AIRequest: "summarize safely"},
		func() (secrets.Config, error) { return global, nil },
	)
	if outcome.ExitCode != Success {
		t.Fatalf("exit code = %d, messages = %v", outcome.ExitCode, outcome.Messages)
	}
	if outcome.Payload == nil || outcome.Payload.Kind != PayloadAIAdvisory {
		t.Fatalf("payload = %+v", outcome.Payload)
	}
	var payload aiAdvisoryPayload
	if err := json.Unmarshal(outcome.Payload.Data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != "ok" || payload.Provider != "openai" || payload.Model != "gpt-5.6-luna" || payload.Advisory.Summary != "mocked advisory" {
		t.Fatalf("advisory payload = %+v", payload)
	}
}

func TestExecuteAIWithMockedInvalidResponseSurfacesSafeClass(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		response := map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]string{
						"content": "{\"summary\":\"ok\",\"patch_recommendations\":[]}",
					},
				},
			},
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(response)
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "migration.yaml")
	configuration := "source:\n  type: sqlite\n  database: source.db\ntarget:\n  type: sqlite\n  database: target.db\nai:\n  provider: lmstudio\n  base_url: " + server.URL + "\n  model: qwen-local\n"
	if err := os.WriteFile(path, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}
	outcome := executeAIWith(
		context.Background(),
		Request{Command: "ai", AIAction: "config-review", ConfigPath: path},
		func() (secrets.Config, error) { return secrets.Config{}, nil },
	)
	if outcome.ExitCode != ConfigurationError {
		t.Fatalf("exit code = %d, messages = %v", outcome.ExitCode, outcome.Messages)
	}
	if outcome.Payload == nil || outcome.Payload.Kind != PayloadAIAdvisory {
		t.Fatalf("payload = %+v", outcome.Payload)
	}
	var payload aiAdvisoryPayload
	if err := json.Unmarshal(outcome.Payload.Data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != "invalid_response" || payload.Provider != "lmstudio" || payload.Model != "qwen-local" || payload.Error != "response_schema_validation" {
		t.Fatalf("safe parse payload = %+v", payload)
	}
	if strings.Contains(saidBy(outcome), "patch_recommendations") {
		t.Fatal("response content leaked into CLI messages")
	}
	if !strings.Contains(saidBy(outcome), "response schema_validation") {
		t.Fatalf("safe parse class was not surfaced: %v", outcome.Messages)
	}
}

func TestExecuteAIMissingSecretsIsAdvisoryUnavailable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migration.yaml")
	configuration := "source:\n  type: sqlite\n  database: source.db\ntarget:\n  type: sqlite\n  database: target.db\n"
	if err := os.WriteFile(path, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}

	outcome := executeAIWith(
		context.Background(),
		Request{Command: "ai", AIAction: "config-review", ConfigPath: path},
		func() (secrets.Config, error) { return secrets.Config{}, os.ErrNotExist },
	)
	if outcome.ExitCode != Success || !strings.Contains(saidBy(outcome), "no protected provider configured") {
		t.Fatalf("missing secrets outcome = code %d, messages %v", outcome.ExitCode, outcome.Messages)
	}
}

func TestExecuteAISecretsLoadFailureFailsWithoutLeakingError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migration.yaml")
	configuration := "source:\n  type: sqlite\n  database: source.db\ntarget:\n  type: sqlite\n  database: target.db\n"
	if err := os.WriteFile(path, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, loadErr := range []error{
		errors.New("parse secrets: api-key-sentinel"),
		os.ErrPermission,
	} {
		outcome := executeAIWith(
			context.Background(),
			Request{Command: "ai", AIAction: "config-review", ConfigPath: path},
			func() (secrets.Config, error) { return secrets.Config{}, loadErr },
		)
		if outcome.ExitCode != FileError {
			t.Fatalf("secrets load error %v returned code %d", loadErr, outcome.ExitCode)
		}
		message := saidBy(outcome)
		if !strings.Contains(message, "AI secrets could not be loaded") {
			t.Fatalf("secrets load error was not surfaced safely: %v", outcome.Messages)
		}
		if strings.Contains(message, "api-key-sentinel") || strings.Contains(message, "permission denied") {
			t.Fatalf("secrets load error leaked details: %v", outcome.Messages)
		}
	}
}

func TestExecuteAIHonorsRequestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		time.Sleep(1100 * time.Millisecond)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte("{\"choices\":[{\"message\":{\"content\":\"{\\\"summary\\\":\\\"late\\\"}\"}}]}"))
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "migration.yaml")
	configuration := "source:\n  type: sqlite\n  database: source.db\ntarget:\n  type: sqlite\n  database: target.db\nai:\n  provider: lmstudio\n  base_url: " + server.URL + "\n  model: qwen-local\n"
	if err := os.WriteFile(path, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}

	outcome := executeAIWith(
		context.Background(),
		Request{Command: "ai", AIAction: "config-review", ConfigPath: path, AITimeout: 1},
		func() (secrets.Config, error) { return secrets.Config{}, nil },
	)
	if outcome.ExitCode != ConnectionError {
		t.Fatalf("exit code = %d, messages = %v", outcome.ExitCode, outcome.Messages)
	}
	if !strings.Contains(saidBy(outcome), "provider timeout") {
		t.Fatalf("timeout was not reported safely: %v", outcome.Messages)
	}
}
