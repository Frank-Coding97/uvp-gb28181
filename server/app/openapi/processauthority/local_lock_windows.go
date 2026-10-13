package processauthority

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func lockLocalFile(file *os.File) error {
	err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, ^uint32(0), ^uint32(0), &windows.Overlapped{})
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return ErrLocalAuthorityBusy
	}
	if err != nil {
		return ErrLocalAuthorityUnavailable
	}
	return nil
}

func secureLocalFile(file *os.File, directory bool) bool {
	handle := windows.Handle(file.Fd())
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(handle, &info) != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return false
	}
	if directory != (info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) || (!directory && info.NumberOfLinks != 1) {
		return false
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return false
	}
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	owner, _, err := sd.Owner()
	trusted := func(sid *windows.SID) bool {
		return sid != nil && (sid.Equals(user.User.Sid) || sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid))
	}
	if err != nil || !trusted(owner) {
		return false
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		return false
	}
	const writes = windows.GENERIC_ALL | windows.GENERIC_WRITE | windows.DELETE | windows.WRITE_DAC | windows.WRITE_OWNER | 0x156
	prohibited := uint32(writes)
	if !directory {
		// A read-only handle is sufficient to take LockFileEx and deny the
		// service ownership, so the fixed lock file must also be private.
		prohibited |= windows.GENERIC_READ | windows.FILE_READ_DATA
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(dacl, i, &ace) != nil || ace == nil {
			return false
		}
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			continue
		}
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceSize < uint16(unsafe.Sizeof(*ace)) {
			return false
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if uint32(ace.Mask)&prohibited != 0 && !trusted(sid) {
			return false
		}
	}
	var fs [32]uint16
	if windows.GetVolumeInformationByHandle(handle, nil, 0, nil, nil, nil, &fs[0], uint32(len(fs))) != nil {
		return false
	}
	name := windows.UTF16ToString(fs[:])
	if name != "NTFS" && name != "ReFS" {
		return false
	}
	var finalPath, volumePath [32768]uint16
	n, err := windows.GetFinalPathNameByHandle(handle, &finalPath[0], uint32(len(finalPath)), 0)
	if err != nil || n == 0 || n >= uint32(len(finalPath)) {
		return false
	}
	if windows.GetVolumePathName(&finalPath[0], &volumePath[0], uint32(len(volumePath))) != nil {
		return false
	}
	return windows.GetDriveType(&volumePath[0]) == windows.DRIVE_FIXED
}

func localFileIdentity(file *os.File) (string, error) {
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info) != nil {
		return "", ErrLocalAuthorityUnavailable
	}
	return fmt.Sprintf("windows:%x:%x:%x", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow), nil
}

// File.Sync flushes the fixed domain record. Directory handles opened for
// reading cannot be flushed on Windows. Loss of the directory entry must fail
// DB domain binding on next start, never authorize automatic domain recovery.
func syncLocalDirectory(*os.File) error { return nil }
