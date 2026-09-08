package standalone

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPreparedMaintenanceRequiresVerifiedBackupBeforeMigration(t *testing.T) {
	for _, mode := range []string{"complete", "wrong-operation", "wrong-root", "missing-marker", "bad-manifest", "current-changed"} {
		t.Run(mode, func(t *testing.T) {
			paths := newBackupTestPaths(t)
			destination := filepath.Join(filepath.Dir(paths.InstallDir), "prepare-complete")
			backup, err := BackupStopped(context.Background(), paths, destination)
			require.NoError(t, err)
			current, err := os.ReadFile(filepath.Join(paths.InstallDir, "current.json"))
			require.NoError(t, err)
			lock, err := AcquireInstanceLock(paths.InstallDir)
			require.NoError(t, err)
			defer lock.Close()
			journal := maintenanceTestJournal(paths.InstallDir)
			journal.Phase, journal.BackupManifestSHA256 = MaintenancePreparing, ""
			journal.OldVersion, journal.BackupRoot, journal.OldCurrentSHA256 = backup.Version, destination, testSHA256(current)
			require.NoError(t, createPreparingMaintenanceJournal(paths.InstallDir, journal))
			operation, root := journal.OperationID, destination
			switch mode {
			case "wrong-operation":
				operation = strings.Repeat("c", 64)
			case "wrong-root":
				root = filepath.Join(filepath.Dir(destination), "other")
			case "missing-marker":
				require.NoError(t, os.Remove(filepath.Join(destination, "complete.json")))
			case "bad-manifest":
				require.NoError(t, os.WriteFile(filepath.Join(destination, "manifest.json"), []byte("broken"), 0600))
			case "current-changed":
				require.NoError(t, os.WriteFile(filepath.Join(paths.InstallDir, "current.json"), append(current, '\n'), 0600))
			}
			before := maintenanceCompatibilitySnapshot(t, paths.InstallDir)
			err = promotePreparedMaintenance(context.Background(), paths.InstallDir, operation, root)
			got, readErr := ReadMaintenanceJournal(paths.InstallDir)
			require.NoError(t, readErr)
			if mode == "complete" {
				require.NoError(t, err)
				require.Equal(t, MaintenanceUpgrading, got.Phase)
				raw, err := os.ReadFile(filepath.Join(destination, "manifest.json"))
				require.NoError(t, err)
				require.Equal(t, testSHA256(raw), got.BackupManifestSHA256)
				require.NoError(t, promotePreparedMaintenance(context.Background(), paths.InstallDir, operation, root))
			} else {
				require.Error(t, err)
				require.Equal(t, journal, got)
				require.Equal(t, before, maintenanceCompatibilitySnapshot(t, paths.InstallDir))
			}
			require.ErrorIs(t, CheckMaintenanceGate(paths.InstallDir), ErrMaintenanceRequired)
		})
	}
}
