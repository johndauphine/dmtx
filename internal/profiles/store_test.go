package profiles

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johndauphine/dmtx/internal/secrets"
)

func writeSecretsFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOpenWithSecretsCreatesKeyAndSealsStore(t *testing.T) {
	directory := t.TempDir()
	secretsPath := filepath.Join(directory, "secrets.yaml")
	profilesPath := filepath.Join(directory, "profiles.db")
	writeSecretsFile(t, secretsPath, []byte("# retained\nencryption:\n  master_key: \"\"\nfuture:\n  setting: keep\n"))

	store, err := OpenWithSecrets(profilesPath, secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("source:\n  password: profile-plaintext\n")
	if err := store.Save("production", plain); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	for _, storedPath := range []string{profilesPath, profilesPath + "-wal"} {
		stored, readErr := os.ReadFile(storedPath)
		if readErr == nil && bytes.Contains(stored, plain) {
			t.Fatalf("profile plaintext persisted in %s", filepath.Base(storedPath))
		}
	}

	key, err := secrets.EnsureMasterKey(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != 32 {
		t.Fatalf("generated key length = %d, want 32", len(key))
	}
	secretData, err := os.ReadFile(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(secretData, []byte("future:\n  setting: keep\n")) {
		t.Fatal("unrelated secrets content changed")
	}
	if bytes.Contains(secretData, plain) {
		t.Fatal("profile plaintext persisted to secrets file")
	}
	store, err = Open(profilesPath, key)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.Load("production")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatal("profile was not recoverable with generated persisted key")
	}
}

func TestOpenWithSecretsUsesExistingKey(t *testing.T) {
	directory := t.TempDir()
	secretsPath := filepath.Join(directory, "secrets.yaml")
	profilesPath := filepath.Join(directory, "profiles.db")
	key := bytes.Repeat([]byte{0x2a}, 32)
	writeSecretsFile(t, secretsPath, []byte("encryption:\n  master_key: \""+base64.RawStdEncoding.EncodeToString(key)+"\"\n"))

	store, err := OpenWithSecrets(profilesPath, secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save("existing", []byte("driver: postgres\n")); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(profilesPath, key)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Load("existing"); err != nil {
		t.Fatalf("existing key did not open store: %v", err)
	}
}

func TestOpenWithSecretsRefusesUnsafeSecretsWithoutLeakingContents(t *testing.T) {
	directory := t.TempDir()
	secretsPath := filepath.Join(directory, "secrets.yaml")
	profilesPath := filepath.Join(directory, "profiles.db")
	sentinel := "do-not-expose-this-secret"
	writeSecretsFile(t, secretsPath, []byte("encryption: [\""+sentinel+"\"\n"))

	_, err := OpenWithSecrets(profilesPath, secretsPath)
	if err == nil {
		t.Fatal("malformed secrets accepted")
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Fatal("secrets content leaked through error")
	}
	if _, statErr := os.Stat(profilesPath); !os.IsNotExist(statErr) {
		t.Fatal("profile store was created after secrets refusal")
	}
}
