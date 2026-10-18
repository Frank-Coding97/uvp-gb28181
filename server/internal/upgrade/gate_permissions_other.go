//go:build !linux && !darwin

package upgrade

import (
	"errors"
	"os"
)

func verifySharedGateFile(os.FileInfo) error {
	return errors.New("shared updater gate permissions are unsupported on this platform")
}

func verifySharedGateParent(os.FileInfo) error {
	return errors.New("shared updater gate permissions are unsupported on this platform")
}

func VerifySharedGateParent(info os.FileInfo) error {
	return verifySharedGateParent(info)
}

func inheritSharedGateGroup(*os.File, os.FileInfo) error {
	return errors.New("shared updater gate permissions are unsupported on this platform")
}
