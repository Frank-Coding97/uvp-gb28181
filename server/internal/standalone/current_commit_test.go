package standalone

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCommitMaintenanceCurrentPublishesCandidateAndKeepsGate(t *testing.T) {
	current, candidate, journal := newCurrentCommitTestFixture(t)

	require.NoError(t, commitMaintenanceCurrent(current.installDir, journal.OperationID))
	require.Equal(t, candidate.manifest.Version, mustLoadCurrentVersion(t, current.installDir))
	raw, err := os.ReadFile(filepath.Join(current.installDir, "current.json"))
	require.NoError(t, err)
	require.Equal(t, []byte(`{"version":"2.0.0-win10"}`), raw)

	gotJournal, err := ReadMaintenanceJournal(current.installDir)
	require.NoError(t, err)
	require.Equal(t, journal, gotJournal)
	require.ErrorIs(t, CheckMaintenanceGate(current.installDir), ErrMaintenanceRequired)
}

func TestCommitMaintenanceCurrentRetryIsIdempotent(t *testing.T) {
	current, _, journal := newCurrentCommitTestFixture(t)

	require.NoError(t, commitMaintenanceCurrent(current.installDir, journal.OperationID))
	first, err := os.ReadFile(filepath.Join(current.installDir, "current.json"))
	require.NoError(t, err)
	require.NoError(t, commitMaintenanceCurrent(current.installDir, journal.OperationID))
	second, err := os.ReadFile(filepath.Join(current.installDir, "current.json"))
	require.NoError(t, err)
	require.Equal(t, first, second)
}

func TestCommitMaintenanceCurrentRejectsOwnerPhaseDriftAndCandidateFailures(t *testing.T) {
	tests := []struct {
		name        string
		operationID func(string) string
		prepare     func(*testing.T, testReleaseFixture, testReleaseFixture, MaintenanceJournal)
	}{
		{
			name:        "wrong owner",
			operationID: func(string) string { return strings.Repeat("b", 64) },
		},
		{
			name: "wrong phase",
			prepare: func(t *testing.T, current, _ testReleaseFixture, journal MaintenanceJournal) {
				require.NoError(t, advanceMaintenance(current.installDir, journal.OperationID, MaintenanceCommitting, MaintenanceRestoreRequired))
			},
		},
		{
			name: "pointer drift",
			prepare: func(t *testing.T, current, _ testReleaseFixture, _ MaintenanceJournal) {
				require.NoError(t, os.WriteFile(filepath.Join(current.installDir, "current.json"), []byte(`{"version":"drifted"}`), 0o600))
			},
		},
		{
			name: "candidate hash failure",
			prepare: func(t *testing.T, _, candidate testReleaseFixture, _ MaintenanceJournal) {
				require.NoError(t, os.WriteFile(filepath.Join(candidate.releaseDir, "backend", "uvp-server.exe"), []byte("tampered"), 0o600))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current, candidate, journal := newCurrentCommitTestFixture(t)
			if tt.prepare != nil {
				tt.prepare(t, current, candidate, journal)
			}
			before, err := os.ReadFile(filepath.Join(current.installDir, "current.json"))
			require.NoError(t, err)
			operationID := journal.OperationID
			if tt.operationID != nil {
				operationID = tt.operationID(operationID)
			}

			err = commitMaintenanceCurrent(current.installDir, operationID)

			require.Error(t, err)
			after, readErr := os.ReadFile(filepath.Join(current.installDir, "current.json"))
			require.NoError(t, readErr)
			require.Equal(t, before, after)
		})
	}
}

func newCurrentCommitTestFixture(t *testing.T) (testReleaseFixture, testReleaseFixture, MaintenanceJournal) {
	t.Helper()
	current := newTestReleaseFixture(t, "1.2.3-win10")
	candidate := newTestReleaseFixtureAt(t, current.installDir, "2.0.0-win10", false)
	currentRaw, err := os.ReadFile(filepath.Join(current.installDir, "current.json"))
	require.NoError(t, err)
	sum := sha256.Sum256(currentRaw)
	journal := MaintenanceJournal{
		Schema:               1,
		OperationID:          strings.Repeat("a", 64),
		OldVersion:           current.manifest.Version,
		CandidateVersion:     candidate.manifest.Version,
		OldCurrentSHA256:     hex.EncodeToString(sum[:]),
		BackupRoot:           filepath.Join(current.installDir, "backup"),
		BackupManifestSHA256: strings.Repeat("c", 64),
		Phase:                MaintenanceUpgrading,
		CreatedAt:            time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC),
	}
	lock, err := AcquireInstanceLock(current.installDir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.Close()) })
	require.NoError(t, createMaintenanceJournal(current.installDir, journal))
	require.NoError(t, advanceMaintenance(current.installDir, journal.OperationID, MaintenanceUpgrading, MaintenanceCommitting))
	journal.Phase = MaintenanceCommitting
	return current, candidate, journal
}

func mustLoadCurrentVersion(t *testing.T, installDir string) string {
	t.Helper()
	release, err := LoadRelease(installDir)
	require.NoError(t, err)
	return release.Version
}
