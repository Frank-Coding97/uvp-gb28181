//go:build linux || darwin

package upgrade

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func verifySharedGateFile(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("configured updater gate is not a regular file")
	}
	if info.Mode().Perm() != 0640 {
		return fmt.Errorf("configured updater gate mode must be 0640, got %04o", info.Mode().Perm())
	}
	return nil
}

func verifySharedGateParent(info os.FileInfo) error {
	if info == nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("shared updater gate parent is not a real directory")
	}
	if info.Mode().Perm() != 0750 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return fmt.Errorf("shared updater gate parent mode must be 0750, got %04o", info.Mode().Perm())
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("shared gate parent ownership is unavailable")
	}
	if stat.Uid != 0 && stat.Uid != uint32(os.Geteuid()) {
		return errors.New("shared updater gate parent owner is not trusted")
	}
	return nil
}

// VerifySharedGateParent lets the privileged helper reject an existing unsafe
// parent before it opens or creates the shared gate file.
func VerifySharedGateParent(info os.FileInfo) error {
	return verifySharedGateParent(info)
}

func inheritSharedGateGroup(file *os.File, parent os.FileInfo) error {
	if file == nil || parent == nil {
		return errors.New("shared gate ownership is unavailable")
	}
	// os.FileInfo.Sys uses syscall.Stat_t on both supported host platforms.
	// x/sys/unix.Stat_t is not the same concrete type on Darwin.
	stat, ok := parent.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("shared gate parent ownership is unavailable")
	}
	if err := unix.Fchown(int(file.Fd()), -1, int(stat.Gid)); err != nil {
		return fmt.Errorf("set shared gate group: %w", err)
	}
	return nil
}
