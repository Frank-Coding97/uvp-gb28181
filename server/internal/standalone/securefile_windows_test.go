//go:build windows

package standalone

import (
	"io"
	"os"
	"path/filepath"
	"strings"
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

func TestWindowsProtectedDirectoryUsesTwoInheritedFAEntries(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, protectConfigDir(dir, true))
	userSID, err := currentWindowsUserSID()
	require.NoError(t, err)
	descriptor, err := windows.GetNamedSecurityInfo(
		dir,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION,
	)
	require.NoError(t, err)
	dacl, _, err := descriptor.DACL()
	require.NoError(t, err)
	require.NotNil(t, dacl)
	require.Equal(t, uint16(2), dacl.AceCount)
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		require.NoError(t, windows.GetAce(dacl, index, &ace))
		require.Equal(t, uint8(windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE), ace.Header.AceFlags)
		require.Equal(t, windowsFileAllAccessMask, ace.Mask)
	}
	require.NoError(t, validateProtectedACL(descriptor, userSID, true))
}

func TestWindowsAtomicReplaceKeepsSharedReaderSnapshotAndPublishesNewACL(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, protectConfigDir(dir, true))
	target := filepath.Join(dir, "replace shared 中文.yml")
	require.NoError(t, writeSecureConfigFile(target, []byte("old-value"), false, nil))
	reader := openWindowsSecureReadHandle(t, target, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE)
	defer reader.Close()

	require.NoError(t, writeSecureConfigFile(target, []byte("new-value"), true, nil))
	oldValue, err := readWindowsFileFromStart(reader)
	require.NoError(t, err)
	require.Equal(t, []byte("old-value"), oldValue)
	newValue, err := readSecureConfigFile(target)
	require.NoError(t, err)
	require.Equal(t, []byte("new-value"), newValue)
	userSID, err := currentWindowsUserSID()
	require.NoError(t, err)
	require.NoError(t, validateProtectedACLPath(target, userSID, false))
}

func TestWindowsAtomicReplaceRejectsReaderWithoutShareDeleteAndRetriesAfterClose(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, protectConfigDir(dir, true))
	target := filepath.Join(dir, "replace locked.conf")
	require.NoError(t, writeSecureConfigFile(target, []byte("old-value"), false, nil))
	reader := openWindowsSecureReadHandle(t, target, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE)

	err := writeSecureConfigFile(target, []byte("blocked-value"), true, nil)
	require.Error(t, err)
	unchanged, readErr := readSecureConfigFile(target)
	require.NoError(t, readErr)
	require.Equal(t, []byte("old-value"), unchanged)
	require.NoError(t, reader.Close())

	require.NoError(t, writeSecureConfigFile(target, []byte("after-close"), true, nil))
	updated, readErr := readSecureConfigFile(target)
	require.NoError(t, readErr)
	require.Equal(t, []byte("after-close"), updated)
	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	for _, entry := range entries {
		require.False(t, strings.HasPrefix(entry.Name(), secureTempPrefix), "temporary file remains: %s", entry.Name())
	}
}

func openWindowsSecureReadHandle(t *testing.T, path string, share uint32) *os.File {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	require.NoError(t, err)
	handle, err := windows.CreateFile(
		name,
		windows.FILE_GENERIC_READ,
		share,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	require.NoError(t, err)
	file := os.NewFile(uintptr(handle), path)
	require.NotNil(t, file)
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func readWindowsFileFromStart(file *os.File) ([]byte, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(file)
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
