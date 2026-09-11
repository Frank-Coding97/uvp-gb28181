//go:build !windows

package standalone

import (
	"context"
	"errors"
)

// RecoverRedisStage is unavailable outside the Windows standalone package.
// Keeping the adapter present lets the command package retain one safe API;
// the executable itself rejects non-Windows startup before dispatch.
func RecoverRedisStage(ctx context.Context, redisExe, sourceCopyDir, targetDir, controlDir string, indexDB int) error {
	return errors.New("Windows recovery Redis runner is unavailable")
}
