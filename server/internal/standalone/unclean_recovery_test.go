package standalone

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUncleanRecoveryPreservesVersionAndRequiresAppropriateConfirmation(t *testing.T) {
	for _, initialized := range []bool{false, true} {
		t.Run(map[bool]string{false: "pristine", true: "administrator"}[initialized], func(t *testing.T) {
			paths := newBackupTestPaths(t)
			db := newRecoveryPristineSQLite(t)
			if initialized {
				addRecoveryAuthorizationUser(t, db, 42, "unclean-admin", "Fixture!Password42")
				execRecoveryPristineSQL(t, db, `UPDATE standalone_installation SET phase='pending_sip', admin_user_id=42 WHERE id=1`)
			}
			raw, err := os.ReadFile(db)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(paths.DatabasePath, raw, 0600))
			current, err := LoadRelease(paths.InstallDir)
			require.NoError(t, err)
			trust, err := releaseFileSHA256(current.BackendExe)
			require.NoError(t, err)
			before, err := LoadConfig(paths)
			require.NoError(t, err)
			currentRaw, err := os.ReadFile(filepath.Join(paths.InstallDir, "current.json"))
			require.NoError(t, err)
			lock, err := AcquireInstanceLock(paths.InstallDir)
			require.NoError(t, err)
			_, err = BeginRun(paths)
			require.NoError(t, err)
			require.NoError(t, lock.Close())
			marker, err := os.ReadFile(filepath.Join(paths.DataDir, runMarkerName))
			require.NoError(t, err)
			destination := filepath.Join(filepath.Dir(paths.InstallDir), "unclean-snapshot")
			run := func(context.Context, Paths, string, string, string) error { return nil }
			redis := func(_ context.Context, _, _, target, _ string, _ int) error {
				return os.WriteFile(filepath.Join(target, "fresh"), []byte("sanitized-fixture"), 0600)
			}
			result, err := recoverUncleanStoppedWithTrust(t.Context(), paths, destination, trust, run, redis)
			require.NoError(t, err)
			require.Equal(t, initialized, result.AwaitingLocalConfirmation)
			after, err := LoadConfig(paths)
			require.NoError(t, err)
			require.NotEqual(t, sha256.Sum256([]byte(before.JWTSecret())), sha256.Sum256([]byte(after.JWTSecret())))
			require.NotEqual(t, before.InstanceGeneration(), after.InstanceGeneration())
			require.Equal(t, sha256.Sum256([]byte(before.RedisPassword())), sha256.Sum256([]byte(after.RedisPassword())))
			actualCurrent, err := os.ReadFile(filepath.Join(paths.InstallDir, "current.json"))
			require.NoError(t, err)
			require.Equal(t, currentRaw, actualCurrent)
			if initialized {
				require.ErrorIs(t, CheckMaintenanceGate(paths.InstallDir), ErrMaintenanceRequired)
				_, wrongEntryErr := RestoreStopped(t.Context(), paths, run, redis)
				require.ErrorContains(t, wrongEntryErr, "must use the recover command")
				_, changedErr := recoverUncleanStoppedWithTrust(t.Context(), paths, destination+"-different", trust, run, redis)
				require.ErrorContains(t, changedErr, "destination changed")
				repeated, err := recoverUncleanStoppedWithTrust(t.Context(), paths, destination, trust, run, redis)
				require.NoError(t, err)
				require.Equal(t, result, repeated)
				require.NoError(t, confirmRecoveryWithTrust(t.Context(), paths.InstallDir, result.OperationID, trust, func(info RecoveryConfirmationInfo) (RecoveryConfirmationInput, error) {
					return RecoveryConfirmationInput{"unclean-admin", "Fixture!Password42", "CONFIRM " + info.OperationID}, nil
				}))
			}
			require.NoError(t, CheckMaintenanceGate(paths.InstallDir))
			archive := filepath.Join(paths.InstallDir, ".uvp-recovered-"+result.OperationID)
			failedMarker, err := os.ReadFile(filepath.Join(archive, "failed", result.OperationID, "data", runMarkerName))
			require.NoError(t, err)
			require.Equal(t, marker, failedMarker)
			require.NoFileExists(t, filepath.Join(paths.DataDir, runMarkerName))
			manifest, err := VerifyBackup(t.Context(), destination)
			require.NoError(t, err)
			require.Equal(t, 2, manifest.FormatVersion)
			require.Equal(t, result.OperationID, manifest.OperationID)
			if !initialized {
				require.FileExists(t, filepath.Join(archive, "pristine-confirmation.json"))
				require.NoFileExists(t, filepath.Join(archive, "confirmation.json"))
			}
		})
	}
}
