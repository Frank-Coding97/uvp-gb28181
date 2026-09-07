//go:build windows

package standalone

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	secureTempPrefix = ".uvp-secure-"
	secureLockName   = ".uvp-config.lock"
	secureLockWait   = 5 * time.Second
	// FILE_ALL_ACCESS for a file object: STANDARD_RIGHTS_REQUIRED,
	// SYNCHRONIZE, and the file-specific 0x1ff rights.
	windowsFileAllAccessMask windows.ACCESS_MASK = 0x001F01FF
)

var (
	errSecurePath        = errors.New("standalone: secure path rejected")
	errSecureACL         = errors.New("standalone: secure permissions rejected")
	errSecureLockTimeout = errors.New("standalone: configuration lock timed out")
)

func withConfigLock(dir string, fn func() error) error {
	if fn == nil {
		return errors.New("standalone: configuration lock callback is required")
	}
	if err := ensureWindowsDirectory(dir); err != nil {
		return err
	}
	lockPath := filepath.Join(dir, secureLockName)
	if _, err := ensureWindowsTarget(lockPath, true, false); err != nil {
		return err
	}
	deadline := time.Now().Add(secureLockWait)
	for {
		handle, err := openWindowsLock(lockPath)
		if err == nil {
			var overlapped windows.Overlapped
			err = windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
			if err == nil {
				callbackErr, unlockErr, closeErr := runWindowsLockedCallback(handle, &overlapped, fn)
				if callbackErr != nil {
					return callbackErr
				}
				if unlockErr != nil {
					return fmt.Errorf("unlock configuration directory %q: %w", dir, unlockErr)
				}
				if closeErr != nil {
					return fmt.Errorf("close configuration lock %q: %w", lockPath, closeErr)
				}
				return nil
			}
			_ = windows.CloseHandle(handle)
		}
		if !isWindowsLockContention(err) {
			return fmt.Errorf("acquire configuration lock %q: %w", lockPath, err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("acquire configuration lock %q: %w", dir, errSecureLockTimeout)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func runWindowsLockedCallback(handle windows.Handle, overlapped *windows.Overlapped, fn func() error) (callbackErr, unlockErr, closeErr error) {
	defer func() {
		unlockErr = windows.UnlockFileEx(handle, 0, 1, 0, overlapped)
		closeErr = windows.CloseHandle(handle)
	}()
	callbackErr = fn()
	return
}

func openWindowsLock(path string) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, fmt.Errorf("open configuration lock %q: %w", path, err)
	}
	return windows.CreateFile(
		name,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
}

func isWindowsLockContention(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}

func protectConfigDir(dir string, initializing bool) error {
	if err := ensureWindowsDirectory(dir); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("inspect configuration directory %q: %w", dir, err)
	}
	userSID, err := currentWindowsUserSID()
	if err != nil {
		return err
	}
	if initializing && len(entries) == 0 {
		if err := setProtectedACL(dir, userSID, true); err != nil {
			return fmt.Errorf("protect configuration directory %q: %w", dir, err)
		}
	}
	if err := validateProtectedACLPath(dir, userSID, true); err != nil {
		return fmt.Errorf("validate configuration directory %q: %w", dir, err)
	}
	return nil
}

func readSecureConfigFile(path string) ([]byte, error) {
	dir := filepath.Dir(path)
	if err := protectConfigDir(dir, false); err != nil {
		return nil, err
	}
	if _, err := ensureWindowsTarget(path, false, false); err != nil {
		return nil, err
	}
	userSID, err := currentWindowsUserSID()
	if err != nil {
		return nil, err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("open secure file %q: %w", path, err)
	}
	handle, err := windows.CreateFile(
		name,
		windows.FILE_GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("open secure file %q: %w", path, err)
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("open secure file %q: %w", path, errSecurePath)
	}
	info := windows.ByHandleFileInformation{}
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspect secure file %q: %w", path, err)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		_ = file.Close()
		return nil, fmt.Errorf("secure file %q is not a regular file: %w", path, errSecurePath)
	}
	if err := validateProtectedACLHandle(handle, userSID, false); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("validate secure file %q: %w", path, err)
	}
	data, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read secure file %q: %w", path, readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close secure file %q: %w", path, closeErr)
	}
	if _, err := ensureWindowsTarget(path, false, false); err != nil {
		return nil, err
	}
	if err := validateProtectedACLPath(path, userSID, false); err != nil {
		return nil, fmt.Errorf("validate secure file %q: %w", path, err)
	}
	return data, nil
}

