//go:build !windows && !darwin && !linux

package standalone

import "errors"

func backupPublish(source, target string) error {
	return errors.New("backup publication unsupported on this platform")
}
