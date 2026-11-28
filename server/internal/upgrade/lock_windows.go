//go:build windows

package upgrade

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
)

func lockUpgradeFile(file *os.File) error {
	err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, ^uint32(0), ^uint32(0), &windows.Overlapped{})
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return errUpgradeLockBusy
	}
	return err
}

func unlockUpgradeFile(file *os.File) error {
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, ^uint32(0), ^uint32(0), &windows.Overlapped{})
}