func writeSecureConfigFile(path string, data []byte, replace bool, hook func(stage string) error) error {
	dir := filepath.Dir(path)
	if err := protectConfigDir(dir, false); err != nil {
		return err
	}
	exists, err := ensureWindowsTarget(path, true, false)
	if err != nil {
		return err
	}
	if exists && !replace {
		return fs.ErrExist
	}
	userSID, err := currentWindowsUserSID()
	if err != nil {
		return err
	}
	if exists {
		if err := validateProtectedACLPath(path, userSID, false); err != nil {
			return fmt.Errorf("validate existing secure file %q: %w", path, err)
		}
	}
	tempPath, tempHandle, err := createWindowsTempFile(dir, path, userSID, hook)
	if err != nil {
		return err
	}
	tempFile := os.NewFile(uintptr(tempHandle), tempPath)
	if tempFile == nil {
		_ = windows.CloseHandle(tempHandle)
		_ = os.Remove(tempPath)
		return fmt.Errorf("open secure temporary file %q: %w", path, errSecurePath)
	}
	removeTemp := true
	defer func() {
		_ = tempFile.Close()
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if err := callSecureHook(hook, "write", path); err != nil {
		return err
	}
	n, err := tempFile.Write(data)
	if err != nil {
		return fmt.Errorf("write secure file %q: %w", path, err)
	}
	if n != len(data) {
		return fmt.Errorf("write secure file %q: %w", path, io.ErrShortWrite)
	}
	if err := callSecureHook(hook, "flush", path); err != nil {
		return err
	}
	if err := windows.FlushFileBuffers(tempHandle); err != nil {
		return fmt.Errorf("flush secure file %q: %w", path, err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close secure file %q: %w", path, err)
	}
	if err := callSecureHook(hook, "acl", path); err != nil {
		return err
	}
	if err := setProtectedACL(tempPath, userSID, false); err != nil {
		return fmt.Errorf("protect secure file %q: %w", path, err)
	}
	if _, err := ensureWindowsTarget(tempPath, false, false); err != nil {
		return err
	}
	if err := validateProtectedACLPath(tempPath, userSID, false); err != nil {
		return fmt.Errorf("validate temporary secure file %q: %w", path, err)
	}

	// Recheck both names immediately before publication. A same-privilege
	// attacker racing these checks is outside this helper's claim.
	if err := protectConfigDir(dir, false); err != nil {
		return err
	}
	if exists, err := ensureWindowsTarget(path, true, false); err != nil {
		return err
	} else if exists && !replace {
		return fs.ErrExist
	}
	if err := callSecureHook(hook, "publish", path); err != nil {
		return err
	}
	from, err := windows.UTF16PtrFromString(tempPath)
	if err != nil {
		return fmt.Errorf("publish secure file %q: %w", path, err)
	}
	to, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("publish secure file %q: %w", path, err)
	}
	flags := uint32(windows.MOVEFILE_WRITE_THROUGH)
	if replace {
		flags |= windows.MOVEFILE_REPLACE_EXISTING
	}
	if err := windows.MoveFileEx(from, to, flags); err != nil {
		if !replace && isWindowsAlreadyExists(err) {
			return fs.ErrExist
		}
		return fmt.Errorf("publish secure file %q: %w", path, err)
	}
	removeTemp = false
	if _, err := ensureWindowsTarget(path, false, false); err != nil {
		return err
	}
	if err := validateProtectedACLPath(path, userSID, false); err != nil {
		return fmt.Errorf("validate published secure file %q: %w", path, err)
	}
	return nil
}

func createWindowsTempFile(dir, target string, userSID *windows.SID, hook func(stage string) error) (string, windows.Handle, error) {
	securityDescriptor, securityAttributes, err := protectedFileSecurityAttributes(userSID)
	if err != nil {
		return "", windows.InvalidHandle, err
	}
	// Keep the descriptor alive until CreateFile returns; SecurityAttributes
	// points into it through the native call.
	_ = securityDescriptor
	for attempt := 0; attempt < 16; attempt++ {
		name, err := randomSecureTempName()
		if err != nil {
			return "", windows.InvalidHandle, errors.New("generate secure temporary name")
		}
		path := filepath.Join(dir, name)
		if err := callSecureHook(hook, "create", target); err != nil {
			return "", windows.InvalidHandle, err
		}
		namePtr, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return "", windows.InvalidHandle, fmt.Errorf("create secure temporary file %q: %w", target, err)
		}
		handle, err := windows.CreateFile(
			namePtr,
			windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE,
			0,
			securityAttributes,
			windows.CREATE_NEW,
			windows.FILE_ATTRIBUTE_NORMAL,
			0,
		)
		if err == nil {
			return path, handle, nil
		}
		if !isWindowsAlreadyExists(err) {
			return "", windows.InvalidHandle, fmt.Errorf("create secure temporary file %q: %w", target, err)
		}
	}
	return "", windows.InvalidHandle, errors.New("create secure temporary file: random name collision")
}

