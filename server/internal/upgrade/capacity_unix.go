//go:build linux || darwin

package upgrade

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
)

func probeFilesystemCapacity(path string) (filesystemCapacity, error) {
	for {
		var stat unix.Stat_t
		err := unix.Stat(path, &stat)
		if errors.Is(err, os.ErrNotExist) {
			parent := filepath.Dir(path)
			if parent == path {
				return filesystemCapacity{}, err
			}
			path = parent
			continue
		}
		if err != nil {
			return filesystemCapacity{}, err
		}
		var fs unix.Statfs_t
		if err := unix.Statfs(path, &fs); err != nil {
			return filesystemCapacity{}, err
		}
		return filesystemCapacity{Device: uint64(stat.Dev), Available: uint64(fs.Bavail) * uint64(fs.Bsize), BlockSize: uint64(fs.Bsize)}, nil
	}
}
