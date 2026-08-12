package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johndauphine/dmtx/internal/config"
)

func TestSetupWritesARestrictedValidatedSQLiteConfiguration(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.db")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "migration.yaml")
	setup := NewSetup(path)
	if got := setup.Prompt(); got.Step != "source_database" {
		t.Fatalf("first prompt = %+v", got)
	}
	setup.Input(source)
	setup.Input(filepath.Join(directory, "target.db"))
	setup.Input("upsert")
	setup.Input(path)
	got := setup.Input("yes")
	if !got.Done || got.Error != "" {
		t.Fatalf("completion = %+v", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := config.Parse(data)
	if err != nil {
		t.Fatalf("setup configuration did not parse: %v", err)
	}
	if parsed.Source.Type != "sqlite" || parsed.Target.Type != "sqlite" || parsed.Migration.TargetMode != "upsert" {
		t.Fatalf("unexpected setup configuration: %+v", parsed)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("configuration mode = %04o, want owner-only", info.Mode().Perm())
	}
}

func TestSetupRefusesUnsafeOrDestructiveAnswers(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.db")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "migration.yaml")
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	setup := NewSetup(path)
	if got := setup.Input(filepath.Join(directory, "missing.db")); got.Step != "source_database" || got.Error == "" {
		t.Fatalf("missing source answer = %+v", got)
	}
	setup.Input(source)
	setup.Input(source)
	setup.Input("drop_recreate")
	setup.Input(path)
	if got := setup.Input("yes"); got.Step != "confirm" || got.Error == "" {
		t.Fatalf("existing configuration answer = %+v", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep" {
		t.Fatalf("existing configuration changed to %q", data)
	}
}

func TestSetupCancellationDoesNotWriteConfiguration(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.db")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "migration.yaml")
	setup := NewSetup(path)
	setup.Input(source)
	setup.Input(filepath.Join(directory, "target.db"))
	setup.Input("drop_recreate")
	setup.Input(path)
	if got := setup.Input("no"); !got.Done || got.Text != "setup cancelled" {
		t.Fatalf("cancellation = %+v", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cancelled setup wrote configuration: %v", err)
	}
}

func TestSetupRejectsUnreadableOrNonregularSQLiteSourcesWithoutLeakingPaths(t *testing.T) {
	directory := t.TempDir()
	setup := NewSetup(filepath.Join(directory, "migration.yaml"))
	missing := filepath.Join(directory, "source-sentinel.db")
	if got := setup.Input(missing); got.Error != "source SQLite database is not readable" || strings.Contains(got.Error, "source-sentinel") {
		t.Fatalf("missing source validation = %+v", got)
	}
	if got := setup.Input(directory); got.Error != "source SQLite database must be a regular file" || strings.Contains(got.Error, directory) {
		t.Fatalf("nonregular source validation = %+v", got)
	}
}

func TestSetupFromProfileConfigurationSeedsSafeSQLiteDefaults(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.db")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	flow, err := newSetupFromConfig("saved.yaml", config.Config{
		Source:    config.Endpoint{Type: "sqlite", Database: source},
		Target:    config.Endpoint{Type: "sqlite", Database: filepath.Join(directory, "target.db")},
		Migration: config.Migration{TargetMode: "upsert"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if prompt := flow.Prompt(); prompt.Default != source || prompt.ConfigPath != "saved.yaml" {
		t.Fatalf("profile-backed SQLite prompt = %+v", prompt)
	}
	if prompt := flow.Input(""); prompt.Default != filepath.Join(directory, "target.db") {
		t.Fatalf("target default = %+v", prompt)
	}
	if prompt := flow.Input(""); prompt.Default != "upsert" {
		t.Fatalf("mode default = %+v", prompt)
	}
}

func TestProfileSetupUsesProfileNameAsNewOutputDefault(t *testing.T) {
	flow, err := newSetupForProfileData("saved", "", "", profileTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	prompt := flow.Prompt()
	if prompt.ConfigPath != "saved.yaml" || prompt.Step != "source_database" {
		t.Fatalf("profile setup prompt = %+v", prompt)
	}
}
