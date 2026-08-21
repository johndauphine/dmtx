package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/johndauphine/dmtx/internal/config"
	"github.com/johndauphine/dmtx/internal/contract"
)

type stage6PublicContract struct {
	Major        int               `json:"major"`
	Commands     []string          `json:"commands"`
	Aliases      map[string]string `json:"aliases"`
	ExitCodes    map[string]int    `json:"exit_codes"`
	Deprecations []struct {
		Field          string `json:"field"`
		Replacement    string `json:"replacement"`
		RemovalVersion string `json:"removal_version"`
	} `json:"deprecations"`
}

func TestStage6PublicCompatibilityFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/stage6/public-contract-v5.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture stage6PublicContract
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Major != 5 {
		t.Fatalf("compatibility fixture major = %d", fixture.Major)
	}

	for _, name := range fixture.Commands {
		command, found := contract.Resolve(name)
		if !found || command.Name != name {
			t.Fatalf("stable command %q is missing or resolves to %#v", name, command)
		}
	}
	for alias, canonical := range fixture.Aliases {
		command, found := contract.Resolve(alias)
		if !found || command.Name != canonical {
			t.Fatalf("stable alias %q resolves to %#v, want %q", alias, command, canonical)
		}
	}

	wantExitCodes := map[string]int{
		"success":       Success,
		"configuration": ConfigurationError,
		"connection":    ConnectionError,
		"transfer":      TransferError,
		"validation":    ValidationError,
		"cancelled":     Cancelled,
		"state":         StateError,
		"file":          FileError,
	}
	if !reflect.DeepEqual(fixture.ExitCodes, wantExitCodes) {
		t.Fatalf("public exit codes = %#v, want %#v", fixture.ExitCodes, wantExitCodes)
	}

	legacy, err := config.Parse([]byte("migration:\n  schema_evolution: report\n  ai_adjust: false\n  ai_adjust_interval: 11s\n"))
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := legacy.Diagnostics()
	sort.Slice(diagnostics, func(left, right int) bool {
		return diagnostics[left].Field < diagnostics[right].Field
	})
	sort.Slice(fixture.Deprecations, func(left, right int) bool {
		return fixture.Deprecations[left].Field < fixture.Deprecations[right].Field
	})
	if len(diagnostics) != len(fixture.Deprecations) {
		t.Fatalf("deprecation diagnostics = %#v", diagnostics)
	}
	for index, expected := range fixture.Deprecations {
		actual := diagnostics[index]
		if actual.Severity != config.ConfigDiagnosticWarning ||
			actual.Code != config.ConfigDiagnosticDeprecatedField ||
			actual.Field != expected.Field ||
			actual.Replacement != expected.Replacement ||
			actual.RemovalVersion != expected.RemovalVersion {
			t.Fatalf("diagnostic[%d] = %#v, want %#v", index, actual, expected)
		}
	}
}

func TestStage6PublicJSONReadersTolerateAdditiveFields(t *testing.T) {
	var decoded struct {
		ExitCode int `json:"exit_code"`
	}
	if err := json.Unmarshal(
		[]byte(`{"exit_code":0,"future_additive_field":{"nested":true}}`),
		&decoded,
	); err != nil {
		t.Fatalf("additive public JSON field was rejected: %v", err)
	}
	if decoded.ExitCode != Success {
		t.Fatalf("decoded exit code = %d", decoded.ExitCode)
	}
}

func TestStage6DeprecatedFieldsWarnThroughEveryApplicationSurface(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.yaml")
	if err := os.WriteFile(path, []byte("migration:\n  ai_adjust: false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outcome := Execute(context.Background(), Request{Command: "config", ConfigPath: path})
	if outcome.ExitCode != Success {
		t.Fatalf("config exit = %d, messages = %#v", outcome.ExitCode, outcome.Messages)
	}
	want := "warning: migration.ai_adjust is deprecated; use migration.runtime_tuning; removal is scheduled for version 6"
	if len(outcome.Messages) == 0 || outcome.Messages[0] != (Message{Stream: StreamStderr, Text: want}) {
		t.Fatalf("deprecation warning = %#v, want %q", outcome.Messages, want)
	}
}
