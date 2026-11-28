package processauthority

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"
	"unsafe"

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

// 新建锁文件必须显式授权「当前用户」全权，且 DACL 受保护（不继承父目录）。
//
// 背景（222 现场）：Windows 新建文件的默认 DACL 跟着**创建令牌**走 —— 用管理员
// 令牌创建时只有 Administrators/SYSTEM，用户之后用普通权限双击托盘（UAC 过滤令牌）
// 连打开锁都没权限，后端启动直接 phase=process_authority state_unavailable。
// 提权与不提权是同一个用户 SID，所以只要创建时把当前用户 SID 写进 DACL，
// 两种令牌都能读。这条断言把这个行为钉死，避免以后又被默认继承改回去。
func TestCreatedLockFileGrantsCurrentUserAndIsProtected(t *testing.T) {
	dir := privateStateDir(t)
	owner, err := AcquireLocalLock(dir)
	require.NoError(t, err)
	require.NoError(t, owner.Close())

	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	require.NoError(t, err)

	path := filepath.Join(dir, lockName)
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION)
	require.NoError(t, err)
	control, _, err := sd.Control()
	require.NoError(t, err)
	require.NotZero(t, control&windows.SE_DACL_PROTECTED, "锁文件的 DACL 必须是 protected，不能被父目录继承 ACE 稀释")

	dacl, _, err := sd.DACL()
	require.NoError(t, err)
	require.NotNil(t, dacl)

	granted := false
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		require.NoError(t, windows.GetAce(dacl, i, &ace))
		if ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		// ⛔ 用 SDDL 的 FA（= 标准权限 + SYNCHRONIZE + 0x1FF）逐位比对，不能拿
		//   GENERIC_ALL 比：FA 展开成具体权限位后 GENERIC_ALL(0x10000000) 并不置位。
		const fileAllAccess = uint32(windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1ff)
		if sid.Equals(user.User.Sid) && uint32(ace.Mask)&fileAllAccess == fileAllAccess {
			granted = true
		}
	}
	require.True(t, granted, "新建锁文件必须给当前用户全权，否则管理员创建后普通令牌读不了、后端起不来")
}
