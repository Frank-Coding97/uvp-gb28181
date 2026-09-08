package standalone

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpgradePreparationOwnsWholeBackupTransaction(t *testing.T) {
	for _, mode := range []string{"complete", "untrusted", "busy", "invalid-destination", "run-marker"} {
		t.Run(mode, func(t *testing.T) {
			paths := newBackupTestPaths(t)
			candidate := newTestReleaseFixtureAt(t, paths.InstallDir, "t25.1", false)
			current, err := LoadRelease(paths.InstallDir)
			require.NoError(t, err)
			oldSHA, err := releaseFileSHA256(current.BackendExe)
			require.NoError(t, err)
			newSHA, err := releaseFileSHA256(filepath.Join(candidate.releaseDir, filepath.FromSlash(releaseBackendPath)))
			require.NoError(t, err)
			trust := oldSHA + "," + newSHA
			destination := filepath.Join(filepath.Dir(paths.InstallDir), "transaction-backup")
			switch mode {
			case "untrusted":
				trust = strings.Repeat("a", 64)
			case "busy":
				lock, err := AcquireInstanceLock(paths.InstallDir)
				require.NoError(t, err)
				defer lock.Close()
			case "invalid-destination":
				destination = filepath.Join(paths.ConfigDir, "backup")
			case "run-marker":
				require.NoError(t, os.WriteFile(filepath.Join(paths.DataDir, runMarkerName), []byte("active"), 0600))
			}
			beforeData := maintenanceCompatibilitySnapshot(t, paths.DataDir)
			beforeConfig := maintenanceCompatibilitySnapshot(t, paths.ConfigDir)
			beforeCurrent, err := releaseFileSHA256(filepath.Join(paths.InstallDir, "current.json"))
			require.NoError(t, err)
			prepared, err := prepareUpgradeStoppedWithTrust(context.Background(), paths, candidate.manifest.Version, destination, trust)
			if mode == "complete" {
				require.NoError(t, err)
				defer prepared.Close()
				require.Equal(t, MaintenanceUpgrading, prepared.journal.Phase)
				_, err := VerifyBackup(context.Background(), destination)
				require.NoError(t, err)
				_, err = AcquireInstanceLock(paths.InstallDir)
				require.ErrorIs(t, err, ErrInstanceRunning)
				require.NoError(t, prepared.Close())
				lock, err := AcquireInstanceLock(paths.InstallDir)
				require.NoError(t, err)
				require.NoError(t, lock.Close())
			} else {
				require.Error(t, err)
				require.Nil(t, prepared)
				_, err := os.Stat(destination)
				require.ErrorIs(t, err, os.ErrNotExist)
			}
			if mode == "complete" || mode == "run-marker" {
				require.ErrorIs(t, CheckMaintenanceGate(paths.InstallDir), ErrMaintenanceRequired)
			} else {
				require.NoError(t, CheckMaintenanceGate(paths.InstallDir))
			}
			require.Equal(t, beforeData, maintenanceCompatibilitySnapshot(t, paths.DataDir))
			require.Equal(t, beforeConfig, maintenanceCompatibilitySnapshot(t, paths.ConfigDir))
			afterCurrent, err := releaseFileSHA256(filepath.Join(paths.InstallDir, "current.json"))
			require.NoError(t, err)
			require.Equal(t, beforeCurrent, afterCurrent)
		})
	}
}
