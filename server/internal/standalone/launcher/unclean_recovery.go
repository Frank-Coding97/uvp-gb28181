package launcher

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"

	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

// RecoverUncleanStopped runs the offline recovery transaction for a stopped
// standalone installation. An empty snapshot is intentionally passed through
// for resume: the core accepts it only when an earlier recovery gate already
// owns the snapshot destination.
func RecoverUncleanStopped(ctx context.Context, installDir, recordingsDir, snapshot string) (standalone.UncleanRecoveryResult, error) {
	var result standalone.UncleanRecoveryResult
	if runtime.GOOS != "windows" {
		return result, errors.New("Windows standalone recovery is unavailable")
	}
	release, err := standalone.LoadRelease(installDir)
	if err != nil {
		return result, err
	}
	paths, err := standalone.ResolvePaths(standalone.PathOptions{
		InstallDir: installDir, ConfigDir: filepath.Join(installDir, "config"), DataDir: filepath.Join(installDir, "data"), ResourceDir: release.ResourceDir, WebDir: release.WebDir, RecordingsDir: recordingsDir,
	})
	if err != nil {
		return result, err
	}
	return standalone.RecoverUncleanStopped(ctx, paths, snapshot, runRecoveryMaintenanceChild, standalone.RecoverRedisStage)
}
