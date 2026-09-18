package processauthority

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLocalAuthorityRejectsDarwinExtendedACL(t *testing.T) {
	for _, target := range []string{"directory", "file"} {
		t.Run(target, func(t *testing.T) {
			dir := privateStateDir(t)
			owner, err := AcquireLocalLock(dir)
			require.NoError(t, err)
			defer owner.Close()
			path, grant := dir, "user:nobody allow add_file,delete_child"
			if target == "file" {
				path, grant = filepath.Join(dir, lockName), "user:nobody allow write,append"
			}
			out, err := exec.Command("/bin/chmod", "+a", grant, path).CombinedOutput()
			require.NoError(t, err, "%s", out)
			info, err := os.Stat(path)
			require.NoError(t, err)
			require.Zero(t, info.Mode().Perm()&0077, "the ACL bypass is invisible to POSIX mode checks")
			_, err = owner.DomainID()
			require.ErrorIs(t, err, ErrLocalAuthorityUnavailable)
			out, err = exec.Command("/bin/chmod", "-N", path).CombinedOutput()
			require.NoError(t, err, "%s", out)
			require.ErrorIs(t, owner.Check(), ErrLocalAuthorityUnavailable, "ACL repair cannot revive a poisoned owner")
			out, err = exec.Command("/bin/chmod", "+a", grant, path).CombinedOutput()
			require.NoError(t, err, "%s", out)
			require.NoError(t, owner.Close())
			replacement, err := AcquireLocalLock(dir)
			if replacement != nil {
				defer replacement.Close()
			}
			require.ErrorIs(t, err, ErrLocalAuthorityUnavailable)
			out, err = exec.Command("/bin/chmod", "-N", path).CombinedOutput()
			require.NoError(t, err, "%s", out)
			_, err = owner.DomainID()
			require.ErrorIs(t, err, ErrLocalAuthorityUnavailable)
		})
	}
}

func TestDarwinACLAttributeBounds(t *testing.T) {
	clean := func() []byte {
		data := make([]byte, 56)
		binary.NativeEndian.PutUint32(data[:4], 56)
		binary.NativeEndian.PutUint32(data[4:8], 8)
		binary.NativeEndian.PutUint32(data[8:12], 44)
		binary.NativeEndian.PutUint32(data[12:16], 0x012cc16d)
		return data
	}
	require.True(t, darwinACLAbsent(clean()))
	noACL := clean()
	binary.NativeEndian.PutUint32(noACL[48:52], ^uint32(0))
	require.True(t, darwinACLAbsent(noACL))
	for name, modify := range map[string]func([]byte) []byte{
		"short":           func(b []byte) []byte { return b[:11] },
		"short-return":    func(b []byte) []byte { binary.NativeEndian.PutUint32(b[:4], 11); return b },
		"oversize-return": func(b []byte) []byte { binary.NativeEndian.PutUint32(b[:4], 57); return b },
		"negative-offset": func(b []byte) []byte { binary.NativeEndian.PutUint32(b[4:8], ^uint32(0)); return b },
		"huge-offset":     func(b []byte) []byte { binary.NativeEndian.PutUint32(b[4:8], 0x7fffffff); return b },
		"bad-length":      func(b []byte) []byte { binary.NativeEndian.PutUint32(b[8:12], 43); return b },
		"huge-length":     func(b []byte) []byte { binary.NativeEndian.PutUint32(b[8:12], ^uint32(0)); return b },
		"bad-magic":       func(b []byte) []byte { b[12] = 0; return b },
		"nonempty":        func(b []byte) []byte { binary.NativeEndian.PutUint32(b[48:52], 1); return b },
	} {
		t.Run(name, func(t *testing.T) { require.False(t, darwinACLAbsent(modify(clean()))) })
	}
}
