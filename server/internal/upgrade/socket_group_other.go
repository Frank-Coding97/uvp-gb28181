//go:build !linux && !darwin

package upgrade

import "fmt"

func setSocketGroup(_ string, gid int) error {
	return fmt.Errorf("setting updater socket group %d is unsupported on this platform", gid)
}
