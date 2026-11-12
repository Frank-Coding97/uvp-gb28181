//go:build !windows

package upgrade

import "os"

// syncDirectoryPath flushes the directory entry itself. Without it a rename
// survives in the file data but the name may not, so a crash could leave the
// old file in place after we reported success.
func syncDirectoryPath(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
