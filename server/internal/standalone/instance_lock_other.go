//go:build !windows

package standalone

import (
	"errors"
	"golang.org/x/sys/unix"
	"path/filepath"
)

// This implementation supports local verification; the shipping launcher is Windows-only.
func AcquireInstanceLock(dir string) (*InstanceLock, error) {
	if err := ensureOtherDirectory(dir); err != nil {
		return nil, err
	}
	fd, err := unix.Open(filepath.Join(dir, ".uvp-instance.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		unix.Close(fd)
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrInstanceRunning
		}
		return nil, err
	}
	return &InstanceLock{release: func() error { return unix.Close(fd) }}, nil
}
