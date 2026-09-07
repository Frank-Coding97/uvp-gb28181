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
