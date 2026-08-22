//go:build windows

package app

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

const (
	windowsFileAddFile     = 0x0002
	windowsFileDeleteChild = 0x0040
)

func sqliteTargetFreeBytes(path string) (uint64, bool) {
	location := path
	if info, err := os.Stat(location); err == nil {
		if !info.IsDir() {
			location = filepath.Dir(location)
		}
	} else if os.IsNotExist(err) {
		location = filepath.Dir(location)
	} else {
		return 0, false
	}
	name, err := windows.UTF16PtrFromString(location)
	if err != nil {
		return 0, false
	}
	var available uint64
	if err := windows.GetDiskFreeSpaceEx(name, &available, nil, nil); err != nil {
		return 0, false
	}
	return available, true
}

func sqliteTargetParentWriteAccess(path string) (bool, bool) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsPermission(err) || os.IsNotExist(err) {
			return false, true
		}
		return false, false
	}
	if !info.IsDir() {
		return false, true
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false, false
	}
	handle, err := windows.CreateFile(
		name,
		windowsFileAddFile|windowsFileDeleteChild,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) ||
			errors.Is(err, windows.ERROR_FILE_NOT_FOUND) ||
			errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
			return false, true
		}
		return false, false
	}
	return windows.CloseHandle(handle) == nil, true
}
