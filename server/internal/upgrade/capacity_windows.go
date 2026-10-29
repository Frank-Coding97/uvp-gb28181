//go:build windows

package upgrade

import (
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// windowsAllocationUnit is the NTFS/extent allocation unit assumed for the
// budget. Windows does not expose the cluster size through a cheap portable
// call, and 4 KiB is the default for every NTFS volume the installer creates.
// Overestimating is safe (the budget is only a refusal threshold); guessing too
// small silently under-budgets, so we deliberately round up from the common
// case rather than down.
const windowsAllocationUnit uint64 = 4 << 10

// probeFilesystemCapacity mirrors the Linux helper on Windows: the volume
// serial number identifies the filesystem and the caller-available byte count
// is what an upgrade may actually consume.
func probeFilesystemCapacity(path string) (filesystemCapacity, error) {
	if path == "" {
		return filesystemCapacity{}, fmt.Errorf("capacity probe path is empty")
	}
	volume := filepath.VolumeName(path)
	if volume == "" {
		return filesystemCapacity{}, fmt.Errorf("capacity probe path %q has no volume", path)
	}
	root := volume + string(filepath.Separator)
	volumePath, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return filesystemCapacity{}, err
	}
	var serial uint32
	// Buffer sizes must be non-zero even when the caller ignores the strings.
	nameBuffer := make([]uint16, windows.MAX_PATH+1)
	fsBuffer := make([]uint16, windows.MAX_PATH+1)
	if err := windows.GetVolumeInformation(volumePath, &nameBuffer[0], uint32(len(nameBuffer)), &serial, nil, nil, &fsBuffer[0], uint32(len(fsBuffer))); err != nil {
		return filesystemCapacity{}, fmt.Errorf("read volume serial for %s: %w", root, err)
	}
	dirPath, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return filesystemCapacity{}, err
	}
	var availableToCaller, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(dirPath, &availableToCaller, &total, &totalFree); err != nil {
		return filesystemCapacity{}, fmt.Errorf("read free space for %s: %w", root, err)
	}
	return filesystemCapacity{
		Device:    uint64(serial),
		Available: availableToCaller,
		BlockSize: windowsAllocationUnit,
	}, nil
}
