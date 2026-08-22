package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johndauphine/dmtx/internal/config"
	"github.com/johndauphine/dmtx/internal/profiles"
	"github.com/johndauphine/dmtx/internal/secrets"
)

func profileTestPaths(t *testing.T) (string, string) {
	t.Helper()
	directory := t.TempDir()
	secretsPath := filepath.Join(directory, "secrets.yaml")
	if err := secrets.Create(secretsPath, false); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(directory, "profiles.db"), secretsPath
}

func profileTestConfig() []byte {
	return []byte("source:\n  type: sqlite\n  database: source.db\ntarget:\n  type: sqlite\n  database: target.db\nmigration:\n  target_mode: drop_recreate\n")
}

func TestResumeProfilePreservesDMTOriginSyntax(t *testing.T) {
	request, _, dispatched := ParseRequest([]string{"resume", "--profile=prod"})
	if !dispatched {
		t.Fatal("DMT profile spelling was refused before the WebUI could apply its state default")
	}
	if request.ProfileName != "prod" || request.StatePath != "" || request.ConfigPath != "" {
		t.Fatalf("unexpected unresolved profile request: %+v", request)
	}
	request, _, dispatched = ParseRequest([]string{"resume", "--profile", "prod", "--state", "run.db"})
	if !dispatched {
		t.Fatal("profile resume with explicit state was refused")
	}
	if request.ProfileName != "prod" || request.StatePath != "run.db" || request.ConfigPath != "" {
		t.Fatalf("unexpected resume profile request: %+v", request)
	}
}

func TestProfileOriginLoadsIntoNormalConfigParser(t *testing.T) {
	profilesPath, secretsPath := profileTestPaths(t)
	store, err := profiles.OpenWithSecrets(profilesPath, secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save("prod", profileTestConfig()); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	data, origin, err := configurationDataAt(
		Request{Command: "resume", ProfileName: "prod"},
		profilesPath,
		secretsPath,
	)
	if err != nil {
		t.Fatal(err)
	}
	if origin != "profile prod" {
		t.Fatalf("origin = %q", origin)
	}
	if _, err := config.Parse(data); err != nil {
		t.Fatalf("profile bytes bypassed normal parser: %v", err)
	}
}

func TestProfileOriginRejectsAmbiguousSelection(t *testing.T) {
	_, _, err := configurationDataAt(
		Request{ConfigPath: "migration.yaml", ProfileName: "prod"},
		"ignored.db",
		"ignored.yaml",
	)
	if err == nil || !strings.Contains(err.Error(), "either configuration path or profile") {
		t.Fatalf("ambiguous origin error = %v", err)
	}
}

func TestProfileCommandSaveListDelete(t *testing.T) {
	profilesPath, secretsPath := profileTestPaths(t)
	open := func() (*profiles.Store, error) {
		return profiles.OpenWithSecrets(profilesPath, secretsPath)
	}
	configPath := filepath.Join(t.TempDir(), "migration.yaml")
	if err := os.WriteFile(configPath, profileTestConfig(), 0o600); err != nil {
		t.Fatal(err)
	}
	save := executeProfileWithStore(
		newOutcome("profile"),
		Request{Command: "profile", ProfileAction: "save", ProfileName: "prod", ConfigPath: configPath},
		open,
	)
	if save.ExitCode != Success {
		t.Fatalf("save outcome: %+v", save)
	}
	list := executeProfileWithStore(newOutcome("profile"), Request{Command: "profile", ProfileAction: "list"}, open)
	if list.ExitCode != Success || len(list.Messages) != 1 || list.Messages[0].Text != "prod" {
		t.Fatalf("list outcome: %+v", list)
	}
	deleteOutcome := executeProfileWithStore(
		newOutcome("profile"),
		Request{Command: "profile", ProfileAction: "delete", ProfileName: "prod"},
		open,
	)
	if deleteOutcome.ExitCode != Success {
		t.Fatalf("delete outcome: %+v", deleteOutcome)
	}
	if bytes.Contains([]byte(strings.Join(messageTexts(deleteOutcome), "\n")), []byte("password")) {
		t.Fatal("profile output exposed plaintext")
	}
}

func TestProfileExportIsRefusedUntilProtectedRoundTripExists(t *testing.T) {
	profilesPath, secretsPath := profileTestPaths(t)
	open := func() (*profiles.Store, error) {
		return profiles.OpenWithSecrets(profilesPath, secretsPath)
	}
	store, err := open()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save("prod", profileTestConfig()); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(t.TempDir(), "portable.yaml")
	if err := os.WriteFile(output, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := executeProfileWithStore(newOutcome("profile"), Request{
		Command: "profile", ProfileAction: "export", ProfileName: "prod", OutputPath: output,
	}, open)
	if result.ExitCode != ConfigurationError {
		t.Fatalf("export outcome: %+v", result)
	}
	if got := strings.Join(messageTexts(result), "\n"); !strings.Contains(got, "deferred") {
		t.Fatalf("export refusal = %q", got)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old" {
		t.Fatalf("deferred export wrote plaintext: %q", data)
	}
}

func messageTexts(outcome Outcome) []string {
	texts := make([]string, len(outcome.Messages))
	for index, message := range outcome.Messages {
		texts[index] = message.Text
	}
	return texts
}
