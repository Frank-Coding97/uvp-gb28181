//go:build linux || darwin

package processauthority

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func lockLocalFile(file *os.File) error {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return ErrLocalAuthorityBusy
	}
	if err != nil {
		return ErrLocalAuthorityUnavailable
	}
	return nil
}

func secureLocalFile(file *os.File, directory bool) bool {
	var stat unix.Stat_t
	if unix.Fstat(int(file.Fd()), &stat) != nil || stat.Uid != uint32(os.Geteuid()) || stat.Mode&0077 != 0 {
		return false
	}
	if directory {
		if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
			return false
		}
	} else if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 {
		return false
	}
	return persistentLocalFilesystem(file) && secureLocalACL(file)
}

func syncLocalDirectory(file *os.File) error { return file.Sync() }

func localFileIdentity(file *os.File) (string, error) {
	var stat unix.Stat_t
	if unix.Fstat(int(file.Fd()), &stat) != nil {
		return "", ErrLocalAuthorityUnavailable
	}
	return fmt.Sprintf("unix:%x:%x", uint64(stat.Dev), stat.Ino), nil
}
