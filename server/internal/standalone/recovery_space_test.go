package standalone

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecoverySpaceBudgetAndNativeQuery(t *testing.T) {
	manifest := BackupManifest{Files: []BackupFile{{Path: "data/uvp.db", Size: 8192}, {Path: "config/config.yml", Size: 0}, {Path: "install/current.json", Size: math.MaxInt64}}}
	needed, err := recoverySpaceBudget(manifest)
	require.NoError(t, err)
	require.Equal(t, uint64(64<<20)+(8192+4096+4096)*3, needed)
	require.Error(t, checkRecoverySpace("fixture", manifest, func(string) (uint64, error) { return needed - 1, nil }))
	require.NoError(t, checkRecoverySpace("fixture", manifest, func(string) (uint64, error) { return needed, nil }))
	for _, size := range []int64{-1, math.MaxInt64} {
		_, err := recoverySpaceBudget(BackupManifest{Files: []BackupFile{{Path: "data/uvp.db", Size: size}}})
		require.Error(t, err)
	}
	free, err := recoveryAvailableSpace(t.TempDir())
	require.NoError(t, err)
	require.Greater(t, free, uint64(0))
	_, err = recoveryAvailableSpace(filepath.Join(t.TempDir(), "missing"))
	require.Error(t, err)
}

func TestRecoveryStageSpaceFailurePreservesOriginals(t *testing.T) {
	for _, mode := range []string{"full", "query-failed"} {
		t.Run(mode, func(t *testing.T) {
			owner := completedUpgradeFixture(t)
			root, op := owner.paths.InstallDir, owner.journal.OperationID
			require.NoError(t, advanceMaintenance(root, op, MaintenanceCommitting, MaintenanceRestoreRequired))
			paths := []string{owner.paths.ConfigDir, owner.paths.DataDir, owner.journal.BackupRoot}
			before := make([]string, len(paths))
			for i, path := range paths {
				var err error
				before[i], err = recoveryTreeIdentity(context.Background(), path)
				require.NoError(t, err)
			}
			called := false
			_, err := prepareRecoveryStageWithSpace(context.Background(), owner.paths, op, owner.trust,
				func(context.Context, Paths, string, string, string) error {
					t.Fatal("backend started with no space")
					return nil
				},
				func(context.Context, string, string, string, string, int) error {
					t.Fatal("Redis started with no space")
					return nil
				},
				func(path string) (uint64, error) {
					called = true
					require.Equal(t, filepath.Join(root, maintenanceDirName), path)
					if mode == "query-failed" {
						return 0, errors.New("space query unavailable")
					}
					return 0, nil
				})
			require.Error(t, err)
			require.True(t, called)
			for i, path := range paths {
				after, err := recoveryTreeIdentity(context.Background(), path)
				require.NoError(t, err)
				require.Equal(t, before[i], after)
			}
			journal, err := readRecoveryJournal(root)
			require.NoError(t, err)
			require.Equal(t, "staging", journal.Phase)
			require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)
		})
	}
}
