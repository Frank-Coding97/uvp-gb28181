//go:build windows

package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	getVolumeInformationProc = kernel32.NewProc("GetVolumeInformationW")
	getDiskFreeSpaceExProc   = kernel32.NewProc("GetDiskFreeSpaceExW")
)

func diskFullTestSupported() bool {
	return true
}

func platformNoSpaceError(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	return errno == syscall.Errno(39) || errno == syscall.Errno(112)
}

func inspectDiskFullTarget(path string) (diskVolumeInfo, error) {
	cleanPath := filepath.Clean(path)
	volumeName := filepath.VolumeName(cleanPath)
	if volumeName == "" {
		return diskVolumeInfo{}, fmt.Errorf("disk-full target %q has no Windows volume name", path)
	}
	if len(volumeName) != 2 || volumeName[1] != ':' {
		return diskVolumeInfo{}, fmt.Errorf("disk-full target must be a local drive root, got %q", volumeName)
	}
	root := volumeName + `\`
	if !strings.EqualFold(cleanPath, filepath.Clean(root)) {
		return diskVolumeInfo{}, fmt.Errorf("disk-full target must be a volume root such as %s", root)
	}
	rootUTF16, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return diskVolumeInfo{}, fmt.Errorf("encode volume root: %w", err)
	}
	var volumeLabel [261]uint16
	var serialNumber uint32
	var maximumComponentLength uint32
	var fileSystemFlags uint32
	var fileSystemName [261]uint16
	result, _, callErr := getVolumeInformationProc.Call(
		uintptr(unsafe.Pointer(rootUTF16)),
		uintptr(unsafe.Pointer(&volumeLabel[0])),
		uintptr(len(volumeLabel)),
		uintptr(unsafe.Pointer(&serialNumber)),
		uintptr(unsafe.Pointer(&maximumComponentLength)),
		uintptr(unsafe.Pointer(&fileSystemFlags)),
		uintptr(unsafe.Pointer(&fileSystemName[0])),
		uintptr(len(fileSystemName)),
	)
	if result == 0 {
		return diskVolumeInfo{}, fmt.Errorf("GetVolumeInformationW(%s): %w", root, callErr)
	}
	var freeBytesAvailable uint64
	var totalBytes uint64
	var totalFreeBytes uint64
	result, _, callErr = getDiskFreeSpaceExProc.Call(
		uintptr(unsafe.Pointer(rootUTF16)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFreeBytes)),
	)
	if result == 0 {
		return diskVolumeInfo{}, fmt.Errorf("GetDiskFreeSpaceExW(%s): %w", root, callErr)
	}
	return diskVolumeInfo{
		Root:       root,
		Label:      syscall.UTF16ToString(volumeLabel[:]),
		TotalBytes: totalBytes,
		FreeBytes:  freeBytesAvailable,
	}, nil
}
