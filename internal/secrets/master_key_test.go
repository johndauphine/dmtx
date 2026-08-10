package secrets

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureMasterKeyCreatesAndPreservesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	input := []byte("# keep\nencryption:\n  master_key: \"\"\nfuture:\n  setting: retained\n")
	if err := os.WriteFile(path, input, fileMode); err != nil {
		t.Fatal(err)
	}
	first, err := EnsureMasterKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 32 {
		t.Fatalf("key length %d", len(first))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("future:\n  setting: retained\n")) {
		t.Fatal("future section changed")
	}
	second, err := EnsureMasterKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("existing key changed")
	}
}

func TestEnsureMasterKeyRejectsInvalidOrInsecureKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	input := []byte("encryption:\n  master_key: \"bad\"\n")
	if err := os.WriteFile(path, input, fileMode); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureMasterKey(path); err == nil {
		t.Fatal("invalid key accepted")
	}
	if err := os.WriteFile(path, []byte("encryption:\n  master_key: \""+base64.RawStdEncoding.EncodeToString(make([]byte, 32))+"\"\n"), fileMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureMasterKey(path); err == nil {
		t.Fatal("insecure file accepted")
	}
}
