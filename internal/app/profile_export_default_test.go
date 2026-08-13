package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johndauphine/dmtx/internal/profiles"
)

func TestProfileExportOmittedOutputWritesPortableDefaultNotConfig(t *testing.T) {
	request, outcome, dispatched := ParseRequest([]string{
		"profile", "export", "prod", "--passphrase-file", "placeholder",
	})
	if !dispatched || outcome.ExitCode != Success {
		t.Fatalf("parse omitted-output export: dispatched=%v outcome=%+v", dispatched, outcome)
	}
	if request.OutputPath != "prod.dmtx-profile.json" || request.OutputPath == "config.yaml" {
		t.Fatalf("omitted export output = %q, want portable default", request.OutputPath)
	}

	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.yaml")
	originalConfig := profileTestConfig()
	if err := os.WriteFile(configPath, originalConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	passphrasePath := filepath.Join(directory, "passphrase")
	if err := os.WriteFile(passphrasePath, []byte("correct horse battery staple\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	profilesPath, secretsPath := profileTestPaths(t)
	open := func() (*profiles.Store, error) { return profiles.OpenWithSecrets(profilesPath, secretsPath) }
	store, err := open()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save("prod", originalConfig); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	request.OutputPath = filepath.Join(directory, request.OutputPath)
	request.PassphraseFile = passphrasePath
	exported := executeProfileWithStore(newOutcome("profile"), request, open)
	if exported.ExitCode != Success {
		t.Fatalf("export outcome: %+v", exported)
	}
	if len(exported.Messages) != 1 || !strings.Contains(exported.Messages[0].Text, request.OutputPath) {
		t.Fatalf("export message = %+v, want output path %q", exported.Messages, request.OutputPath)
	}
	if got, err := os.ReadFile(configPath); err != nil || !bytes.Equal(got, originalConfig) {
		t.Fatalf("config.yaml was changed by omitted-output export: %q, %v", got, err)
	}
	if data, err := os.ReadFile(request.OutputPath); err != nil || bytes.Equal(data, originalConfig) {
		t.Fatalf("portable output was not written as ciphertext: %q, %v", data, err)
	}
}

func TestDefaultProfileExportPathUsesSafeBoundedFallback(t *testing.T) {
	for _, name := range []string{"../config", `windows\\path`, ".", "..", strings.Repeat("x", 201)} {
		path := defaultProfileExportPath(name)
		if filepath.Base(path) != path || strings.Contains(path, "config.yaml") || !strings.HasSuffix(path, ".dmtx-profile.json") {
			t.Fatalf("default path for %q = %q, not a safe portable filename", name, path)
		}
	}
}
