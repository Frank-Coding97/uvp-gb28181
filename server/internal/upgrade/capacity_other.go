//go:build !linux && !darwin && !windows

package upgrade

import "errors"

func probeFilesystemCapacity(string) (filesystemCapacity, error) {
	return filesystemCapacity{}, errors.New("upgrade capacity preflight is unsupported on this platform")
}
