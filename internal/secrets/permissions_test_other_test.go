//go:build !windows

package secrets

import (
	"os"
	"testing"
)

func makeTestSecretInsecure(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
}
