//go:build windows

package upgrade

import (
	"errors"
	"os"
)

// Windows has no POSIX mode bits, so the shared gate cannot be checked with the
// Unix 0640/0750 rules. The gate still lives inside the installation root,
// which the installer creates under the administrator's profile directory, so
// the only portable assertions left are "a real file/directory" and "not a
// reparse point" (a junction or symlink could redirect the gate elsewhere).
func verifySharedGateFile(info os.FileInfo) error {
	if info == nil {
		return errors.New("configured updater gate is unavailable")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("configured updater gate is not a regular file")
	}
	return nil
}

func verifySharedGateParent(info os.FileInfo) error {
	if info == nil {
		return errors.New("shared updater gate parent is unavailable")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("shared updater gate parent is not a real directory")
	}
	return nil
}

// VerifySharedGateParent lets the privileged helper reject an existing unsafe
// parent before it opens or creates the shared gate file.
func VerifySharedGateParent(info os.FileInfo) error {
	return verifySharedGateParent(info)
}

func inheritSharedGateGroup(*os.File, os.FileInfo) error {
	// No group ownership to inherit; the gate inherits the directory ACL.
	return nil
}
