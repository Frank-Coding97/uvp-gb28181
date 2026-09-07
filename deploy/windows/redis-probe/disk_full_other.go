//go:build !windows

package main

import (
	"errors"
	"syscall"
)

func diskFullTestSupported() bool {
	return false
}

func platformNoSpaceError(err error) bool {
	return errors.Is(err, syscall.ENOSPC)
}

func inspectDiskFullTarget(path string) (diskVolumeInfo, error) {
	return diskVolumeInfo{}, errors.New("disk-full volume probing requires native Windows")
}
