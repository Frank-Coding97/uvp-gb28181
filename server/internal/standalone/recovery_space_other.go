//go:build !windows && !darwin && !linux

package standalone

import "errors"

func recoveryAvailableSpace(string) (uint64, error) {
	return 0, errors.New("recovery disk space query unsupported on this platform")
}
