package standalone

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPreparedUpgradeExecutesBeforeCurrentCommit(t *testing.T) {
	for _, mode := range []string{"complete", "migrate-failure", "health-failure", "cancel", "closed", "release-changed", "backup-changed", "cancel-after-migrate", "release-changed-after-migrate"} {
		t.Run(mode, func(t *testing.T) {
			paths := newBackupTestPaths(t)
			candidate := newTestReleaseFixtureAt(t, paths.InstallDir, "t25.2", false)
			current, err := LoadRelease(paths.InstallDir)
			require.NoError(t, err)
			oldHash, err := releaseFileSHA256(current.BackendExe)
			require.NoError(t, err)
			candidatePath := filepath.Join(candidate.releaseDir, filepath.FromSlash(releaseBackendPath))
			newHash, err := releaseFileSHA256(candidatePath)
			require.NoError(t, err)
			backup := filepath.Join(filepath.Dir(paths.InstallDir), "execute-backup")
			prepared, err := prepareUpgradeStoppedWithTrust(context.Background(), paths, candidate.manifest.Version, backup, oldHash+","+newHash)
			require.NoError(t, err)
			defer prepared.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "cancel":
				cancel()
			case "closed":
				require.NoError(t, prepared.Close())
			case "release-changed":
				require.NoError(t, os.WriteFile(candidatePath, []byte("changed"), 0600))
			case "backup-changed":
				require.NoError(t, os.Remove(filepath.Join(backup, "complete.json")))
			}
			var calls []string
			err = prepared.execute(ctx, func(ctx context.Context, gotPaths Paths, operation, purpose, version string) error {
				require.Equal(t, paths, gotPaths)
				require.Equal(t, prepared.journal.OperationID, operation)
				require.Equal(t, candidate.manifest.Version, version)
				pointer, err := LoadRelease(paths.InstallDir)
				require.NoError(t, err)
				require.Equal(t, current.Version, pointer.Version)
				_, err = AcquireInstanceLock(paths.InstallDir)
				require.ErrorIs(t, err, ErrInstanceRunning)
				calls = append(calls, purpose)
				if purpose == "migrate_up" {
					if mode == "cancel-after-migrate" {
						cancel()
					}
					if mode == "release-changed-after-migrate" {
						require.NoError(t, os.WriteFile(candidatePath, []byte("changed"), 0600))
					}
				}
				if mode == "migrate-failure" && purpose == "migrate_up" || mode == "health-failure" && purpose == "candidate_health" {
					return errors.New("injected child failure")
				}
				return nil
			})
			journal, journalErr := ReadMaintenanceJournal(paths.InstallDir)
			require.NoError(t, journalErr)
			pointer, pointerErr := os.ReadFile(filepath.Join(paths.InstallDir, "current.json"))
			require.NoError(t, pointerErr)
			if mode == "complete" {
				require.NoError(t, err)
				require.Equal(t, []string{"migrate_up", "db_check", "candidate_health"}, calls)
				require.Equal(t, MaintenanceCommitting, journal.Phase)
				require.Contains(t, string(pointer), candidate.manifest.Version)
				require.Error(t, prepared.execute(ctx, func(context.Context, Paths, string, string, string) error {
					t.Fatal("repeated execution must not invoke a child")
					return nil
				}))
			} else {
				require.Error(t, err)
				require.Equal(t, journal.OldCurrentSHA256, currentPointerSHA256(pointer))
				if mode == "migrate-failure" || mode == "health-failure" || mode == "cancel-after-migrate" || mode == "release-changed-after-migrate" {
					require.Equal(t, MaintenanceRestoreRequired, journal.Phase)
					if mode == "health-failure" {
						require.Equal(t, []string{"migrate_up", "db_check", "candidate_health"}, calls)
					} else {
						require.Equal(t, []string{"migrate_up"}, calls)
					}
				} else {
					require.Empty(t, calls)
				}
			}
			require.ErrorIs(t, CheckMaintenanceGate(paths.InstallDir), ErrMaintenanceRequired)
		})
	}
}
