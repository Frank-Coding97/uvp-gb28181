//go:build !windows

package upgrade

import "os"

// syncFile flushes file data so that a crash after we report success cannot
// resurrect the pre-rename content.
func syncFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
