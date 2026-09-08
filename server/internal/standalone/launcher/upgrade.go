package launcher

import (
	"context"
	"path/filepath"

	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

// UpgradeStopped never starts the business backend or opens a browser. A
// successful transaction leaves the selected version ready for normal launch.
func UpgradeStopped(ctx context.Context, installDir, recordingsDir, candidateVersion, destination string) error {
	current, err := standalone.LoadRelease(installDir)
	if err != nil {
		return err
	}
	paths, err := standalone.ResolvePaths(standalone.PathOptions{
		InstallDir: installDir, ConfigDir: filepath.Join(installDir, "config"), DataDir: filepath.Join(installDir, "data"),
		ResourceDir: current.ResourceDir, WebDir: current.WebDir, RecordingsDir: recordingsDir,
	})
	if err != nil {
		return err
	}
	return standalone.UpgradeStopped(ctx, paths, candidateVersion, destination,
		func(ctx context.Context, paths standalone.Paths, operation, purpose, version string) error {
			if purpose == "candidate_health" {
				return runMaintenanceCandidateHealth(ctx, paths, operation, version)
			}
			return runMaintenanceChild(ctx, paths, operation, purpose, version)
		})
}
