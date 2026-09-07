package standalone

import (
	"fmt"
	"golang.org/x/sys/windows"
	"path/filepath"
)

func validateLocalVolume(path string) error {
	// Junctions are reparse points even when filepath.EvalSymlinks leaves
	// their spelling unchanged. Reject them, including parent directories.
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		name, err := windows.UTF16PtrFromString(current)
		if err != nil {
			return err
		}
		attributes, err := windows.GetFileAttributes(name)
		if err != nil {
			return err
		}
		if attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return ErrPathSymlink
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	if isUNCPath(resolved) {
		return ErrUNCPath
	}
	root, err := windows.UTF16PtrFromString(filepath.VolumeName(resolved) + `\`)
	if err != nil {
		return err
	}
	switch windows.GetDriveType(root) {
	case windows.DRIVE_FIXED, windows.DRIVE_REMOVABLE, windows.DRIVE_RAMDISK:
		return nil
	default:
		return fmt.Errorf("standalone: local writable volume required")
	}
}
