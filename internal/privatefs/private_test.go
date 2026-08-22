package privatefs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRestrictAndValidateFileAndDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Restrict(directory); err != nil {
		t.Fatal(err)
	}
	if err := Validate(directory); err != nil {
		t.Fatalf("restricted directory rejected: %v", err)
	}
	path := filepath.Join(directory, "data")
	if err := os.WriteFile(path, []byte("private"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Restrict(path); err != nil {
		t.Fatal(err)
	}
	if err := Validate(path); err != nil {
		t.Fatalf("restricted file rejected: %v", err)
	}
}
