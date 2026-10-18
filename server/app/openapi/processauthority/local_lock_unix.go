//go:build linux || darwin

package processauthority

import (
	"errors"
	"fmt"
	"os"
	"strings"

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

// localFileIdentity names the held file by inode only, on purpose: Darwin
// assigns volume device numbers dynamically, so the very same lock file
// reports a different st_dev after every reboot while its inode is stable.
// Baking the device number into the persistent record made the platform
// refuse to start after a reboot (phase=process_authority
// reason=state_unavailable). The open descriptor already proves which file
// is held; the record only has to detect that the file was replaced.
func localFileIdentity(file *os.File) (string, error) {
	var stat unix.Stat_t
	if unix.Fstat(int(file.Fd()), &stat) != nil {
		return "", ErrLocalAuthorityUnavailable
	}
	return fmt.Sprintf("unix:%x", stat.Ino), nil
}

// localIdentityMatch accepts a record written before the device number was
// dropped: both sides are compared by scheme and inode, so a legacy
// "unix:<device>:<inode>" record still names the same file and a file copied
// to a different inode still cannot inherit authority.
func localIdentityMatch(recorded, live string) bool {
	if recorded == live {
		return true
	}
	if identityScheme(recorded) != identityScheme(live) {
		return false
	}
	recordedInode := recorded[strings.LastIndexByte(recorded, ':')+1:]
	liveInode := live[strings.LastIndexByte(live, ':')+1:]
	return recordedInode != "" && recordedInode == liveInode
}

func identityScheme(identity string) string {
	if i := strings.IndexByte(identity, ':'); i >= 0 {
		return identity[:i]
	}
	return identity
}
