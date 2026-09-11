//go:build windows

package standalone

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func replaceCurrentPointerAtomically(tempPath, target string) error {
	from, err := windows.UTF16PtrFromString(tempPath)
	if err != nil {
		return fmt.Errorf("encode current pointer temporary path: %w", err)
	}
	to, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return fmt.Errorf("encode current pointer path: %w", err)
	}
	flags := uint32(windows.MOVEFILE_REPLACE_EXISTING | windows.MOVEFILE_WRITE_THROUGH)
	if err := windows.MoveFileEx(from, to, flags); err != nil {
		return fmt.Errorf("replace current pointer: %w", err)
	}
	return nil
}
