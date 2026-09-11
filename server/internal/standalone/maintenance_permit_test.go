package standalone

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type maintenancePermitTestFixture struct {
	current   testReleaseFixture
	candidate testReleaseFixture
	journal   MaintenanceJournal
	lock      *InstanceLock
}

func TestMaintenancePermitIssueConsumeOnce(t *testing.T) {
	fixture := newMaintenancePermitTestFixture(t)
	frame, err := IssueMaintenancePermit(fixture.current.installDir, fixture.journal.OperationID, "candidate_health", fixture.candidate.manifest.Version)
	require.NoError(t, err)
	require.Equal(t, len(maintenancePermitFramePrefix)+maintenancePermitTokenTextSize+1, len(frame))

	permitPath := filepath.Join(fixture.current.installDir, maintenanceDirName, maintenancePermitFileName)
	raw, err := readSecureConfigFile(permitPath)
	require.NoError(t, err)
	require.NotContains(t, string(raw), string(frame))
	token := string(frame[len(maintenancePermitFramePrefix) : len(frame)-1])
	require.NotContains(t, string(raw), token)

	claims, err := consumeMaintenancePermitAt(fixture.current.installDir, "candidate_health", &maintenancePermitNoEOFReader{frame: frame}, maintenancePermitBackendPath(fixture.candidate), time.Now)
	require.NoError(t, err)
	require.Equal(t, MaintenancePermitClaims{
		OperationID: fixture.journal.OperationID,
		Version:     fixture.candidate.manifest.Version,
		Purpose:     "candidate_health",
	}, claims)
	_, err = os.Stat(permitPath)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.ErrorIs(t, CheckMaintenanceGate(fixture.current.installDir), ErrMaintenanceRequired)
	gotJournal, err := ReadMaintenanceJournal(fixture.current.installDir)
	require.NoError(t, err)
	require.Equal(t, fixture.journal, gotJournal)

	_, err = consumeMaintenancePermitAt(fixture.current.installDir, "candidate_health", bytes.NewReader(frame), maintenancePermitBackendPath(fixture.candidate), time.Now)
	require.Error(t, err)
}

func TestMaintenancePermitIssueDoesNotOverwriteExistingPermit(t *testing.T) {
	fixture := newMaintenancePermitTestFixture(t)
	first, err := IssueMaintenancePermit(fixture.current.installDir, fixture.journal.OperationID, "candidate_health", fixture.candidate.manifest.Version)
	require.NoError(t, err)

	second, err := IssueMaintenancePermit(fixture.current.installDir, fixture.journal.OperationID, "candidate_health", fixture.candidate.manifest.Version)
	require.Error(t, err)
	require.Nil(t, second)

	permitPath := filepath.Join(fixture.current.installDir, maintenanceDirName, maintenancePermitFileName)
	raw, err := readSecureConfigFile(permitPath)
	require.NoError(t, err)
	require.NotContains(t, string(raw), string(first))
}

