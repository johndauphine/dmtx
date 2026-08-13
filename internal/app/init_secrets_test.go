package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/johndauphine/dmtx/internal/secrets"
)

// TestInitSecretsLifecycle keeps the deliberately cautious key-file creation
// workflow executable.  In particular, a second invocation reports rather
// than replaces key material; --force is the explicit escape hatch.
func TestInitSecretsLifecycle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path, err := secrets.Path()
	if err != nil {
		t.Fatal(err)
	}

	first := executeInitSecrets(Request{Command: "init-secrets"})
	if first.ExitCode != Success {
		t.Fatalf("initial creation failed: %s", saidBy(first))
	}
	if !strings.Contains(saidBy(first), "wrote "+path) {
		t.Errorf("creation did not name its result: %q", saidBy(first))
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != secrets.Template {
		t.Errorf("initial contents = %q, want starter template", before)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Errorf("secret file mode = %04o, want owner-only", info.Mode().Perm())
		}
		directory, err := os.Stat(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		if directory.Mode().Perm()&0o077 != 0 {
			t.Errorf("secret directory mode = %04o, want owner-only", directory.Mode().Perm())
		}
	}

	second := executeInitSecrets(Request{Command: "init-secrets"})
	if second.ExitCode != Success {
		t.Fatalf("second invocation should report safely: %s", saidBy(second))
	}
	if got := saidBy(second); !strings.Contains(got, "already exists") || !strings.Contains(got, "--force") {
		t.Errorf("second invocation lacks idempotent/refusal guidance: %q", got)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("second invocation changed existing key material")
	}

	if err := os.WriteFile(path, []byte("old key material"), 0o644); err != nil {
		t.Fatal(err)
	}
	forced := executeInitSecrets(Request{Command: "init-secrets", Force: true})
	if forced.ExitCode != Success {
		t.Fatalf("forced replacement failed: %s", saidBy(forced))
	}
	replaced, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(replaced) != secrets.Template {
		t.Error("--force did not replace the existing secrets file")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Errorf("forced secret file mode = %04o, want owner-only", info.Mode().Perm())
		}
	}
}

// TestInitSecretsFailureDoesNotEchoSensitiveFileContents ensures an OS error
// remains diagnostic without turning a secret-shaped pathname into output.
func TestInitSecretsFailureDoesNotEchoSensitiveFileContents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// A file where ~/.secrets must be a directory makes Create fail before it
	// can write. Its contents must never be treated as part of the diagnostic.
	if err := os.WriteFile(filepath.Join(home, ".secrets"), []byte("do-not-echo-this-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	outcome := executeInitSecrets(Request{Command: "init-secrets"})
	if outcome.ExitCode == Success {
		t.Fatal("init-secrets succeeded through a non-directory parent")
	}
	if got := saidBy(outcome); strings.Contains(got, "do-not-echo-this-secret") {
		t.Errorf("failure leaked file contents: %q", got)
	}
}
