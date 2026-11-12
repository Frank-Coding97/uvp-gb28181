//go:build darwin

package upgrade

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// macOS development installs run unprivileged. Once every parent has a
// trusted owner and no group/other write permission, an untrusted business
// peer cannot replace the checked path between lstat and open.
func openTrustedRegistration(path string) (*os.File, error) {
	parts, err := registrationPathComponents(path)
	if err != nil {
		return nil, err
	}
	current := string(filepath.Separator)
	if err := validateTrustedRegistrationInfo(current, true); err != nil {
		return nil, err
	}
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		if err := validateTrustedRegistrationInfo(current, true); err != nil {
			return nil, err
		}
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if err := validateTrustedRegistrationFileInfo(before); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		_ = file.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("updater registration changed while opening")
	}
	if err := validateTrustedRegistrationFileInfo(after); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func validateTrustedRegistrationInfo(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || directory && !info.IsDir() {
		return fmt.Errorf("updater registration parent %q is not a real directory", path)
	}
	if info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("updater registration parent %q must not be group/world writable", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !registrationOwnerTrusted(stat.Uid) {
		return fmt.Errorf("updater registration parent %q owner is not trusted", path)
	}
	return nil
}

func validateTrustedRegistrationFileInfo(info os.FileInfo) error {
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("updater registration must be a regular file, not a symlink")
	}
	if info.Mode().Perm()&0022 != 0 {
		return errors.New("updater registration must not be group/world writable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !registrationOwnerTrusted(stat.Uid) {
		return fmt.Errorf("updater registration owner is not trusted")
	}
	return nil
}
