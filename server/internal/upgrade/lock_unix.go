//go:build linux || darwin

package upgrade

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func lockUpgradeFile(file *os.File) error {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return errUpgradeLockBusy
	}
	return err
}

func unlockUpgradeFile(file *os.File) error { return unix.Flock(int(file.Fd()), unix.LOCK_UN) }
