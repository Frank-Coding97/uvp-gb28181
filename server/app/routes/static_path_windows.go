//go:build windows

package routes

import (
	"errors"
	"path/filepath"

	"golang.org/x/sys/windows"
)

var errStaticReparsePoint = errors.New("static path contains a Windows reparse point")

func rejectStaticPathReparsePoints(candidate string) error {
	for current := filepath.Clean(candidate); ; current = filepath.Dir(current) {
		name, err := windows.UTF16PtrFromString(current)
		if err != nil {
			return err
		}
		attributes, err := windows.GetFileAttributes(name)
		if err == nil {
			if attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
				return errStaticReparsePoint
			}
		} else if !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) && !errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
			return err
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return nil
}
