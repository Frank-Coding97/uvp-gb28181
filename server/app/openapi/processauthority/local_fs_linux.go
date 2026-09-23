package processauthority

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func persistentLocalFilesystem(file *os.File) bool {
	var fs unix.Statfs_t
	if unix.Fstatfs(int(file.Fd()), &fs) != nil {
		return false
	}
	switch fs.Type {
	case unix.EXT4_SUPER_MAGIC, unix.XFS_SUPER_MAGIC, unix.BTRFS_SUPER_MAGIC, unix.F2FS_SUPER_MAGIC:
		return true
	default:
		// Network, overlay and memory filesystems cannot establish the
		// deployment's persistent local authority domain by this check.
		return false
	}
}

func secureLocalACL(file *os.File) bool {
	for _, attr := range []string{"system.posix_acl_access", "system.posix_acl_default"} {
		_, err := unix.Fgetxattr(int(file.Fd()), attr, nil)
		if !errors.Is(err, unix.ENODATA) && !errors.Is(err, unix.ENOTSUP) {
			return false
		}
	}
	return true
}
