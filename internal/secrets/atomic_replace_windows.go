//go:build windows

package secrets

import "golang.org/x/sys/windows"

func replaceSecretFile(temporaryPath, destinationPath string) error {
	temporary, err := windows.UTF16PtrFromString(temporaryPath)
	if err != nil {
		return err
	}
	destination, err := windows.UTF16PtrFromString(destinationPath)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(
		temporary,
		destination,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH,
	)
}