func randomSecureTempName() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return secureTempPrefix + hex.EncodeToString(random[:]), nil
}

func callSecureHook(hook func(stage string) error, stage, path string) error {
	if hook == nil {
		return nil
	}
	if err := hook(stage); err != nil {
		// Never wrap callback text: a callback may have seen configuration data.
		return fmt.Errorf("secure file %q %s hook failed", path, stage)
	}
	return nil
}

func ensureWindowsDirectory(path string) error {
	exists, err := ensureWindowsTarget(path, false, true)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("secure directory %q: %w", path, fs.ErrNotExist)
	}
	return nil
}

func ensureWindowsTarget(path string, allowMissing, directory bool) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if allowMissing && errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("inspect secure path %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("secure path %q is a symbolic link: %w", path, errSecurePath)
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false, fmt.Errorf("inspect secure path %q: %w", path, err)
	}
	attrs, err := windows.GetFileAttributes(name)
	if err != nil {
		return false, fmt.Errorf("inspect secure path %q: %w", path, err)
	}
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return false, fmt.Errorf("secure path %q is a reparse point: %w", path, errSecurePath)
	}
	if directory {
		if !info.IsDir() || attrs&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
			return false, fmt.Errorf("secure path %q is not a directory: %w", path, errSecurePath)
		}
	} else if info.IsDir() || attrs&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return false, fmt.Errorf("secure path %q is not a regular file: %w", path, errSecurePath)
	}
	return true, nil
}

func currentWindowsUserSID() (*windows.SID, error) {
	token := windows.GetCurrentThreadEffectiveToken()
	user, err := token.GetTokenUser()
	if err == nil {
		return user.User.Sid.Copy()
	}
	if !errors.Is(err, windows.ERROR_NO_TOKEN) {
		return nil, errors.New("query effective Windows user token")
	}
	var processToken windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &processToken); err != nil {
		return nil, errors.New("query process Windows user token")
	}
	defer processToken.Close()
	user, err = processToken.GetTokenUser()
	if err != nil {
		return nil, errors.New("query process Windows user token")
	}
	return user.User.Sid.Copy()
}

