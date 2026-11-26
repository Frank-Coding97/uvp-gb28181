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

// hardenCreatedLocalFile 把新建锁文件的 DACL 固定成「当前用户 + 本机管理员 + 系统」。
//
// 为什么必须显式设置：Windows 新建文件的默认 DACL 来自**创建进程的令牌**。用管理员
// 令牌（提权）创建时只会落下 Administrators / SYSTEM 两个条目 —— 之后用户用普通
// 权限双击托盘（UAC 过滤令牌），后端连打开这个锁都没权限，启动直接失败为
// phase=process_authority reason=state_unavailable（222 现场实测：锁属主
// BUILTIN\Administrators，普通用户 icacls / Get-Content 全部 Access denied）。
//
// 提权与不提权是**同一个用户 SID**，所以把当前用户 SID 写进 DACL 后两种令牌都能读；
// 同时仍只放本机三个可信主体，Everyone 之类一律不出现 —— 与 secureLocalFile 的
// trusted 判据、local_acl_windows_test 的标准形态（P + SY + BA + 当前用户）保持一致。
//
// 只对**新建**文件调用：已存在的锁禁止改写（AcquireLocalLock 的契约是绝不修复既有
// 状态，能走到打开它说明当前令牌本来就有权限）。
func hardenCreatedLocalFile(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString(fmt.Sprintf(
		"D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FA;;;%s)", user.User.Sid.String()))
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	// ⛔⛔ 必须用**按路径**的 SetNamedSecurityInfo，不能用句柄版 SetSecurityInfo：
	//   os.OpenFile 拿到的句柄只有 GENERIC_READ|GENERIC_WRITE，没有 WRITE_DAC，
	//   句柄版会直接 ERROR_ACCESS_DENIED（Windows 单测实测），而创建者对自有文件
	//   通过路径改 DACL 有隐式权限。目标路径由已校验的私有目录 + 固定文件名拼出，
	//   紧接着 secureLocalFile 还会按句柄复核身份与 DACL。
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil)
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

// Windows volume serial numbers and file indexes are stable across reboots,
// so the record keeps all three fields and is compared verbatim.
func localIdentityMatch(recorded, live string) bool { return recorded == live }

// File.Sync flushes the fixed domain record. Directory handles opened for
// reading cannot be flushed on Windows. Loss of the directory entry must fail
// DB domain binding on next start, never authorize automatic domain recovery.
func syncLocalDirectory(*os.File) error { return nil }
