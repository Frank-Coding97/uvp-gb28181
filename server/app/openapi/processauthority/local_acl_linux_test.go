package processauthority

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestLocalAuthorityRejectsLinuxExtendedACL(t *testing.T) {
	for _, kind := range []string{"directory-access", "directory-default", "file-access"} {
		t.Run(kind, func(t *testing.T) {
			dir := privateStateDir(t)
			owner, err := AcquireLocalLock(dir)
			require.NoError(t, err)
			defer owner.Close()
			path, attr := dir, "system.posix_acl_access"
			if kind == "file-access" {
				path = filepath.Join(dir, lockName)
			} else if kind == "directory-default" {
				attr = "system.posix_acl_default"
			}
			// A named-user ACL with a zero access mask leaves POSIX mode
			// private; default ACLs also do not change directory mode bits.
			data := make([]byte, 4+5*8)
			binary.LittleEndian.PutUint32(data[:4], 2)
			for i, entry := range [][3]uint32{{1, 7, ^uint32(0)}, {2, 7, 65534}, {4, 0, ^uint32(0)}, {16, 0, ^uint32(0)}, {32, 0, ^uint32(0)}} {
				at := 4 + i*8
				binary.LittleEndian.PutUint16(data[at:], uint16(entry[0]))
				binary.LittleEndian.PutUint16(data[at+2:], uint16(entry[1]))
				binary.LittleEndian.PutUint32(data[at+4:], entry[2])
			}
			require.NoError(t, unix.Setxattr(path, attr, data, 0))
			info, err := os.Stat(path)
			require.NoError(t, err)
			require.Zero(t, info.Mode().Perm()&0077)
			require.ErrorIs(t, owner.Check(), ErrLocalAuthorityUnavailable)
			require.NoError(t, unix.Removexattr(path, attr))
			require.ErrorIs(t, owner.Check(), ErrLocalAuthorityUnavailable)
			require.NoError(t, owner.Close())
			require.NoError(t, unix.Setxattr(path, attr, data, 0))
			replacement, err := AcquireLocalLock(dir)
			if replacement != nil {
				defer replacement.Close()
			}
			require.ErrorIs(t, err, ErrLocalAuthorityUnavailable)
		})
	}
}
