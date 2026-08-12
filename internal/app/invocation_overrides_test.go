package app

import (
	"testing"

	"github.com/johndauphine/dmtx/internal/config"
)

func TestInvocationOverridesApplyOnlyToParsedConfiguration(t *testing.T) {
	cfg, err := config.Parse(profileTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := applyInvocationOverrides(&cfg, Request{
		SourceSchema:  "source_schema",
		TargetSchema:  "target_schema",
		Workers:       3,
		SkipPreflight: "source.connectivity,target.connectivity",
	}); err != nil {
		t.Fatal(err)
	}
	if cfg.Source.Schema != "source_schema" || cfg.Target.Schema != "target_schema" || cfg.Migration.Workers != 3 {
		t.Fatalf("overrides were not applied: %+v", cfg)
	}
	if got := cfg.Migration.Preflight.SkipChecks; len(got) != 2 || got[0] != "source.connectivity" || got[1] != "target.connectivity" {
		t.Fatalf("skip selectors = %#v", got)
	}
}

func TestInvocationOverridesRefuseAmbiguousSkipList(t *testing.T) {
	cfg := config.Config{}
	if err := applyInvocationOverrides(&cfg, Request{SkipPreflight: "source.connectivity, target.connectivity"}); err == nil {
		t.Fatal("whitespace in --skip-preflight list was accepted")
	}
}
