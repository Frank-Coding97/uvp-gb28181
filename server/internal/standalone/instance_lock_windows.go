//go:build windows

package standalone

import (
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func AcquireInstanceLock(dir string) (*InstanceLock, error) {
	if err := ensureWindowsDirectory(dir); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, ".uvp-instance.lock")
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		if isWindowsLockContention(err) {
			return nil, ErrInstanceRunning
		}
		return nil, fmt.Errorf("acquire instance lock: %w", err)
	}
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(handle, &info); err != nil {
		windows.CloseHandle(handle)
		return nil, err
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		windows.CloseHandle(handle)
		return nil, ErrPathSymlink
	}
	return &InstanceLock{release: func() error { return windows.CloseHandle(handle) }}, nil
}
