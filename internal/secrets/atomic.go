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
	create func(string, string) (atomicFile, error)
	rename func(string, string) error
	remove func(string) error
}

var defaultAtomicOps = atomicOps{
	create: func(directory, pattern string) (atomicFile, error) {
		return os.CreateTemp(directory, pattern)
	},
	rename: replaceSecretFile,
	remove: os.Remove,
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