func TestMaintenancePermitIssueRejectsInvalidSelectionAndJournal(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, *maintenancePermitTestFixture)
		purpose string
		version string
		op      string
	}{
		{
			name: "wrong operation",
			op:   strings.Repeat("b", 64),
		},
		{
			name:    "unknown purpose",
			purpose: "migrate_down",
		},
		{
			name:    "wrong version",
			version: "1.2.3-win10",
		},
		{
			name: "wrong phase",
			prepare: func(t *testing.T, fixture *maintenancePermitTestFixture) {
				require.NoError(t, advanceMaintenance(fixture.current.installDir, fixture.journal.OperationID, MaintenanceCommitting, MaintenanceRestoreRequired))
			},
		},
		{
			name: "bad journal",
			prepare: func(t *testing.T, fixture *maintenancePermitTestFixture) {
				path := filepath.Join(fixture.current.installDir, maintenanceDirName, "journal.json")
				require.NoError(t, writeSecureConfigFile(path, []byte("broken"), true, nil))
			},
		},
		{
			name: "tampered candidate",
			prepare: func(t *testing.T, fixture *maintenancePermitTestFixture) {
				path := filepath.Join(fixture.candidate.releaseDir, "backend", "uvp-server.exe")
				require.NoError(t, os.WriteFile(path, []byte("tampered"), 0o600))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newMaintenancePermitTestFixture(t)
			purpose := "candidate_health"
			version := fixture.candidate.manifest.Version
			op := fixture.journal.OperationID
			if tt.purpose != "" {
				purpose = tt.purpose
			}
			if tt.version != "" {
				version = tt.version
			}
			if tt.op != "" {
				op = tt.op
			}
			if tt.prepare != nil {
				tt.prepare(t, &fixture)
			}
			_, err := IssueMaintenancePermit(fixture.current.installDir, op, purpose, version)
			require.Error(t, err)
			_, err = os.Stat(filepath.Join(fixture.current.installDir, maintenanceDirName, maintenancePermitFileName))
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

func TestMaintenancePermitConsumeRejectsTamperingAndKeepsPermit(t *testing.T) {
	tests := []struct {
		name    string
		purpose string
		exe     func(*maintenancePermitTestFixture) string
		mutate  func(*maintenancePermitEnvelope)
		frame   func([]byte) []byte
		now     func() time.Time
	}{
		{
			name:    "wrong purpose",
			purpose: "db_check",
		},
		{
			name: "wrong operation",
			mutate: func(envelope *maintenancePermitEnvelope) {
				envelope.OperationID = strings.Repeat("b", 64)
			},
		},
		{
			name: "wrong digest",
			mutate: func(envelope *maintenancePermitEnvelope) {
				envelope.Digest = strings.Repeat("0", 64)
			},
		},
		{
			name: "wrong version",
			mutate: func(envelope *maintenancePermitEnvelope) {
				envelope.Version = "1.2.3-win10"
			},
		},
		{
			name: "expired",
			mutate: func(envelope *maintenancePermitEnvelope) {
				envelope.ExpiresAt = time.Now().Add(-time.Second)
			},
		},
		{
			name: "wrong executable",
			exe: func(fixture *maintenancePermitTestFixture) string {
				return filepath.Join(fixture.current.installDir, "wrong-backend.exe")
			},
		},
		{
			name: "short frame",
			frame: func(frame []byte) []byte {
				return frame[:len(frame)-1]
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newMaintenancePermitTestFixture(t)
			frame, err := IssueMaintenancePermit(fixture.current.installDir, fixture.journal.OperationID, "candidate_health", fixture.candidate.manifest.Version)
			require.NoError(t, err)
			permitPath := filepath.Join(fixture.current.installDir, maintenanceDirName, maintenancePermitFileName)
			if tt.mutate != nil {
				mutateMaintenancePermitEnvelope(t, permitPath, tt.mutate)
			}
			if tt.frame != nil {
				frame = tt.frame(frame)
			}
			purpose := tt.purpose
			if purpose == "" {
				purpose = "candidate_health"
			}
			exe := maintenancePermitBackendPath(fixture.candidate)
			if tt.exe != nil {
				exe = tt.exe(&fixture)
			}
			now := time.Now
			if tt.now != nil {
				now = tt.now
			}

			_, err = consumeMaintenancePermitAt(fixture.current.installDir, purpose, bytes.NewReader(frame), exe, now)
			require.Error(t, err)
			_, err = os.Stat(permitPath)
			require.NoError(t, err)
			require.ErrorIs(t, CheckMaintenanceGate(fixture.current.installDir), ErrMaintenanceRequired)
		})
	}
}

func TestMaintenancePermitRequiresParentInstanceLock(t *testing.T) {
	fixture := newMaintenancePermitTestFixture(t)
	frame, err := IssueMaintenancePermit(fixture.current.installDir, fixture.journal.OperationID, "candidate_health", fixture.candidate.manifest.Version)
	require.NoError(t, err)
	require.NoError(t, fixture.lock.Close())

	_, err = IssueMaintenancePermit(fixture.current.installDir, fixture.journal.OperationID, "candidate_health", fixture.candidate.manifest.Version)
	require.Error(t, err)
	_, err = consumeMaintenancePermitAt(fixture.current.installDir, "candidate_health", bytes.NewReader(frame), maintenancePermitBackendPath(fixture.candidate), time.Now)
	require.Error(t, err)
}

func TestMaintenancePermitRejectsParentLockReleasedWhileReading(t *testing.T) {
	fixture := newMaintenancePermitTestFixture(t)
	frame, err := IssueMaintenancePermit(fixture.current.installDir, fixture.journal.OperationID, "candidate_health", fixture.candidate.manifest.Version)
	require.NoError(t, err)
	reader := &maintenancePermitReleasingReader{frame: frame, lock: fixture.lock}

	_, err = consumeMaintenancePermitAt(fixture.current.installDir, "candidate_health", reader, maintenancePermitBackendPath(fixture.candidate), time.Now)
	require.Error(t, err)
	require.NoError(t, reader.closeErr)
	_, err = os.Stat(filepath.Join(fixture.current.installDir, maintenanceDirName, maintenancePermitFileName))
	require.NoError(t, err)
	require.ErrorIs(t, CheckMaintenanceGate(fixture.current.installDir), ErrMaintenanceRequired)
}

func TestMaintenancePermitRestoreRules(t *testing.T) {
	fixture := newMaintenancePermitTestFixture(t)
	require.NoError(t, advanceMaintenance(fixture.current.installDir, fixture.journal.OperationID, MaintenanceCommitting, MaintenanceRestoreRequired))
	require.NoError(t, advanceMaintenance(fixture.current.installDir, fixture.journal.OperationID, MaintenanceRestoreRequired, MaintenanceRestoring))

	frame, err := IssueMaintenancePermit(fixture.current.installDir, fixture.journal.OperationID, "revoke_sessions", fixture.current.manifest.Version)
	require.NoError(t, err)
	claims, err := consumeMaintenancePermitAt(fixture.current.installDir, "revoke_sessions", bytes.NewReader(frame), maintenancePermitBackendPath(fixture.current), time.Now)
	require.NoError(t, err)
	require.Equal(t, fixture.journal.OperationID, claims.OperationID)
	require.Equal(t, fixture.current.manifest.Version, claims.Version)
	require.Equal(t, "revoke_sessions", claims.Purpose)
}

func newMaintenancePermitTestFixture(t *testing.T) maintenancePermitTestFixture {
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
	return maintenancePermitTestFixture{current: current, candidate: candidate, journal: journal, lock: lock}
}

func mutateMaintenancePermitEnvelope(t *testing.T, path string, mutate func(*maintenancePermitEnvelope)) {
	t.Helper()
	raw, err := readSecureConfigFile(path)
	require.NoError(t, err)
	var envelope maintenancePermitEnvelope
	require.NoError(t, decodeReleaseJSON(raw, &envelope))
	mutate(&envelope)
	updated, err := json.Marshal(envelope)
	require.NoError(t, err)
	require.NoError(t, writeSecureConfigFile(path, updated, true, nil))
}

func maintenancePermitBackendPath(fixture testReleaseFixture) string {
	return filepath.Join(fixture.releaseDir, "backend", "uvp-server.exe")
}

type maintenancePermitReleasingReader struct {
	frame    []byte
	lock     *InstanceLock
	released bool
	closeErr error
}

type maintenancePermitNoEOFReader struct {
	frame []byte
}

func (reader *maintenancePermitNoEOFReader) Read(destination []byte) (int, error) {
	if len(reader.frame) == 0 {
		return 0, io.ErrNoProgress
	}
	n := copy(destination, reader.frame)
	reader.frame = reader.frame[n:]
	return n, io.ErrNoProgress
}

func (reader *maintenancePermitReleasingReader) Read(destination []byte) (int, error) {
	if !reader.released {
		reader.released = true
		reader.closeErr = reader.lock.Close()
	}
	if len(reader.frame) == 0 {
		return 0, io.EOF
	}
	n := copy(destination, reader.frame)
	reader.frame = reader.frame[n:]
	return n, nil
}
