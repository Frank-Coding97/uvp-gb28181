//go:build linux

package upgrade

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// openTrustedRegistration walks an absolute path from a root directory file
// descriptor. O_NOFOLLOW and openat keep path resolution pinned to checked
// directories even if an attacker races a name replacement between opens.
func openTrustedRegistration(path string) (*os.File, error) {
	parts, err := registrationPathComponents(path)
	if err != nil {
		return nil, err
	}
	dirFD, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(dirFD) }()
	// current 只用于报错定位。⛔ 现场只看得到「parent must not be group/world
	//   writable」时根本不知道是哪一层 —— 2026-10-20 实测就是靠人工逐层 stat
	//   才找出真正元凶（安装目录所在的两个上级目录是 775）。
	current := string(filepath.Separator)
	if err := validateTrustedRegistrationDirectoryFD(dirFD, current); err != nil {
		return nil, err
	}
	for _, part := range parts[:len(parts)-1] {
		nextFD, openErr := unix.Openat(dirFD, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if openErr != nil {
			return nil, openErr
		}
		current = filepath.Join(current, part)
		if checkErr := validateTrustedRegistrationDirectoryFD(nextFD, current); checkErr != nil {
			_ = unix.Close(nextFD)
			return nil, checkErr
		}
		_ = unix.Close(dirFD)
		dirFD = nextFD
	}
	fileFD, err := unix.Openat(dirFD, parts[len(parts)-1], unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fileFD, &stat); err != nil {
		_ = unix.Close(fileFD)
		return nil, err
	}
	if err := validateTrustedRegistrationFileStat(&stat); err != nil {
		_ = unix.Close(fileFD)
		return nil, err
	}
	file := os.NewFile(uintptr(fileFD), path)
	if file == nil {
		_ = unix.Close(fileFD)
		return nil, errors.New("wrap trusted updater registration descriptor")
	}
	return file, nil
}

func validateTrustedRegistrationDirectoryFD(fd int, path string) error {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return fmt.Errorf("updater registration parent %q is not a directory", path)
	}
	if stat.Mode&0022 != 0 {
		return fmt.Errorf("updater registration parent %q must not be group/world writable", path)
	}
	if !registrationOwnerTrusted(stat.Uid) {
		return fmt.Errorf("updater registration parent %q owner UID %d is not trusted", path, stat.Uid)
	}
	return nil
}

func validateTrustedRegistrationFileStat(stat *unix.Stat_t) error {
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return errors.New("updater registration must be a regular file, not a symlink")
	}
	if stat.Mode&0022 != 0 {
		return errors.New("updater registration must not be group/world writable")
	}
	if !registrationOwnerTrusted(stat.Uid) {
		return fmt.Errorf("updater registration owner UID %d is not trusted", stat.Uid)
	}
	return nil
}