func protectedFileSecurityAttributes(userSID *windows.SID) (*windows.SECURITY_DESCRIPTOR, *windows.SecurityAttributes, error) {
	if userSID == nil || !userSID.IsValid() {
		return nil, nil, errors.New("invalid effective Windows user SID")
	}
	// Use the file-specific full-access mask. A generic inherited ACE is
	// expanded by Windows into a direct FILE_ALL_ACCESS ACE plus an
	// inherit-only generic ACE; the file-specific mask remains one direct OI/CI
	// ACE for each principal.
	sddl := fmt.Sprintf("D:P(A;;FA;;;%s)(A;;FA;;;SY)", userSID.String())
	descriptor, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return nil, nil, errors.New("build protected file security descriptor")
	}
	attributes := &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: descriptor,
	}
	return descriptor, attributes, nil
}

func setProtectedACL(path string, userSID *windows.SID, directory bool) error {
	if userSID == nil || !userSID.IsValid() {
		return errors.New("invalid effective Windows user SID")
	}
	systemSID, err := windows.StringToSid("S-1-5-18")
	if err != nil {
		return errors.New("build SYSTEM SID")
	}
	// Keep directory entries inheritable so SQLite/Redis child objects receive
	// the same protected principals. FILE_ALL_ACCESS avoids the generic-mask
	// split that would otherwise create inherit-only ACEs.
	inheritance := uint32(windows.NO_INHERITANCE)
	if directory {
		inheritance = windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT
	}
	entries := []windows.EXPLICIT_ACCESS{
		{
			AccessPermissions: windowsFileAllAccessMask,
			AccessMode:        windows.SET_ACCESS,
			Inheritance:       inheritance,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_USER,
				TrusteeValue: windows.TrusteeValueFromSID(userSID),
			},
		},
		{
			AccessPermissions: windowsFileAllAccessMask,
			AccessMode:        windows.SET_ACCESS,
			Inheritance:       inheritance,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_WELL_KNOWN_GROUP,
				TrusteeValue: windows.TrusteeValueFromSID(systemSID),
			},
		},
	}
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		return errors.New("build protected Windows ACL")
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	); err != nil {
		return errors.New("set protected Windows ACL")
	}
	return nil
}

func validateProtectedACLPath(path string, userSID *windows.SID, directory bool) error {
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION,
	)
	if err != nil {
		return errors.New("read Windows ACL")
	}
	return validateProtectedACL(descriptor, userSID, directory)
}

func validateProtectedACLHandle(handle windows.Handle, userSID *windows.SID, directory bool) error {
	descriptor, err := windows.GetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION,
	)
	if err != nil {
		return errors.New("read Windows ACL")
	}
	return validateProtectedACL(descriptor, userSID, directory)
}

func validateProtectedACL(descriptor *windows.SECURITY_DESCRIPTOR, userSID *windows.SID, directory bool) error {
	if descriptor == nil || userSID == nil || !userSID.IsValid() {
		return errSecureACL
	}
	control, _, err := descriptor.Control()
	if err != nil || control&windows.SE_DACL_PRESENT == 0 || control&windows.SE_DACL_PROTECTED == 0 {
		return errSecureACL
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil || dacl.AceCount != 2 {
		return errSecureACL
	}
	systemSID, err := windows.StringToSid("S-1-5-18")
	if err != nil {
		return errSecureACL
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil {
		return errSecureACL
	}
	administratorsSID, err := windows.StringToSid("S-1-5-32-544")
	if err != nil {
		return errSecureACL
	}
	ownerKey := owner.String()
	if ownerKey != userSID.String() && ownerKey != systemSID.String() && ownerKey != administratorsSID.String() {
		return errSecureACL
	}
	expected := map[string]bool{userSID.String(): false, systemSID.String(): false}
	var expectedFlags uint8
	if directory {
		expectedFlags = windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE
	}
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil || ace == nil {
			return errSecureACL
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags != expectedFlags || ace.Mask != windowsFileAllAccessMask {
			return errSecureACL
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !sid.IsValid() {
			return errSecureACL
		}
		key := sid.String()
		if _, ok := expected[key]; !ok || expected[key] {
			return errSecureACL
		}
		expected[key] = true
	}
	for _, found := range expected {
		if !found {
			return errSecureACL
		}
	}
	return nil
}

func isWindowsAlreadyExists(err error) bool {
	return errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS)
}
