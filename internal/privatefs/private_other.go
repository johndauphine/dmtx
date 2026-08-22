//go:build !windows

// Package privatefs enforces owner-only access for application-owned files
// and directories using the native platform permission model.
package privatefs

import (
	"fmt"
	"os"
)

// Restrict makes path owner-only using Unix permission bits.
func Restrict(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if info.IsDir() {
		mode = 0o700
	}
	return os.Chmod(path, mode)
}

// Validate refuses paths that are accessible to group/other accounts or are
// not usable by their owner.
func Validate(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	mode := info.Mode().Perm()
	wantOwner := os.FileMode(0o600)
	if info.IsDir() {
		wantOwner = 0o700
	}
	if mode&0o077 != 0 || mode&wantOwner != wantOwner {
		return fmt.Errorf("%q is not owner-only and owner-accessible", path)
	}
	return nil
}
