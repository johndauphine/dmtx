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
	mode     os.FileMode
	written  bool
	synced   bool
	closed   bool
}

func (file *fakeAtomicFile) Name() string { return file.path }
func (file *fakeAtomicFile) Chmod(mode os.FileMode) error {
	file.mode = mode
	return nil
}
func (file *fakeAtomicFile) Write(data []byte) (int, error) {
	if file.writeErr != nil {
		return 0, file.writeErr
	}
	file.written = true
	return len(data), nil
}
func (file *fakeAtomicFile) Sync() error {
	file.synced = true
	return file.syncErr
}
func (file *fakeAtomicFile) Close() error {
	file.closed = true
	return nil
}

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

func TestWriteProtectedFileRefusesSymlinkAndNonRegularDestinations(t *testing.T) {
	directory := t.TempDir()
	t.Run("symlink", func(t *testing.T) {
		target := filepath.Join(directory, "target.yaml")
		if err := os.WriteFile(target, []byte("original"), fileMode); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(directory, "export.yaml")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if err := WriteProtectedFile(link, []byte("decrypted-profile")); err == nil {
			t.Fatal("symlink destination was accepted")
		}
		got, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "original" {
			t.Fatalf("symlink target changed to %q", got)
		}
	})

	t.Run("directory", func(t *testing.T) {
		nonRegular := filepath.Join(directory, "existing-directory")
		if err := os.Mkdir(nonRegular, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := WriteProtectedFile(nonRegular, []byte("decrypted-profile")); err == nil {
			t.Fatal("non-regular destination was accepted")
		}
	})
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

func TestAtomicWrite0600RestrictsBeforeReplacing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	temporary := &fakeAtomicFile{path: filepath.Join(filepath.Dir(path), ".temporary")}
	restricted := false
	ops := atomicOps{
		create: func(string, string) (atomicFile, error) { return temporary, nil },
		restrict: func(name string) error {
			if name != temporary.path || temporary.mode != fileMode {
				t.Fatalf("restricted wrong or unmoded path %q", name)
			}
			restricted = true
			return nil
		},
		rename: func(string, string) error {
			if temporary.mode != fileMode || !restricted || !temporary.written || !temporary.synced || !temporary.closed {
				t.Fatalf("replacement occurred before temporary file was secured: %+v", temporary)
			}
			return nil
		},
		remove: func(string) error { return nil },
	}
	if err := atomicWrite0600With(path, []byte("secret-payload"), ops); err != nil {
		t.Fatal(err)
	}
}
