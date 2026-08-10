//go:build !windows

package secrets

import "os"

func replaceSecretFile(temporaryPath, destinationPath string) error {
	return os.Rename(temporaryPath, destinationPath)
}
