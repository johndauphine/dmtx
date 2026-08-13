package app

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johndauphine/dmtx/internal/config"
)

func TestMSSQLSetupUsesProtectedPasswordOriginsAndVerifiesBothEndpoints(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "migration.yaml")
	sourceCA := writeMSSQLTestCA(t, filepath.Join(directory, "source.pem"))
	targetCA := writeMSSQLTestCA(t, filepath.Join(directory, "target.pem"))
	var verified []config.Endpoint
	var opened []*sql.DB
	setup := newMSSQLSetup(path, func(_ context.Context, endpoint config.Endpoint) (*sql.DB, error) {
		verified = append(verified, endpoint)
		database, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			return nil, err
		}
		opened = append(opened, database)
		return database, nil
	})
	answers := []string{
		"source.test", "", "source_db", "source_user", "source-sentinel-password", sourceCA,
		"target.test", "1444", "target_db", "target_user", "target-sentinel-password", targetCA,
		"upsert", path, "yes",
	}
	for index, answer := range answers {
		prompt := setup.Input(answer)
		if index == 3 && (!prompt.Masked || prompt.Step != "source_password") {
			t.Fatalf("source password prompt = %+v", prompt)
		}
		if index == 9 && (!prompt.Masked || prompt.Step != "target_password") {
			t.Fatalf("target password prompt = %+v", prompt)
		}
		if strings.Contains(prompt.Text+prompt.Default+prompt.Error, "sentinel-password") {
			t.Fatalf("password appeared in prompt: %+v", prompt)
		}
	}
	if len(verified) != 2 {
		t.Fatalf("verification calls = %d, want source and target", len(verified))
	}
	for index, database := range opened {
		if err := database.Ping(); err == nil {
			t.Fatalf("verified database %d was not closed", index)
		}
	}
	if got := verified[0]; got.Type != "mssql" || got.Port != 1433 || got.TLSCAFile != sourceCA {
		t.Fatalf("source verification endpoint = %+v", got)
	}
	if got := verified[1]; got.Type != "mssql" || got.Port != 1444 || got.TLSCAFile != targetCA {
		t.Fatalf("target verification endpoint = %+v", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sentinel-password") {
		t.Fatal("configuration contains a plaintext password")
	}
	parsed, err := config.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Source.Type != "mssql" || parsed.Target.Type != "mssql" ||
		parsed.Source.TLSCAFile != sourceCA || parsed.Target.TLSCAFile != targetCA {
		t.Fatalf("written SQL Server configuration = %+v", parsed)
	}
	if !strings.HasPrefix(parsed.Source.Password, "${file:") || !strings.HasPrefix(parsed.Target.Password, "${file:") {
		t.Fatalf("password origins = %q, %q", parsed.Source.Password, parsed.Target.Password)
	}
	secretsDirectory := path + ".secrets"
	info, err := os.Stat(secretsDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("secret directory mode = %04o", info.Mode().Perm())
	}
	for name, want := range map[string]string{"source.password": "source-sentinel-password\n", "target.password": "target-sentinel-password\n"} {
		secretPath := filepath.Join(secretsDirectory, name)
		secret, err := os.ReadFile(secretPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(secret) != want {
			t.Fatalf("%s contents = %q", name, secret)
		}
		info, err := os.Stat(secretPath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("%s mode = %04o", name, info.Mode().Perm())
		}
	}
}

func TestMSSQLSetupAliasesAndDefaults(t *testing.T) {
	for _, name := range []string{"mssql", "sqlserver", "sql-server"} {
		flow, err := NewSetupForEngine("", name)
		if err != nil {
			t.Fatalf("NewSetupForEngine(%q): %v", name, err)
		}
		setup, ok := flow.(*MSSQLSetup)
		if !ok {
			t.Fatalf("NewSetupForEngine(%q) = %T, want *MSSQLSetup", name, flow)
		}
		if prompt := setup.Prompt(); prompt.Step != "source_host" || prompt.Default != "localhost" {
			t.Fatalf("source host default = %+v", prompt)
		}
		if prompt := setup.Input(""); prompt.Step != "source_port" || prompt.Default != "1433" {
			t.Fatalf("source port default = %+v", prompt)
		}
	}
	_, err := NewSetupForEngine("", "oracle")
	if got := SetupStartErrorMessage(err); got != "unsupported setup engine; choose sqlite, postgres, or sqlserver" {
		t.Fatalf("unsupported engine message = %q", got)
	}
}

func TestMSSQLSetupRejectsInvalidSourceAndTargetPorts(t *testing.T) {
	for _, value := range []string{"0", "65536", "-1", "not-a-port"} {
		t.Run("source_"+value, func(t *testing.T) {
			setup := newMSSQLSetup("migration.yaml", func(context.Context, config.Endpoint) (*sql.DB, error) { return nil, nil })
			setup.Input("source.test")
			prompt := setup.Input(value)
			if prompt.Step != "source_port" || prompt.Error != "source SQL Server port must be a number from 1 to 65535" {
				t.Fatalf("source port %q prompt = %+v", value, prompt)
			}
		})
		t.Run("target_"+value, func(t *testing.T) {
			setup := newMSSQLSetup("migration.yaml", func(context.Context, config.Endpoint) (*sql.DB, error) { return nil, nil })
			for _, answer := range []string{"source.test", "1433", "source", "reader", "source-password", "", "target.test"} {
				setup.Input(answer)
			}
			prompt := setup.Input(value)
			if prompt.Step != "target_port" || prompt.Error != "target SQL Server port must be a number from 1 to 65535" {
				t.Fatalf("target port %q prompt = %+v", value, prompt)
			}
		})
	}
}

func TestMSSQLSetupFromProfileNeverReflectsPassword(t *testing.T) {
	setup, err := newMSSQLSetupFromConfig("saved.yaml", config.Config{
		Source:    config.Endpoint{Type: "mssql", Host: "source.example", Port: 1434, Database: "source", User: "reader", Password: "source-sentinel", TLSCAFile: "/ca/source.pem"},
		Target:    config.Endpoint{Type: "mssql", Host: "target.example", Port: 1435, Database: "target", User: "writer", Password: "target-sentinel", TLSCAFile: "/ca/target.pem"},
		Migration: config.Migration{TargetMode: "upsert"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, prompt := range []SetupPrompt{setup.Prompt(), setup.Input(""), setup.Input(""), setup.Input(""), setup.Input("")} {
		if strings.Contains(prompt.Text+prompt.Default+prompt.Error, "sentinel") {
			t.Fatalf("profile password reached a prompt: %+v", prompt)
		}
	}
	if prompt := setup.Prompt(); !prompt.Masked || prompt.Step != "source_password" {
		t.Fatalf("source password must still be freshly requested: %+v", prompt)
	}
	if prompt := setup.Input("fresh-password"); prompt.Step != "source_tls_ca_file" || prompt.Default != "/ca/source.pem" {
		t.Fatalf("source TLS prompt = %+v", prompt)
	}
}

func TestMSSQLSetupVerificationIsBoundedAndRedactsFailures(t *testing.T) {
	setup := newMSSQLSetup("migration.yaml", func(ctx context.Context, endpoint config.Endpoint) (*sql.DB, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > mssqlSetupVerificationTimeout {
			t.Fatalf("verification deadline = %v, %v", deadline, ok)
		}
		if endpoint.Type != "mssql" {
			t.Fatalf("endpoint type = %q", endpoint.Type)
		}
		return nil, errors.New("source-sentinel-password and /private/path")
	})
	setup.Input("source.test")
	setup.Input("1433")
	setup.Input("source_db")
	setup.Input("source_user")
	prompt := setup.Input("source-sentinel-password")
	if prompt.Step != "source_tls_ca_file" || prompt.Error != "" {
		t.Fatalf("password step = %+v", prompt)
	}
	prompt = setup.Input("")
	if prompt.Error != "source SQL Server connection could not be verified" {
		t.Fatalf("connection error = %+v", prompt)
	}
	if strings.Contains(prompt.Error+prompt.Text, "sentinel") || strings.Contains(prompt.Error+prompt.Text, "/private") {
		t.Fatalf("private failure was exposed: %+v", prompt)
	}

	timedOut := newMSSQLSetup("migration.yaml", func(ctx context.Context, _ config.Endpoint) (*sql.DB, error) {
		<-ctx.Done()
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("verification context error = %v", ctx.Err())
		}
		return nil, ctx.Err()
	})
	timedOut.timeout = 0
	timedOut.Input("source.test")
	timedOut.Input("1433")
	timedOut.Input("source_db")
	timedOut.Input("source_user")
	timedOut.Input("source-sentinel-password")
	prompt = timedOut.Input("")
	if prompt.Error != "source SQL Server connection could not be verified" {
		t.Fatalf("timeout error = %+v", prompt)
	}
}

func TestMSSQLSetupRedactsTLSCAValidationFailuresBeforeConnection(t *testing.T) {
	called := false
	setup := newMSSQLSetup("migration.yaml", func(context.Context, config.Endpoint) (*sql.DB, error) {
		called = true
		return nil, nil
	})
	setup.Input("source.test")
	setup.Input("1433")
	setup.Input("source_db")
	setup.Input("source_user")
	setup.Input("source-sentinel-password")
	prompt := setup.Input("/private/source-sentinel.txt")
	if prompt.Error != "source SQL Server TLS CA certificate could not be verified" {
		t.Fatalf("TLS CA error = %+v", prompt)
	}
	if called || strings.Contains(prompt.Error+prompt.Text, "private") || strings.Contains(prompt.Error+prompt.Text, "sentinel") {
		t.Fatalf("TLS CA failure leaked or verified early: called=%v prompt=%+v", called, prompt)
	}
}

func TestMSSQLSetupTLSCAValidationRejectsUnsafeAndMalformedFiles(t *testing.T) {
	directory := t.TempDir()
	valid := writeMSSQLTestCA(t, filepath.Join(directory, "valid.pem"))
	tests := []struct {
		name string
		path string
	}{
		{name: "directory", path: directory},
		{name: "oversized", path: filepath.Join(directory, "oversized.pem")},
		{name: "malformed PEM", path: filepath.Join(directory, "malformed.pem")},
		{name: "malformed DER", path: filepath.Join(directory, "malformed.der")},
	}
	if err := os.WriteFile(tests[1].path, make([]byte, maxMSSQLSetupTLSCAFile+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tests[2].path, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tests[3].path, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if link := filepath.Join(directory, "link.pem"); os.Symlink(valid, link) == nil {
		tests = append(tests, struct {
			name string
			path string
		}{name: "symlink", path: link})
	}
	if info, err := os.Lstat(os.DevNull); err == nil && !info.Mode().IsRegular() {
		tests = append(tests, struct {
			name string
			path string
		}{name: "device", path: os.DevNull})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateMSSQLTLSCAFile(test.path); err == nil {
				t.Fatalf("validateMSSQLTLSCAFile(%q) accepted unsafe input", test.path)
			}
		})
	}
}

func TestMSSQLSetupConnectionFailuresReturnToMaskedPasswordAndKeepCA(t *testing.T) {
	directory := t.TempDir()
	caPath := writeMSSQLTestCA(t, filepath.Join(directory, "ca.pem"))
	setup := newMSSQLSetup(filepath.Join(directory, "migration.yaml"), func(_ context.Context, endpoint config.Endpoint) (*sql.DB, error) {
		if endpoint.Password == "wrong-source" || endpoint.Password == "wrong-target" {
			return nil, errors.New("credential-sentinel")
		}
		return nil, nil
	})
	for _, answer := range []string{"source.test", "1433", "source", "reader", "wrong-source", caPath} {
		setup.Input(answer)
	}
	prompt := setup.Prompt()
	if prompt.Step != "source_password" || !prompt.Masked || prompt.Error != "source SQL Server connection could not be verified" {
		t.Fatalf("source retry prompt = %+v", prompt)
	}
	prompt = setup.Input("correct-source")
	if prompt.Step != "source_tls_ca_file" || prompt.Default != caPath {
		t.Fatalf("source retry CA prompt = %+v", prompt)
	}
	if prompt = setup.Input(""); prompt.Step != "target_host" || prompt.Error != "" {
		t.Fatalf("source retry did not verify: %+v", prompt)
	}
	for _, answer := range []string{"target.test", "1433", "target", "writer", "wrong-target", caPath} {
		setup.Input(answer)
	}
	prompt = setup.Prompt()
	if prompt.Step != "target_password" || !prompt.Masked || prompt.Error != "target SQL Server connection could not be verified" {
		t.Fatalf("target retry prompt = %+v", prompt)
	}
	prompt = setup.Input("correct-target")
	if prompt.Step != "target_tls_ca_file" || prompt.Default != caPath {
		t.Fatalf("target retry CA prompt = %+v", prompt)
	}
	if prompt = setup.Input(""); prompt.Step != "target_mode" || prompt.Error != "" {
		t.Fatalf("target retry did not verify: %+v", prompt)
	}
}

func TestMSSQLSetupCancellationAndOverwriteDoNotWriteConfiguration(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "migration.yaml")
	setup := newMSSQLSetup(path, func(context.Context, config.Endpoint) (*sql.DB, error) { return nil, nil })
	for _, answer := range []string{
		"source.test", "1433", "source", "reader", "source-password", "",
		"target.test", "1433", "target", "writer", "target-password", "",
		"drop_recreate", path,
	} {
		setup.Input(answer)
	}
	if prompt := setup.Input("no"); !prompt.Done || prompt.Text != "setup cancelled" {
		t.Fatalf("cancel prompt = %+v", prompt)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled setup wrote configuration: %v", err)
	}
	if _, err := os.Stat(path + ".secrets"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled setup wrote secrets: %v", err)
	}

	if err := os.WriteFile(path, []byte("existing-sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	overwrite := newMSSQLSetup(path, func(context.Context, config.Endpoint) (*sql.DB, error) { return nil, nil })
	for _, answer := range []string{
		"source.test", "1433", "source", "reader", "source-password", "",
		"target.test", "1433", "target", "writer", "target-password", "",
		"drop_recreate", path,
	} {
		overwrite.Input(answer)
	}
	prompt := overwrite.Input("yes")
	if prompt.Error != "configuration already exists; choose another path" {
		t.Fatalf("overwrite prompt = %+v", prompt)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "existing-sentinel" {
		t.Fatalf("existing configuration changed: %q, %v", data, err)
	}
}

func TestMSSQLSetupRefusesSamePhysicalEndpointBeforePersistence(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "migration.yaml")
	setup := newMSSQLSetup(path, func(context.Context, config.Endpoint) (*sql.DB, error) { return nil, nil })
	for _, answer := range []string{
		"DB.EXAMPLE", "", "same_database", "reader", "source-password", "",
		"db.example", "1433", "same_database", "writer", "target-password", "",
		"upsert", path,
	} {
		setup.Input(answer)
	}
	prompt := setup.Input("yes")
	if prompt.Step != "confirm" || prompt.Error != "generated configuration is invalid" {
		t.Fatalf("same endpoint prompt = %+v", prompt)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("same endpoint wrote configuration: %v", err)
	}
	if _, err := os.Stat(path + ".secrets"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("same endpoint wrote secrets: %v", err)
	}
}

func TestMSSQLSetupRefusesDanglingConfigurationSymlink(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "migration.yaml")
	target := filepath.Join(directory, "unintended.yaml")
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	setup := newMSSQLSetup(path, func(context.Context, config.Endpoint) (*sql.DB, error) { return nil, nil })
	for _, answer := range []string{
		"source.test", "1433", "source", "reader", "source-password", "",
		"target.test", "1433", "target", "writer", "target-password", "",
		"drop_recreate", path,
	} {
		setup.Input(answer)
	}
	prompt := setup.Input("yes")
	if prompt.Error != "configuration already exists; choose another path" {
		t.Fatalf("dangling symlink prompt = %+v", prompt)
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("configuration symlink changed: info=%v err=%v", info, err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("setup created symlink target: %v", err)
	}
	if _, err := os.Stat(path + ".secrets"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("setup left secret directory after refusal: %v", err)
	}
}

func writeMSSQLTestCA(t *testing.T, path string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          new(big.Int).SetInt64(1),
		Subject:               pkix.Name{CommonName: "DMTX test CA"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
