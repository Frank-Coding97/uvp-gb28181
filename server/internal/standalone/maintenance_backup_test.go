package standalone

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPreparingBackupAdmissionAndPromotion(t *testing.T) {
	for _, mode := range []string{"complete", "wrong-operation", "wrong-root", "wrong-phase", "run-marker", "untrusted"} {
		t.Run(mode, func(t *testing.T) {
			paths := newBackupTestPaths(t)
			candidate := newTestReleaseFixtureAt(t, paths.InstallDir, "t25.1", false)
			current, err := LoadRelease(paths.InstallDir)
			require.NoError(t, err)
			oldHash, err := releaseFileSHA256(current.BackendExe)
			require.NoError(t, err)
			candidateHash, err := releaseFileSHA256(filepath.Join(candidate.releaseDir, filepath.FromSlash(releaseBackendPath)))
			require.NoError(t, err)
			trust := oldHash + "," + candidateHash
			currentHash, err := releaseFileSHA256(filepath.Join(paths.InstallDir, "current.json"))
			require.NoError(t, err)
			destination := filepath.Join(filepath.Dir(paths.InstallDir), "preparing-backup")
			journal := maintenanceTestJournal(paths.InstallDir)
			journal.Phase, journal.BackupManifestSHA256 = MaintenancePreparing, ""
			journal.OldVersion, journal.CandidateVersion = current.Version, candidate.manifest.Version
			journal.BackupRoot, journal.OldCurrentSHA256 = destination, currentHash
			lock, err := AcquireInstanceLock(paths.InstallDir)
			require.NoError(t, err)
			defer lock.Close()
			if mode == "wrong-phase" {
				journal.Phase, journal.BackupManifestSHA256 = MaintenanceUpgrading, strings.Repeat("b", 64)
				require.NoError(t, createMaintenanceJournal(paths.InstallDir, journal))
			} else {
				require.NoError(t, createPreparingMaintenanceJournal(paths.InstallDir, journal))
			}
			operation := journal.OperationID
			switch mode {
			case "wrong-operation":
				operation = strings.Repeat("c", 64)
			case "wrong-root":
				destination += "-other"
			case "run-marker":
				require.NoError(t, os.WriteFile(filepath.Join(paths.DataDir, runMarkerName), []byte("active"), 0600))
			case "untrusted":
				trust = strings.Repeat("d", 64)
			}
			_, err = backupStoppedAdmitted(context.Background(), paths, destination, operation, trust)
			if mode == "complete" {
				require.NoError(t, err)
				require.NoError(t, promotePreparedMaintenance(context.Background(), paths.InstallDir, operation, destination))
				got, err := ReadMaintenanceJournal(paths.InstallDir)
				require.NoError(t, err)
				require.Equal(t, MaintenanceUpgrading, got.Phase)
			} else {
				require.Error(t, err)
				_, err := os.Stat(destination)
				require.ErrorIs(t, err, os.ErrNotExist)
				got, err := ReadMaintenanceJournal(paths.InstallDir)
				require.NoError(t, err)
				require.Equal(t, journal, got)
			}
			require.ErrorIs(t, CheckMaintenanceGate(paths.InstallDir), ErrMaintenanceRequired)
			_, err = AcquireInstanceLock(paths.InstallDir)
			require.ErrorIs(t, err, ErrInstanceRunning)
		})
	}
}
