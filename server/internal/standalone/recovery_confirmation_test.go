package standalone

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecoveryConfirmationRequiresLocalAdministratorAndAcknowledgement(t *testing.T) {
	for _, mode := range []string{"complete", "wrong-password", "wrong-acknowledgement", "cancelled", "state-changed"} {
		t.Run(mode, func(t *testing.T) {
			paths := newBackupTestPaths(t)
			db := newRecoveryAuthorizationSQLite(t)
			addRecoveryAuthorizationUser(t, db, 42, "recovery-admin", "Fixture!Password42")
			raw, err := os.ReadFile(db)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(paths.DatabasePath, raw, 0600))
			candidate := newTestReleaseFixtureAt(t, paths.InstallDir, "t26-confirm", false)
			current, err := LoadRelease(paths.InstallDir)
			require.NoError(t, err)
			oldHash, err := releaseFileSHA256(current.BackendExe)
			require.NoError(t, err)
			newHash, err := releaseFileSHA256(filepath.Join(candidate.releaseDir, filepath.FromSlash(releaseBackendPath)))
			require.NoError(t, err)
			trust := oldHash + "," + newHash
			owner, err := prepareUpgradeStoppedWithTrust(context.Background(), paths, candidate.manifest.Version, filepath.Join(filepath.Dir(paths.InstallDir), "confirm-backup"), trust)
			require.NoError(t, err)
			op := owner.journal.OperationID
			// This later disable must be explicitly visible to the administrator
			// because restoration returns the older enabled account.
			execRecoveryAuthorizationSQL(t, paths.DatabasePath, "UPDATE sys_users SET status=0 WHERE id=42")
			require.NoError(t, advanceMaintenance(paths.InstallDir, op, MaintenanceUpgrading, MaintenanceRestoreRequired))
			require.NoError(t, owner.Close())
			_, err = restoreStoppedWithRunners(context.Background(), paths, op, trust,
				func(context.Context, Paths, string, string, string) error { return nil },
				func(_ context.Context, _, _, target, _ string, _ int) error {
					return os.WriteFile(filepath.Join(target, "fixture"), []byte("fresh"), 0600)
				})
			require.NoError(t, err)
			called := false
			err = confirmRecoveryWithTrust(context.Background(), paths.InstallDir, op, trust, func(info RecoveryConfirmationInfo) (RecoveryConfirmationInput, error) {
				called = true
				require.Equal(t, op, info.OperationID)
				require.False(t, info.BackupTime.IsZero())
				require.Contains(t, strings.Join(info.Impacts, "\n"), "recovery-admin")
				require.ErrorIs(t, CheckMaintenanceGate(paths.InstallDir), ErrMaintenanceRequired)
				_, err := AcquireInstanceLock(paths.InstallDir)
				require.ErrorIs(t, err, ErrInstanceRunning)
				input := RecoveryConfirmationInput{"recovery-admin", "Fixture!Password42", "CONFIRM " + op}
				switch mode {
				case "wrong-password":
					input.Password = "wrong"
				case "wrong-acknowledgement":
					input.Acknowledgement = "yes"
				case "cancelled":
					return input, errors.New("cancelled")
				case "state-changed":
					require.NoError(t, os.WriteFile(filepath.Join(paths.DataDir, "unexpected"), []byte("change"), 0600))
				}
				return input, nil
			})
			require.True(t, called)
			if mode == "complete" {
				require.NoError(t, err)
				require.NoError(t, CheckMaintenanceGate(paths.InstallDir))
			} else {
				require.Error(t, err)
				require.ErrorIs(t, CheckMaintenanceGate(paths.InstallDir), ErrMaintenanceRequired)
			}
		})
	}
}
