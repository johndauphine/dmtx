package app

import (
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/johndauphine/dmtx/internal/config"
)

// TestMSSQLSetupLiveTLS drives the production SQL Server guided-setup flow
// against the disposable TLS fixture. It deliberately creates only a
// temporary configuration and its companion secret directory; it does not
// change either fixture database.
func TestMSSQLSetupLiveTLS(t *testing.T) {
	const fixtureMessage = "set DMTX_TEST_MSSQL_DSN, DMTX_TEST_MSSQL_TARGET_DSN, and DMTX_TEST_MSSQL_CA to run SQL Server guided setup"
	for _, variable := range []string{"DMTX_TEST_MSSQL_DSN", "DMTX_TEST_MSSQL_TARGET_DSN", "DMTX_TEST_MSSQL_CA"} {
		if os.Getenv(variable) != "" {
			continue
		}
		if os.Getenv("DMTX_STAGE4_LIVE_REQUIRED") == "1" {
			t.Fatalf("%s", fixtureMessage)
		}
		t.Skip(fixtureMessage)
	}
	source := mssqlSetupLiveEndpoint(t, "DMTX_TEST_MSSQL_DSN")
	target := mssqlSetupLiveEndpoint(t, "DMTX_TEST_MSSQL_TARGET_DSN")
	caPath := os.Getenv("DMTX_TEST_MSSQL_CA")
	if source.TLSCAFile != caPath || target.TLSCAFile != caPath {
		t.Fatalf("fixture DSN certificate paths = %q, %q; want %q", source.TLSCAFile, target.TLSCAFile, caPath)
	}

	directory := t.TempDir()
	path := filepath.Join(directory, "migration.yaml")
	setup := NewMSSQLSetup(path)
	for _, answer := range mssqlSetupLiveAnswers(source, target, path) {
		prompt := setup.Input(answer)
		if prompt.Error != "" {
			t.Fatalf("guided setup rejected answer at %q: %+v", prompt.Step, prompt)
		}
	}
	if prompt := setup.Prompt(); !prompt.Done {
		t.Fatalf("guided setup did not complete: %+v", prompt)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), source.Password) || strings.Contains(string(data), target.Password) {
		t.Fatal("guided setup wrote a plaintext SQL Server password")
	}
	parsed, err := config.Parse(data)
	if err != nil {
		t.Fatalf("parse generated configuration: %v", err)
	}
	assertMSSQLSetupLiveEndpoint(t, parsed.Source, source, "source")
	assertMSSQLSetupLiveEndpoint(t, parsed.Target, target, "target")
	if runtime.GOOS != "windows" {
		for _, candidate := range []string{path, path + ".secrets"} {
			info, err := os.Stat(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm()&0o077 != 0 {
				t.Fatalf("%s permissions = %04o, want owner-only", candidate, info.Mode().Perm())
			}
		}
	}
	for name, want := range map[string]string{
		"source.password": source.Password + "\n",
		"target.password": target.Password + "\n",
	} {
		secretPath := filepath.Join(path+".secrets", name)
		secret, err := os.ReadFile(secretPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(secret) != want {
			t.Fatalf("%s contents differ", name)
		}
		info, err := os.Stat(secretPath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("%s permissions = %04o, want owner-only", name, info.Mode().Perm())
		}
	}

	// Production setup rejects an unreadable CA before it attempts the opener;
	// that validation failure must leave neither a configuration nor a secret
	// directory behind.
	failedPath := filepath.Join(directory, "failed.yaml")
	failed := NewMSSQLSetup(failedPath)
	for _, answer := range []string{
		source.Host, strconv.Itoa(source.Port), source.Database, source.User,
		source.Password, filepath.Join(directory, "missing-ca.pem"),
	} {
		prompt := failed.Input(answer)
		if prompt.Error != "" && prompt.Error != "source SQL Server TLS CA certificate could not be verified" {
			t.Fatalf("unexpected failed setup prompt: %+v", prompt)
		}
	}
	if prompt := failed.Prompt(); prompt.Error != "source SQL Server TLS CA certificate could not be verified" {
		t.Fatalf("missing CA did not fail safely: %+v", prompt)
	}
	for _, candidate := range []string{failedPath, failedPath + ".secrets"} {
		if _, err := os.Stat(candidate); !os.IsNotExist(err) {
			t.Fatalf("failed setup mutated %s: %v", candidate, err)
		}
	}
}

func mssqlSetupLiveEndpoint(t *testing.T, variable string) config.Endpoint {
	t.Helper()
	raw := os.Getenv(variable)
	if raw == "" {
		t.Fatalf("%s unexpectedly empty after fixture preflight", variable)
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "sqlserver" || parsed.User == nil {
		t.Fatalf("%s must be a SQL Server URI with credentials", variable)
	}
	password, ok := parsed.User.Password()
	if !ok || parsed.Hostname() == "" || parsed.User.Username() == "" || parsed.Query().Get("database") == "" {
		t.Fatalf("%s is incomplete", variable)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 {
		t.Fatalf("%s has invalid port", variable)
	}
	query := parsed.Query()
	if query.Get("encrypt") != "true" || query.Get("tlsmin") != "1.2" || query.Get("guid conversion") != "true" || query.Get("certificate") == "" {
		t.Fatalf("%s must require verified TLS 1.2 with guid conversion", variable)
	}
	return config.Endpoint{
		Type: "mssql", Host: parsed.Hostname(), Port: port,
		Database: query.Get("database"), User: parsed.User.Username(), Password: password,
		TLSCAFile: query.Get("certificate"),
	}
}

func mssqlSetupLiveAnswers(source, target config.Endpoint, path string) []string {
	return []string{
		source.Host, strconv.Itoa(source.Port), source.Database, source.User, source.Password, source.TLSCAFile,
		target.Host, strconv.Itoa(target.Port), target.Database, target.User, target.Password, target.TLSCAFile,
		"upsert", path, "yes",
	}
}

func assertMSSQLSetupLiveEndpoint(t *testing.T, got, want config.Endpoint, side string) {
	t.Helper()
	if got.Type != "mssql" || got.Host != want.Host || got.Port != want.Port ||
		got.Database != want.Database || got.User != want.User || got.TLSCAFile != want.TLSCAFile {
		t.Fatalf("%s generated endpoint = %+v, want connection fields %+v", side, got, want)
	}
	if !strings.HasPrefix(got.Password, "${file:") {
		t.Fatalf("%s generated password is not a file origin: %q", side, got.Password)
	}
}
