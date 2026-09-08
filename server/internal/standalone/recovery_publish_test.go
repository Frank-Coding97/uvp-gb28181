package standalone

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecoveryPublicationResumesEachUnrecordedRename(t *testing.T) {
	for _, stop := range []string{"none", "config_isolated", "config_published", "data_isolated", "data_published"} {
		t.Run(stop, func(t *testing.T) {
			paths := newBackupTestPaths(t)
			candidate := newTestReleaseFixtureAt(t, paths.InstallDir, "t25-recovery", false)
			current, err := LoadRelease(paths.InstallDir)
			require.NoError(t, err)
			oldSHA, err := releaseFileSHA256(current.BackendExe)
			require.NoError(t, err)
			newSHA, err := releaseFileSHA256(filepath.Join(candidate.releaseDir, filepath.FromSlash(releaseBackendPath)))
			require.NoError(t, err)
			trust := oldSHA + "," + newSHA
			owner, err := prepareUpgradeStoppedWithTrust(context.Background(), paths, candidate.manifest.Version, filepath.Join(filepath.Dir(paths.InstallDir), "recovery-backup"), trust)
			require.NoError(t, err)
			defer owner.Close()
			outer := owner.journal
			require.NoError(t, advanceMaintenance(paths.InstallDir, outer.OperationID, MaintenanceUpgrading, MaintenanceRestoreRequired))
			j := recoveryJournal{Schema: 1, OperationID: outer.OperationID, BackupManifestSHA256: outer.BackupManifestSHA256, OldVersion: outer.OldVersion, OldCurrentSHA256: outer.OldCurrentSHA256, ReleaseSetSHA256: outer.ReleaseSetSHA256, Phase: "staging"}
			require.NoError(t, persistRecoveryJournal(paths.InstallDir, nil, j))
			require.NoError(t, advanceMaintenance(paths.InstallDir, outer.OperationID, MaintenanceRestoreRequired, MaintenanceRestoring))
			work := filepath.Join(paths.InstallDir, maintenanceDirName, "work", j.OperationID)
			failed := filepath.Join(paths.InstallDir, maintenanceDirName, "failed", j.OperationID)
			require.NoError(t, os.MkdirAll(failed, 0700))
			// This is a directory publication fixture, not a sanitized restore
			// or backend/schema acceptance test.
			for _, name := range []string{"config", "data"} {
				require.NoError(t, os.MkdirAll(filepath.Join(work, name), 0700))
				require.NoError(t, os.WriteFile(filepath.Join(work, name, "restored"), []byte(name), 0600))
			}
			next := j
			next.Phase = "staged"
			next.StagedConfigSHA256, err = recoveryTreeIdentity(context.Background(), filepath.Join(work, "config"))
			require.NoError(t, err)
			next.StagedDataSHA256, err = recoveryTreeIdentity(context.Background(), filepath.Join(work, "data"))
			require.NoError(t, err)
			next.FailedConfigSHA256, err = recoveryTreeIdentity(context.Background(), paths.ConfigDir)
			require.NoError(t, err)
			next.FailedDataSHA256, err = recoveryTreeIdentity(context.Background(), paths.DataDir)
			require.NoError(t, err)
			require.NoError(t, persistRecoveryJournal(paths.InstallDir, &j, next))
			if stop == "none" {
				require.Error(t, publishRecoveryDirectories(context.Background(), paths.InstallDir, "wrong-operation", trust, nil))
				require.Error(t, publishRecoveryDirectories(context.Background(), paths.InstallDir, j.OperationID, "", nil))
			}
			err = publishRecoveryDirectories(context.Background(), paths.InstallDir, j.OperationID, trust, func(phase string) error {
				if phase == stop {
					return errors.New("interrupted after rename")
				}
				return nil
			})
			if stop == "none" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.NoError(t, publishRecoveryDirectories(context.Background(), paths.InstallDir, j.OperationID, trust, nil))
			require.NoError(t, publishRecoveryDirectories(context.Background(), paths.InstallDir, j.OperationID, trust, nil))
			final, err := readRecoveryJournal(paths.InstallDir)
			require.NoError(t, err)
			require.Equal(t, "data_published", final.Phase)
			for name, digest := range map[string]string{"config": next.FailedConfigSHA256, "data": next.FailedDataSHA256} {
				actual, err := recoveryTreeIdentity(context.Background(), filepath.Join(failed, name))
				require.NoError(t, err)
				require.Equal(t, digest, actual)
			}
			require.ErrorIs(t, CheckMaintenanceGate(paths.InstallDir), ErrMaintenanceRequired)
			pointer, err := releaseFileSHA256(filepath.Join(paths.InstallDir, "current.json"))
			require.NoError(t, err)
			require.Equal(t, outer.OldCurrentSHA256, pointer)
			if stop == "none" {
				require.NoError(t, os.WriteFile(filepath.Join(failed, "config", "unexpected"), []byte("changed scene"), 0600))
				require.Error(t, publishRecoveryDirectories(context.Background(), paths.InstallDir, j.OperationID, trust, nil))
				require.FileExists(t, filepath.Join(failed, "config", "unexpected"))
				require.ErrorIs(t, CheckMaintenanceGate(paths.InstallDir), ErrMaintenanceRequired)
			}
		})
	}
}
