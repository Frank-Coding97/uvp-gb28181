//go:build windows

package standalone

import "golang.org/x/sys/windows"

func recoveryAvailableSpace(path string) (uint64, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var available uint64
	err = windows.GetDiskFreeSpaceEx(name, &available, nil, nil)
	return available, err
}
