//go:build windows

package logging

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// logDirectory 是「日志目录」在 Windows 上的实现，接口与 Unix 版
// （writer_fs_unix.go）**逐个方法对齐**，这样 writer.go 一行都不用改。
//
// ⛔⛔ 与 Unix 版的差异**全是平台能力差异**，不是偷懒；改之前先读这段：
//   - 目录句柄：Unix 用 `openat/fstatat/renameat2` 把「查身份」与「动文件」绑在同一个
//     目录 fd 上（防 TOCTOU）；Windows 没有这一层 ⇒ 改为
//     「Lstat 判 reparse point 拒软链 → 打开句柄复核 VolumeSerialNumber+FileIndex」，
//     并在动文件前**再用路径复核一次身份**（matches）。
//   - 重命名不覆盖：Unix 用 `RENAME_NOREPLACE`；Windows 的 `MoveFileEx` 只要**不带**
//     `MOVEFILE_REPLACE_EXISTING` 就天然不覆盖目标，语义等价。
//   - 排他锁：Unix `flock(LOCK_EX|LOCK_NB)`；Windows `LockFileEx(FAIL_IMMEDIATELY)`
//     （锁第 0 字节区间，非阻塞）。
//   - **目录 fsync**：Windows 不支持对目录句柄 FlushFileBuffers ⇒ 改用
//     `MOVEFILE_WRITE_THROUGH` 提供写入穿透，`sync()` 只跑钩子。
//   - ⚠️ **刻意放弃** Unix 的 `mode&0022 == 0`（属主独占可写）检查：Windows 没有
//     POSIX 权限位，等价的「ACL 是否对他人可写」无法在 O(1) 内可靠判定。
//     代偿：仍然强制 `NumberOfLinks == 1`（拒硬链接）+ 拒 reparse point（拒软链）。
//     ⇒ 部署要求写成「日志目录不得由非管理员账户写入」，见 README。
type logDirectory struct {
	path       string
	file       *os.File
	beforeSync func() error
}

// 与 Unix 版同名常量，但取值是本平台自己的位标志（Unix 直接借用了 O_* 的真值）。
const (
	logRead      = 1 << 0
	logAppend    = 1 << 1
	logCreate    = 1 << 2
	logExclusive = 1 << 3
)

type fileIdentity struct{ dev, ino uint64 }

// logStat 只喂给 writer.go 的 `s.Size` 与 identity()，字段名不必与 unix.Stat_t 对齐。
type logStat struct {
	Size int64
	dev  uint64
	ino  uint64
}

func identity(s logStat) fileIdentity { return fileIdentity{s.dev, s.ino} }

func openLogDirectory(path string) (*logDirectory, error) {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	return &logDirectory{path: abs, file: f}, nil
}

func leaf(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\")
}

func (d *logDirectory) join(name string) string { return filepath.Join(d.path, name) }

// attr 取句柄/路径上的文件属性；非 Windows 属性结构时返回 0（不判 reparse）。
func reparsePoint(raw any) bool {
	attr, ok := raw.(*syscall.Win32FileAttributeData)
	if !ok || attr == nil {
		return false
	}
	return attr.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func (d *logDirectory) stat(name string) (logStat, error) {
	if !leaf(name) {
		return logStat{}, errUnsafeLogFile
	}
	fi, err := os.Lstat(d.join(name))
	if err != nil {
		return logStat{}, err
	}
	if !fi.Mode().IsRegular() || fi.Mode()&os.ModeSymlink != 0 || reparsePoint(fi.Sys()) {
		return logStat{}, errUnsafeLogFile
	}
	f, err := os.OpenFile(d.join(name), os.O_RDONLY, 0)
	if err != nil {
		return logStat{}, err
	}
	defer func() { _ = f.Close() }()
	id, err := openedIdentity(f)
	if err != nil {
		return logStat{}, err
	}
	if err := safeHandle(windows.Handle(f.Fd())); err != nil {
		return logStat{}, err
	}
	return logStat{Size: fi.Size(), dev: id.dev, ino: id.ino}, nil
}

// safeHandle 复核「这个句柄指向的确实是普通文件、且不是硬链接/reparse point」。
// ⛔ writer.go 的安全不变量都建立在「身份只能有一个名字」上，NumberOfLinks==1 是
//   Windows 上唯一能廉价拿到的等价判据。
func safeHandle(h windows.Handle) error {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return err
	}
	if info.NumberOfLinks != 1 {
		return errUnsafeLogFile
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errUnsafeLogFile
	}
	return nil
}

