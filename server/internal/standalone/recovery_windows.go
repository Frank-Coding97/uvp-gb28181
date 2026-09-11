//go:build windows

package standalone

import "context"

// RecoverRedisStage exposes the Windows-native disposable Redis runner to the
// launcher without exposing its implementation or any temporary credential.
func RecoverRedisStage(ctx context.Context, redisExe, sourceCopyDir, targetDir, controlDir string, indexDB int) error {
	return recoverRedisStage(ctx, redisExe, sourceCopyDir, targetDir, controlDir, indexDB)
}
