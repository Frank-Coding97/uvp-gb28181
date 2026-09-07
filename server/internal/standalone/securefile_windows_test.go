//go:build windows

package standalone

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestWindowsACLPolicyUsesDirectFileFullAccess(t *testing.T) {
	require.Equal(t, windows.ACCESS_MASK(0x001F01FF), windowsFileAllAccessMask)
	require.NotEqual(t, windows.ACCESS_MASK(windows.GENERIC_ALL), windowsFileAllAccessMask)
}

func TestWindowsProtectedFileDescriptorUsesTwoDirectFAEntries(t *testing.T) {
	userSID, err := currentWindowsUserSID()
	require.NoError(t, err)
	descriptor, _, err := protectedFileSecurityAttributes(userSID)
	require.NoError(t, err)
	sddl := descriptor.String()
	require.Contains(t, sddl, "D:P")
	require.Contains(t, sddl, "FA")
	require.NotContains(t, sddl, "GA")
	require.NotContains(t, sddl, "OICI")
}

func TestWindowsConfigLockReleasesAfterCallbackPanic(t *testing.T) {
	dir := t.TempDir()
	panicked := false
	func() {
		defer func() {
			panicked = recover() != nil
		}()
		_ = withConfigLock(dir, func() error {
			panic("test callback panic")
		})
	}()
	require.True(t, panicked)
	require.NoError(t, withConfigLock(dir, func() error { return nil }))
}

func TestWindowsSecureACLAllowsSystemOrAdministratorsOwnerOnly(t *testing.T) {
	userSID, err := currentWindowsUserSID()
	require.NoError(t, err)
	administratorsSID, err := windows.StringToSid("S-1-5-32-544")
	require.NoError(t, err)
	descriptor := windowsTestACLDescriptor(t, userSID, administratorsSID)
	require.NoError(t, validateProtectedACL(descriptor, userSID, false))

	thirdUserSID, err := windows.StringToSid("S-1-5-21-1-2-3-999")
	require.NoError(t, err)
	descriptor = windowsTestACLDescriptor(t, userSID, thirdUserSID)
	require.Error(t, validateProtectedACL(descriptor, userSID, false))
}

func windowsTestACLDescriptor(t *testing.T, userSID, ownerSID *windows.SID) *windows.SECURITY_DESCRIPTOR {
	t.Helper()
	systemSID, err := windows.StringToSid("S-1-5-18")
	require.NoError(t, err)
	entries := []windows.EXPLICIT_ACCESS{
		{
			AccessPermissions: windowsFileAllAccessMask,
			AccessMode:        windows.SET_ACCESS,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeValue: windows.TrusteeValueFromSID(userSID),
			},
		},
		{
			AccessPermissions: windowsFileAllAccessMask,
			AccessMode:        windows.SET_ACCESS,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeValue: windows.TrusteeValueFromSID(systemSID),
			},
		},
	}
	acl, err := windows.ACLFromEntries(entries, nil)
	require.NoError(t, err)
	descriptor, err := windows.NewSecurityDescriptor()
	require.NoError(t, err)
	require.NoError(t, descriptor.SetDACL(acl, true, false))
	require.NoError(t, descriptor.SetOwner(ownerSID, false))
	require.NoError(t, descriptor.SetControl(windows.SE_DACL_PROTECTED, windows.SE_DACL_PROTECTED))
	return descriptor
}
