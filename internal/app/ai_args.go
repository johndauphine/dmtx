package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func parseAIArguments(args []string) (Request, error) {
	if len(args) < 1 || (args[0] != "config-review" && args[0] != "runbook") {
		return Request{}, fmt.Errorf(
			"/ai: expected config-review or its runbook alias (see /help)",
		)
	}

	remaining := args[1:]
	requestText := ""
	for index, argument := range remaining {
		name, inlineValue, hasInlineValue := strings.Cut(argument, "=")
		if name != "--request" {
			continue
		}
		if hasInlineValue {
			requestText = strings.Join(
				append([]string{inlineValue}, remaining[index+1:]...),
				" ",
			)
		} else {
			if index+1 >= len(remaining) {
				return Request{}, fmt.Errorf(
					"/ai %s: flag --request requires a value (see /help)",
					args[0],
				)
			}
			requestText = strings.Join(remaining[index+1:], " ")
		}
		// DMT treats --request as free text and consumes the rest of the line.
		// Anything after it, even something beginning with --, is request text.
		remaining = remaining[:index]
		break
	}

	values := originArgumentValues()
	values["--timeout"] = "timeout"
	parsed, err := parseCommandArguments(commandArgumentSpec{
		command: "ai " + args[0],
		values:  values,
	}, remaining)
	if err != nil {
		return Request{}, err
	}
	configPath, profileName, err := resolveConfigOrigin("ai "+args[0], parsed)
	if err != nil {
		return Request{}, err
	}
	timeout, err := parseAITimeout(parsed.values["timeout"])
	if err != nil {
		return Request{}, fmt.Errorf("/ai %s: %w", args[0], err)
	}
	return Request{
		Command:     "ai",
		AIAction:    "config-review",
		ConfigPath:  configPath,
		ProfileName: profileName,
		AIRequest:   requestText,
		AITimeout:   timeout,
	}, nil
}

// parseAITimeout keeps DMTX's existing integer-seconds spelling while also
// accepting DMT's Go-duration spelling. Request carries whole seconds, so a
// positive sub-second duration is rounded up rather than becoming the zero
// value (which means "use the default").
func parseAITimeout(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 1 || seconds > 600 {
			return 0, fmt.Errorf("--timeout must be between 1s and 10m")
		}
		return seconds, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 || duration > 10*time.Minute {
		return 0, fmt.Errorf(
			"invalid --timeout %q (use Go duration syntax, e.g. 90s, up to 10m)",
			value,
		)
	}
	return int((duration + time.Second - 1) / time.Second), nil
}

func aiArguments(args []string) (Request, bool) {
	request, err := parseAIArguments(args)
	return request, err == nil
}
