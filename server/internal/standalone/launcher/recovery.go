package launcher

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"

	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

// RestoreStopped performs the protected offline recovery transaction for one
// stopped standalone installation. It returns only after the durable gate is
// awaiting local confirmation; it never confirms or removes that gate.
func RestoreStopped(ctx context.Context, installDir, recordingsDir string) (standalone.MaintenanceJournal, error) {
	var result standalone.MaintenanceJournal
	if runtime.GOOS != "windows" {
		return result, errors.New("Windows standalone recovery is unavailable")
	}
	release, err := standalone.LoadRelease(installDir)
	if err != nil {
		return result, err
	}
	paths, err := standalone.ResolvePaths(standalone.PathOptions{
		InstallDir: installDir, ConfigDir: filepath.Join(installDir, "config"), DataDir: filepath.Join(installDir, "data"),
		ResourceDir: release.ResourceDir, WebDir: release.WebDir, RecordingsDir: recordingsDir,
	})
	if err != nil {
		return result, err
	}
	return standalone.RestoreStopped(ctx, paths, runRecoveryMaintenanceChild, standalone.RecoverRedisStage)
}

func runRecoveryMaintenanceChild(ctx context.Context, paths standalone.Paths, operationID, purpose, version string) error {
	switch purpose {
	case "revoke_sessions", "db_check":
		return runMaintenanceChild(ctx, paths, operationID, purpose, version)
	default:
		return errors.New("unsupported recovery maintenance purpose")
	}
}
