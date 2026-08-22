//go:build !windows

package secrets

func restrictSecretPath(string) error {
	return nil
}

func validatePlatformPermissions(string, error) error {
	return nil
}
