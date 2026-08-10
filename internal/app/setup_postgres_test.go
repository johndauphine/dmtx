package app

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johndauphine/dmtx/internal/config"
)

func TestPostgresSetupUsesProtectedPasswordOrigins(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "migration.yaml")
	setup := newPostgresSetup(path, func(context.Context, config.Endpoint) (*sql.DB, error) {
		return nil, nil
	})
	answers := []string{
		"source.test", "5432", "source_db", "source_user", "source-sentinel-password",
		"target.test", "5433", "target_db", "target_user", "target-sentinel-password",
		"upsert", path, "yes",
	}
	for index, answer := range answers {
		prompt := setup.Input(answer)
		if index == 3 && (!prompt.Masked || prompt.Step != "source_password") {
			t.Fatalf("source password prompt = %+v", prompt)
		}
		if index == 8 && (!prompt.Masked || prompt.Step != "target_password") {
			t.Fatalf("target password prompt = %+v", prompt)
		}
		if strings.Contains(prompt.Text+prompt.Error, "sentinel-password") {
			t.Fatalf("password appeared in prompt: %+v", prompt)
		}
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

func TestPostgresSetupRedactsConnectionFailures(t *testing.T) {
	setup := newPostgresSetup("migration.yaml", func(context.Context, config.Endpoint) (*sql.DB, error) {
		return nil, errors.New("source-sentinel-password leaked by driver")
	})
	setup.Input("source.test")
	setup.Input("5432")
	setup.Input("source_db")
	prompt := setup.Input("source_user")
	if !prompt.Masked {
		t.Fatalf("password prompt = %+v", prompt)
	}
	prompt = setup.Input("source-sentinel-password")
	if prompt.Error != "source PostgreSQL connection could not be verified" {
		t.Fatalf("connection error = %+v", prompt)
	}
	if strings.Contains(prompt.Error+prompt.Text, "sentinel") {
		t.Fatalf("secret leaked in prompt: %+v", prompt)
	}
}

func TestPostgresSetupVerificationUsesBoundedContext(t *testing.T) {
	setup := newPostgresSetup("migration.yaml", func(ctx context.Context, endpoint config.Endpoint) (*sql.DB, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("verification context has no deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > postgresSetupVerificationTimeout {
			t.Fatalf("verification deadline has unexpected remaining duration %s", remaining)
		}
		return nil, nil
	})
	setup.Input("source.test")
	setup.Input("5432")
	setup.Input("source_db")
	setup.Input("source_user")
	if prompt := setup.Input("source-sentinel-password"); prompt.Step != "target_host" {
		t.Fatalf("bounded verification did not advance setup: %+v", prompt)
	}
}

func TestPostgresSetupVerificationTimeoutIsRedacted(t *testing.T) {
	setup := newPostgresSetup("migration.yaml", func(ctx context.Context, endpoint config.Endpoint) (*sql.DB, error) {
		<-ctx.Done()
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("verification context error = %v, want deadline exceeded", ctx.Err())
		}
		return nil, ctx.Err()
	})
	setup.timeout = 0
	setup.Input("source.test")
	setup.Input("5432")
	setup.Input("source_db")
	setup.Input("source_user")
	prompt := setup.Input("source-sentinel-password")
	if prompt.Error != "source PostgreSQL connection could not be verified" {
		t.Fatalf("timeout error = %+v", prompt)
	}
	if strings.Contains(prompt.Error+prompt.Text, "sentinel-password") {
		t.Fatalf("timeout leaked password: %+v", prompt)
	}
}
