//go:build windows

package standalone

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestWindowsBeginRunRejectsOverbroadMarkerACL(t *testing.T) {
	paths := runMarkerTestPaths(t)
	marker, err := BeginRun(paths)
	require.NoError(t, err)
	markerPath := testRunMarkerPath(paths)
	userSID, err := currentWindowsUserSID()
	require.NoError(t, err)
	systemSID, err := windows.StringToSid("S-1-5-18")
	require.NoError(t, err)
	everyoneSID, err := windows.StringToSid("S-1-1-0")
	require.NoError(t, err)
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{
		{
			AccessPermissions: windowsFileAllAccessMask,
			AccessMode:        windows.SET_ACCESS,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_USER,
				TrusteeValue: windows.TrusteeValueFromSID(userSID),
			},
		},
		{
			AccessPermissions: windowsFileAllAccessMask,
			AccessMode:        windows.SET_ACCESS,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_WELL_KNOWN_GROUP,
				TrusteeValue: windows.TrusteeValueFromSID(systemSID),
			},
		},
		{
			AccessPermissions: windowsFileAllAccessMask,
			AccessMode:        windows.SET_ACCESS,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_WELL_KNOWN_GROUP,
				TrusteeValue: windows.TrusteeValueFromSID(everyoneSID),
			},
		},
	}, nil)
	require.NoError(t, err)
	require.NoError(t, windows.SetNamedSecurityInfo(
		markerPath,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	))

	next, err := BeginRun(paths)
	require.Error(t, err)
	require.Nil(t, next)
	require.NoError(t, setProtectedACL(markerPath, userSID, false))
	require.NoError(t, marker.Finish())
	_, err = os.Stat(markerPath)
	require.ErrorIs(t, err, os.ErrNotExist)
}
