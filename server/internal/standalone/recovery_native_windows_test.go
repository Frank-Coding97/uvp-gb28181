//go:build windows

package standalone

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWindowsRecoveryCoordinatorRealComponents(t *testing.T) {
	backend := maintenanceBackendTestPath(t)
	fixture := newMaintenanceComponentFixture(t, backend, maintenanceComponentReleaseRoot(t))
	currentBackend := filepath.Join(fixture.current.releaseDir, filepath.FromSlash(releaseBackendPath))
	copyMaintenanceComponentFile(t, backend, currentBackend)
	fixture.current.manifest = rewriteMaintenanceComponentManifest(t, fixture.current.releaseDir, fixture.current.manifest.Version, fixture.current.manifest.SourceCommit)
	seedUpgradeDatabase(t, fixture)
	seedUpgradeRedis(t, fixture)
	removeUpgradeDriverGate(t, fixture.paths.InstallDir)
	require.NoError(t, fixture.lock.Close())
	digest, err := releaseFileSHA256(backend)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	owner, err := prepareUpgradeStoppedWithTrust(ctx, fixture.paths, fixture.journal.CandidateVersion, filepath.Join(filepath.Dir(fixture.paths.InstallDir), "real-recovery-backup"), digest)
	require.NoError(t, err)
	require.NoError(t, advanceMaintenance(fixture.paths.InstallDir, owner.journal.OperationID, MaintenanceUpgrading, MaintenanceRestoreRequired))
	require.NoError(t, owner.Close())
	before, err := LoadConfig(fixture.paths)
	require.NoError(t, err)
	result, err := restoreStoppedWithRunners(ctx, fixture.paths, owner.journal.OperationID, digest,
		func(ctx context.Context, paths Paths, op, purpose, version string) error {
			frame, err := IssueMaintenancePermit(paths.InstallDir, op, purpose, version)
			if err != nil {
				return err
			}
			defer clear(frame)
			args := []string{}
			if purpose == "db_check" {
				args = []string{"-db-check"}
			}
			release, err := LoadReleaseVersion(paths.InstallDir, version)
			if err != nil {
				return err
			}
			output := runMaintenanceBackend(t, release.BackendExe, paths, args, frame)
			assertUpgradeDriverMaintenanceSuccess(t, output, frame, purpose, version, paths.InstallDir)
			return nil
		}, recoverRedisStage)
	require.NoError(t, err)
	require.Equal(t, MaintenanceAwaitingConfirmation, result.Phase)
	require.ErrorIs(t, CheckMaintenanceGate(fixture.paths.InstallDir), ErrMaintenanceRequired)
	after, err := LoadConfig(fixture.paths)
	require.NoError(t, err)
	require.NotEqual(t, before.JWTSecret(), after.JWTSecret())
	require.NotEqual(t, before.InstanceGeneration(), after.InstanceGeneration())
	require.Equal(t, before.RedisPassword(), after.RedisPassword())
	selected, err := LoadRelease(fixture.paths.InstallDir)
	require.NoError(t, err)
	require.Equal(t, owner.journal.OldVersion, selected.Version)
	require.NoError(t, backupComponentsStopped(selected))
	entries, err := os.ReadDir(filepath.Join(fixture.paths.DataDir, "redis"))
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	// Current/candidate use one backend build here. This verifies real offline
	// component wiring, not two-version compatibility or historical-token E2E.
}
