package processauthority

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func setAuthorityTestDACL(t *testing.T, path, extra string) {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	require.NoError(t, err)
	sd, err := windows.SecurityDescriptorFromString(fmt.Sprintf("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FA;;;%s)%s", user.User.Sid.String(), extra))
	require.NoError(t, err)
	dacl, _, err := sd.DACL()
	require.NoError(t, err)
	require.NoError(t, windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil))
}

func TestLocalAuthorityRejectsWindowsJunction(t *testing.T) {
	dir := privateStateDir(t)
	link := filepath.Join(privateStateDir(t), "junction")
	out, err := exec.Command("cmd", "/c", "mklink", "/J", link, dir).CombinedOutput()
	require.NoError(t, err, "%s", out)
	owner, err := AcquireLocalLock(link)
	if owner != nil {
		defer owner.Close()
	}
	require.ErrorIs(t, err, ErrLocalAuthorityUnavailable)
}

func privateAuthorityTestPermissions(t *testing.T, path string) {
	t.Helper()
	setAuthorityTestDACL(t, path, "")
}

func TestLocalAuthorityRejectsWindowsUntrustedACL(t *testing.T) {
	for _, kind := range []string{"directory-write", "file-write", "file-read"} {
		t.Run(kind, func(t *testing.T) {
			dir := privateStateDir(t)
			setAuthorityTestDACL(t, dir, "")
			owner, err := AcquireLocalLock(dir)
			require.NoError(t, err)
			defer owner.Close()
			path, ace := filepath.Join(dir, lockName), "(A;;FW;;;WD)"
			if kind == "directory-write" {
				path = dir
			} else if kind == "file-read" {
				ace = "(A;;FR;;;WD)"
			}
			setAuthorityTestDACL(t, path, ace)
			require.ErrorIs(t, owner.Check(), ErrLocalAuthorityUnavailable)
			setAuthorityTestDACL(t, path, "")
			require.ErrorIs(t, owner.Check(), ErrLocalAuthorityUnavailable, "ACL repair cannot revive a poisoned owner")
			require.NoError(t, owner.Close())
			setAuthorityTestDACL(t, path, ace)
			replacement, err := AcquireLocalLock(dir)
			if replacement != nil {
				defer replacement.Close()
			}
			require.ErrorIs(t, err, ErrLocalAuthorityUnavailable)
		})
	}
}
