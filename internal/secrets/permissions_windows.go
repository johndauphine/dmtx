//go:build windows

package secrets

import (
	"fmt"

	"github.com/johndauphine/dmtx/internal/privatefs"
)

func restrictSecretPath(path string) error {
	return privatefs.Restrict(path)
}

func validatePlatformPermissions(path string, insecure error) error {
	if err := privatefs.Validate(path); err != nil {
		return fmt.Errorf("%w: %v; restrict its Windows ACL to the current account", insecure, err)
	}
	return nil
}
