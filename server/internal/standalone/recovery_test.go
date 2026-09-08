package standalone

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecoveryCoordinatorStopsAtLocalConfirmation(t *testing.T) {
	owner := completedUpgradeFixture(t)
	root, op := owner.paths.InstallDir, owner.journal.OperationID
	require.NoError(t, advanceMaintenance(root, op, MaintenanceCommitting, MaintenanceRestoreRequired))
	require.NoError(t, owner.Close())
	var calls []string
	result, err := restoreStoppedWithRunners(context.Background(), owner.paths, op, owner.trust,
		func(ctx context.Context, p Paths, operation, purpose, version string) error {
			_, err := AcquireInstanceLock(root)
			require.ErrorIs(t, err, ErrInstanceRunning)
			require.NotEqual(t, owner.paths.DataDir, p.DataDir)
			require.Equal(t, owner.journal.OldVersion, version)
			calls = append(calls, purpose)
			return nil
		}, func(ctx context.Context, exe, source, target, control string, indexDB int) error {
			return os.WriteFile(filepath.Join(target, "verified-fixture"), []byte("fresh"), 0600)
		})
	require.NoError(t, err)
	require.Equal(t, MaintenanceAwaitingConfirmation, result.Phase)
	require.Equal(t, []string{"revoke_sessions", "db_check"}, calls)
	require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)
	selected, err := LoadRelease(root)
	require.NoError(t, err)
	require.Equal(t, owner.journal.OldVersion, selected.Version)
	j, err := readRecoveryJournal(root)
	require.NoError(t, err)
	require.Equal(t, "pointer_restored", j.Phase)
	require.NoError(t, checkRecoveryDirectoryState(context.Background(), root, j, 5))
	lock, err := AcquireInstanceLock(root)
	require.NoError(t, err)
	require.NoError(t, lock.Close())
	_, err = restoreStoppedWithRunners(context.Background(), owner.paths, op, owner.trust, nil, nil)
	require.Error(t, err)
	require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)
}
