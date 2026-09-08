package standalone

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecoveryCurrentRestoresOnlyAfterDirectoryPublication(t *testing.T) {
	for _, mode := range []string{"complete", "already-old", "wrong-current", "archive-conflict", "after-archive", "after-pointer"} {
		t.Run(mode, func(t *testing.T) {
			owner := completedUpgradeFixture(t)
			root, outer := owner.paths.InstallDir, owner.journal
			require.NoError(t, advanceMaintenance(root, outer.OperationID, MaintenanceCommitting, MaintenanceRestoreRequired))
			j := recoveryJournal{Schema: 1, OperationID: outer.OperationID, BackupManifestSHA256: outer.BackupManifestSHA256, OldVersion: outer.OldVersion, OldCurrentSHA256: outer.OldCurrentSHA256, ReleaseSetSHA256: outer.ReleaseSetSHA256, Phase: "staging"}
			require.NoError(t, persistRecoveryJournal(root, nil, j))
			require.NoError(t, advanceMaintenance(root, outer.OperationID, MaintenanceRestoreRequired, MaintenanceRestoring))
			failed := filepath.Join(root, maintenanceDirName, "failed", j.OperationID)
			require.NoError(t, os.MkdirAll(failed, 0700))
			require.NoError(t, protectConfigDir(failed, true))
			for _, name := range []string{"config", "data"} {
				require.NoError(t, os.MkdirAll(filepath.Join(failed, name), 0700))
				require.NoError(t, os.WriteFile(filepath.Join(failed, name, "scene"), []byte("failed"), 0600))
			}
			// Only pointer publication is under test; this fixture does not
			// claim to revoke sessions or construct sanitized Redis state.
			next := j
			next.Phase = "staged"
			var err error
			next.StagedConfigSHA256, err = recoveryTreeIdentity(context.Background(), owner.paths.ConfigDir)
			require.NoError(t, err)
			next.StagedDataSHA256, err = recoveryTreeIdentity(context.Background(), owner.paths.DataDir)
			require.NoError(t, err)
			next.FailedConfigSHA256, err = recoveryTreeIdentity(context.Background(), filepath.Join(failed, "config"))
			require.NoError(t, err)
			next.FailedDataSHA256, err = recoveryTreeIdentity(context.Background(), filepath.Join(failed, "data"))
			require.NoError(t, err)
			require.NoError(t, persistRecoveryJournal(root, &j, next))
			j = next
			for _, phase := range []string{"config_isolated", "config_published", "data_isolated", "data_published"} {
				next = j
				next.Phase = phase
				require.NoError(t, persistRecoveryJournal(root, &j, next))
				j = next
			}
			current := filepath.Join(root, "current.json")
			old, err := os.ReadFile(filepath.Join(outer.BackupRoot, "install", "current.json"))
			require.NoError(t, err)
			switch mode {
			case "already-old":
				require.NoError(t, writeCurrentPointerAtomically(current, old))
			case "wrong-current":
				require.NoError(t, os.WriteFile(current, []byte(`{"version":"unknown"}`), 0600))
			case "archive-conflict":
				require.NoError(t, writeSecureConfigFile(filepath.Join(failed, "current.json"), []byte("wrong"), false, nil))
			}
			err = restoreRecoveryCurrent(context.Background(), root, j.OperationID, owner.trust, func(stage string) error {
				if stage == mode {
					return errors.New("interrupted")
				}
				return nil
			})
			if mode == "wrong-current" || mode == "archive-conflict" {
				require.Error(t, err)
			} else {
				if mode == "after-archive" || mode == "after-pointer" {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				require.NoError(t, restoreRecoveryCurrent(context.Background(), root, j.OperationID, owner.trust, nil))
				actual, err := os.ReadFile(current)
				require.NoError(t, err)
				require.Equal(t, old, actual)
				final, err := readRecoveryJournal(root)
				require.NoError(t, err)
				require.Equal(t, "pointer_restored", final.Phase)
			}
			require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)
		})
	}
}