func openedIdentity(f *os.File) (fileIdentity, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); err != nil {
		return fileIdentity{}, err
	}
	ino := uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)
	return fileIdentity{uint64(info.VolumeSerialNumber), ino}, nil
}

func (d *logDirectory) open(name string, flags int) (*os.File, error) {
	if !leaf(name) {
		return nil, errUnsafeLogFile
	}
	perm := os.O_RDONLY
	switch {
	case flags&logAppend != 0:
		perm = os.O_WRONLY | os.O_APPEND
		if flags&logCreate != 0 {
			perm |= os.O_CREATE
		}
		if flags&logExclusive != 0 {
			perm |= os.O_EXCL
		}
	case flags&logRead != 0:
		perm = os.O_RDONLY
	}
	f, err := os.OpenFile(d.join(name), perm, 0o600)
	if err != nil {
		return nil, err
	}
	// ⛔ 打开后再按**句柄**复核一次：挡住「检查路径」与「打开」之间被换成
	//   硬链接/reparse point 的窗口（对应 Unix 的 O_NOFOLLOW + Fstat）。
	if err := safeHandle(windows.Handle(f.Fd())); err != nil {
		_ = f.Close()
		return nil, errors.Join(errUnsafeLogFile, err)
	}
	return f, nil
}

func (d *logDirectory) matches(name string, f *os.File) error {
	current, err := d.stat(name)
	if err != nil {
		return err
	}
	opened, err := openedIdentity(f)
	if err != nil {
		return err
	}
	if current.dev != opened.dev || current.ino != opened.ino {
		return errUnsafeLogFile
	}
	return nil
}

func (d *logDirectory) rename(from, to string, expected *os.File) error {
	if !leaf(to) {
		return errUnsafeLogFile
	}
	if err := d.matches(from, expected); err != nil {
		return err
	}
	// ⛔ 不带 REPLACE_EXISTING ⇒ 目标已存在时直接失败（等价 RENAME_NOREPLACE）；
	//   WRITE_THROUGH 用来补「Windows 不能 fsync 目录」这一块。
	err := windows.MoveFileEx(
		windows.StringToUTF16Ptr(d.join(from)),
		windows.StringToUTF16Ptr(d.join(to)),
		windows.MOVEFILE_WRITE_THROUGH,
	)
	if err != nil {
		return err
	}
	if err := d.matches(to, expected); err != nil {
		return err
	}
	return d.sync()
}

func (d *logDirectory) remove(name string, expected fileIdentity) error {
	f, err := d.open(name, logRead)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	actual, err := openedIdentity(f)
	if err != nil {
		return err
	}
	if actual != expected {
		return errUnsafeLogFile
	}
	if err := d.matches(name, f); err != nil {
		return err
	}
	// ⚠️ 已知差异：Windows 没有 unlinkat，这里按**路径**删除。安全性由上面两次
	//   身份复核 + 「日志目录不可被非管理员写入」的部署要求共同保证。
	if err := os.Remove(d.join(name)); err != nil {
		return err
	}
	return d.sync()
}

func (d *logDirectory) entries() ([]os.DirEntry, error) {
	f, err := os.Open(d.path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return f.ReadDir(-1)
}

func lockLogFile(f *os.File) error {
	var overlapped windows.Overlapped
	return windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, &overlapped,
	)
}

func (d *logDirectory) sync() error {
	if d.beforeSync != nil {
		if err := d.beforeSync(); err != nil {
			return err
		}
	}
	// Windows 不能 fsync 目录（FlushFileBuffers 对目录句柄返回 ERROR_ACCESS_DENIED），
	// 目录项的持久化由 rename 的 MOVEFILE_WRITE_THROUGH 承担。
	return nil
}
