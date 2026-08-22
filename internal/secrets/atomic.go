package secrets

import (
	"fmt"
	"os"
	"path/filepath"
)

type atomicFile interface {
	Name() string
	Chmod(os.FileMode) error
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

type atomicOps struct {
	create   func(string, string) (atomicFile, error)
	restrict func(string) error
	rename   func(string, string) error
	remove   func(string) error
}

var defaultAtomicOps = atomicOps{
	create: func(directory, pattern string) (atomicFile, error) {
		return os.CreateTemp(directory, pattern)
	},
	restrict: restrictSecretPath,
	rename:   replaceSecretFile,
	remove:   os.Remove,
}

// WriteProtectedFile atomically creates or replaces an owner-only regular
// file. Existing symlinks and non-regular files are refused before any bytes
// are written. The replacement itself is a rename, so a destination changed
// after the check is replaced as a directory entry rather than followed.
//
// This is exported for other packages that deliberately materialise sensitive
// data, such as an explicitly requested plaintext profile export. Callers must
// still decide whether writing the data is allowed in the first place.
func WriteProtectedFile(path string, data []byte) error {
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refuse protected file destination %q: existing path is not a regular file", path)
		}
	case !os.IsNotExist(err):
		return fmt.Errorf("inspect protected file destination %q: %w", path, err)
	}
	return atomicWrite0600(path, data)
}

// atomicWrite0600 replaces path only after the replacement has been fully
// written, synced, closed, and restricted. Its errors name the operation,
// never the bytes.
func atomicWrite0600(path string, data []byte) (err error) {
	return atomicWrite0600With(path, data, defaultAtomicOps)
}

func atomicWrite0600With(path string, data []byte, ops atomicOps) (err error) {
	directory := filepath.Dir(path)
	temporary, err := ops.create(directory, ".dmtx-secrets-*")
	if err != nil {
		return fmt.Errorf("create secure temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		if err != nil {
			_ = temporary.Close()
			_ = ops.remove(temporaryPath)
		}
	}()
	if err = temporary.Chmod(fileMode); err != nil {
		return fmt.Errorf("restrict secure temporary file: %w", err)
	}
	if ops.restrict != nil {
		if err = ops.restrict(temporaryPath); err != nil {
			return fmt.Errorf("restrict secure temporary file access: %w", err)
		}
	}
	if _, err = temporary.Write(data); err != nil {
		return fmt.Errorf("write secure temporary file: %w", err)
	}
	if err = temporary.Sync(); err != nil {
		return fmt.Errorf("sync secure temporary file: %w", err)
	}
	if err = temporary.Close(); err != nil {
		return fmt.Errorf("close secure temporary file: %w", err)
	}
	if err = ops.rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace secure file: %w", err)
	}
	return nil
}
