//go:build !windows

package standalone

import (
	"fmt"
	"os"
)

func replaceCurrentPointerAtomically(tempPath, target string) error {
	if err := os.Rename(tempPath, target); err != nil {
		return fmt.Errorf("replace current pointer: %w", err)
	}
	return nil
}
