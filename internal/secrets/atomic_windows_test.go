//go:build windows

package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWrite0600ReplacesExistingWindowsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("old"), fileMode); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite0600(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("replacement contents = %q, want new", data)
	}
}
