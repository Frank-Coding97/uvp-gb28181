//go:build !windows

package standalone

import "os"

func backupOpenSource(path string, exclusive bool) (*os.File, error) {
	if _, err := ensureOtherTarget(path, false, false); err != nil {
		return nil, err
	}
	return os.Open(path)
}
