package app

import (
	"fmt"
	"strings"

	"github.com/johndauphine/dmtx/internal/config"
)

// applyInvocationOverrides applies DMT's per-command configuration overrides
// to the parsed in-memory configuration. The source file/profile is never
// modified: the override belongs to this execution, just as it does in DMT's
// interactive command surface.
func applyInvocationOverrides(cfg *config.Config, request Request) error {
	if request.SourceSchema != "" {
		cfg.Source.Schema = request.SourceSchema
	}
	if request.TargetSchema != "" {
		cfg.Target.Schema = request.TargetSchema
	}
	if request.Workers != 0 {
		if request.Workers < 1 {
			return fmt.Errorf("--workers requires a positive integer")
		}
		cfg.Migration.Workers = request.Workers
	}
	if request.SkipPreflight != "" {
		selectors, err := preflightSelectors(request.SkipPreflight)
		if err != nil {
			return err
		}
		cfg.Migration.Preflight.SkipChecks = selectors
	}
	return nil
}

// preflightSelectors accepts DMT's LIST spelling while retaining DMTX's
// exact, validated selector model. A comma-separated list is expanded before
// configuration validation; surrounding whitespace and empty members are
// rejected instead of being silently changed.
func preflightSelectors(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	for _, part := range parts {
		if part == "" || strings.TrimSpace(part) != part {
			return nil, fmt.Errorf("--skip-preflight requires comma-separated selectors without whitespace")
		}
	}
	return parts, nil
}
