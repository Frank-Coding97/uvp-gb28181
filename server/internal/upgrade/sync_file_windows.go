//go:build windows

package upgrade

import (
	"errors"
	"io/fs"
	"os"
)

// syncFile flushes file data through a write-capable handle.
//
// FlushFileBuffers fails with ERROR_ACCESS_DENIED on a handle opened without
// GENERIC_WRITE, which is why the Unix version cannot be reused here: os.Open
// is read-only. Reopening read-write is the only way to keep the flush. When
// that fails too — a read-only file, or one held by another process — we fall
// back to reporting success instead of failing an otherwise valid upgrade:
// the extracted bytes are already written, and NTFS flushes them lazily.
func syncFile(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrExist) {
			return nil
		}
		return err
	}
	defer file.Close()
	if err := file.Sync(); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return nil
		}
		return err
	}
	return nil
}
