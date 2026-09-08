package standalone

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMaintenanceGateRejectsInterruptedAndCorruptJournal(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, CheckMaintenanceGate(root))
	dir := filepath.Join(root, maintenanceDirName)
	require.NoError(t, os.Mkdir(dir, 0700))
	require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "journal.json"), []byte("broken"), 0600))
	require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)
	_, err := ReadMaintenanceJournal(root)
	require.Error(t, err)
}

func TestMaintenanceJournalPersistsAndNeverClearsGateOnRead(t *testing.T) {
	root := t.TempDir()
	journal := maintenanceTestJournal(root)
	require.NoError(t, createMaintenanceJournal(root, journal))
	got, err := ReadMaintenanceJournal(root)
	require.NoError(t, err)
	require.Equal(t, journal, got)
	require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)
	require.Error(t, createMaintenanceJournal(root, journal))
	require.NoError(t, advanceMaintenance(root, journal.OperationID, MaintenanceUpgrading, MaintenanceRestoreRequired))
	require.NoError(t, advanceMaintenance(root, journal.OperationID, MaintenanceRestoreRequired, MaintenanceRestoring))
	require.NoError(t, advanceMaintenance(root, journal.OperationID, MaintenanceRestoring, MaintenanceAwaitingConfirmation))
	require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)
}

func TestMaintenanceJournalRejectsWrongOwnerAndInvalidTransitions(t *testing.T) {
	root := t.TempDir()
	journal := maintenanceTestJournal(root)
	require.NoError(t, createMaintenanceJournal(root, journal))
	require.Error(t, advanceMaintenance(root, strings.Repeat("b", 64), MaintenanceUpgrading, MaintenanceRestoreRequired))
	require.Error(t, advanceMaintenance(root, journal.OperationID, MaintenanceRestoring, MaintenanceAwaitingConfirmation))
	require.Error(t, advanceMaintenance(root, journal.OperationID, MaintenanceUpgrading, MaintenanceAwaitingConfirmation))
	got, err := ReadMaintenanceJournal(root)
	require.NoError(t, err)
	require.Equal(t, journal, got)
}

func TestMaintenanceGatePreventsNewBackupOfIncompleteState(t *testing.T) {
	paths := newBackupTestPaths(t)
	require.NoError(t, os.Mkdir(filepath.Join(paths.InstallDir, maintenanceDirName), 0700))
	destination := filepath.Join(filepath.Dir(paths.InstallDir), "incomplete-state-backup")
	_, err := BackupStopped(context.Background(), paths, destination)
	require.ErrorIs(t, err, ErrMaintenanceRequired)
	_, err = os.Stat(destination)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func maintenanceTestJournal(root string) MaintenanceJournal {
	return MaintenanceJournal{Schema: 1, OperationID: strings.Repeat("a", 64), OldVersion: "v1", CandidateVersion: "v2", OldCurrentSHA256: strings.Repeat("b", 64), BackupRoot: filepath.Join(root, "external-backup"), BackupManifestSHA256: strings.Repeat("c", 64), Phase: MaintenanceUpgrading, CreatedAt: time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)}
}
