package secrets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type fakeAtomicFile struct {
	path     string
	writeErr error
	syncErr  error
}

func (file *fakeAtomicFile) Name() string       { return file.path }
func (*fakeAtomicFile) Chmod(os.FileMode) error { return nil }
func (file *fakeAtomicFile) Write(data []byte) (int, error) {
	if file.writeErr != nil {
		return 0, file.writeErr
	}
	return len(data), nil
}
func (file *fakeAtomicFile) Sync() error { return file.syncErr }
func (*fakeAtomicFile) Close() error     { return nil }

func TestAtomicWrite0600WithRenameFailurePreservesDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("old"), fileMode); err != nil {
		t.Fatal(err)
	}
	temporary := &fakeAtomicFile{path: filepath.Join(filepath.Dir(path), ".temporary")}
	removed := ""
	ops := atomicOps{
		create: func(string, string) (atomicFile, error) { return temporary, nil },
		rename: func(string, string) error { return os.ErrPermission },
		remove: func(name string) error { removed = name; return nil },
	}
	err := atomicWrite0600With(path, []byte("secret-payload"), ops)
	if err == nil || strings.Contains(err.Error(), "secret-payload") {
		t.Fatalf("unsafe error: %v", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil || string(got) != "old" {
		t.Fatalf("destination changed: %q %v", got, readErr)
	}
	if removed != temporary.path {
		t.Fatalf("temporary file was not removed: %q", removed)
	}
}

func TestAtomicWrite0600WithWriteOrSyncFailureCleansTemporary(t *testing.T) {
	for _, failure := range []string{"write", "sync"} {
		t.Run(failure, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("old"), fileMode); err != nil {
				t.Fatal(err)
			}
			temporary := &fakeAtomicFile{path: filepath.Join(filepath.Dir(path), ".temporary")}
			if failure == "write" {
				temporary.writeErr = os.ErrPermission
			} else {
				temporary.syncErr = os.ErrPermission
			}
			removed := ""
			ops := atomicOps{
				create: func(string, string) (atomicFile, error) { return temporary, nil },
				rename: func(string, string) error { t.Fatal("rename after failure"); return nil },
				remove: func(name string) error { removed = name; return nil },
			}
			err := atomicWrite0600With(path, []byte("secret-payload"), ops)
			if err == nil || strings.Contains(err.Error(), "secret-payload") {
				t.Fatalf("unsafe error: %v", err)
			}
			got, readErr := os.ReadFile(path)
			if readErr != nil || string(got) != "old" {
				t.Fatalf("destination changed: %q %v", got, readErr)
			}
			if removed != temporary.path {
				t.Fatalf("temporary file was not removed: %q", removed)
			}
		})
	}
}

func TestAtomicWrite0600ReplacesContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite0600(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("got %q", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != fileMode {
			t.Fatalf("mode %04o", info.Mode().Perm())
		}
	}
}

func TestAtomicWrite0600DoesNotExposeDataInErrors(t *testing.T) {
	err := atomicWrite0600(filepath.Join(t.TempDir(), "missing", "config.yaml"), []byte("super-secret"))
	if err == nil {
		t.Fatal("expected error")
	}
	if string(err.Error()) == "super-secret" {
		t.Fatal("secret leaked")
	}
}
