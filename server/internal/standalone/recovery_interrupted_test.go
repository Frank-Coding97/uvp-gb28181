package standalone

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRecoveryResumesAfterUnconsumedStagePermit(t *testing.T) {
	owner := completedUpgradeFixture(t)
	root, op := owner.paths.InstallDir, owner.journal.OperationID
	require.NoError(t, advanceMaintenance(root, op, MaintenanceCommitting, MaintenanceRestoreRequired))
	require.NoError(t, owner.Close())
	calls := 0
	runner := func(_ context.Context, _ Paths, operation, purpose, version string) error {
		calls++
		if calls == 1 {
			frame, err := IssueMaintenancePermit(root, operation, purpose, version)
			clear(frame)
			require.NoError(t, err)
			return errors.New("simulated owner exit before child consumes permit")
		}
		return nil
	}
	redis := func(_ context.Context, _, _, target, _ string, _ int) error {
		return os.WriteFile(filepath.Join(target, "fixture"), []byte("fresh"), 0600)
	}
	_, err := restoreStoppedWithRunners(context.Background(), owner.paths, op, owner.trust, runner, redis)
	require.Error(t, err)
	require.FileExists(t, maintenancePermitPath(root))
	require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)
	result, err := restoreStoppedWithRunners(context.Background(), owner.paths, op, owner.trust, runner, redis)
	require.NoError(t, err)
	require.Equal(t, MaintenanceAwaitingConfirmation, result.Phase)
	_, err = os.Lstat(maintenancePermitPath(root))
	require.True(t, errors.Is(err, os.ErrNotExist))
}

func TestRecoveryAdmitsInterruptedUpgradeCommit(t *testing.T) {
	owner := completedUpgradeFixture(t)
	journal, err := ReadMaintenanceJournal(owner.paths.InstallDir)
	require.NoError(t, err)
	require.Equal(t, MaintenanceCommitting, journal.Phase)
	require.NoError(t, owner.Close())
	result, err := restoreStoppedWithRunners(context.Background(), owner.paths, owner.journal.OperationID, owner.trust,
		func(context.Context, Paths, string, string, string) error { return nil },
		func(_ context.Context, _, _, target, _ string, _ int) error {
			return os.WriteFile(filepath.Join(target, "fixture"), []byte("fresh"), 0600)
		})
	require.NoError(t, err)
	require.Equal(t, MaintenanceAwaitingConfirmation, result.Phase)
	selected, err := LoadRelease(owner.paths.InstallDir)
	require.NoError(t, err)
	require.Equal(t, owner.journal.OldVersion, selected.Version)
}

func TestRecoveryDoesNotRetireForeignOrDamagedPermit(t *testing.T) {
	for _, mode := range []string{"wrong-operation", "wrong-backend", "wrong-purpose", "wrong-version", "future-expiry", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			owner := completedUpgradeFixture(t)
			root, op := owner.paths.InstallDir, owner.journal.OperationID
			require.NoError(t, advanceMaintenance(root, op, MaintenanceCommitting, MaintenanceRestoreRequired))
			require.NoError(t, owner.Close())
			run := func(_ context.Context, _ Paths, operation, purpose, version string) error {
				frame, err := IssueMaintenancePermit(root, operation, purpose, version)
				clear(frame)
				require.NoError(t, err)
				return errors.New("interrupted")
			}
			redis := func(context.Context, string, string, string, string, int) error { return nil }
			_, err := restoreStoppedWithRunners(context.Background(), owner.paths, op, owner.trust, run, redis)
			require.Error(t, err)
			envelope, err := readMaintenancePermitEnvelope(root)
			require.NoError(t, err)
			switch mode {
			case "wrong-operation":
				envelope.OperationID = strings.Repeat("b", 64)
			case "wrong-backend":
				envelope.BackendSHA256 = strings.Repeat("b", 64)
			case "wrong-purpose":
				envelope.Purpose = "candidate_health"
			case "wrong-version":
				envelope.Version = owner.journal.CandidateVersion
			case "future-expiry":
				envelope.ExpiresAt = time.Now().Add(time.Hour)
			}
			raw, err := json.Marshal(envelope)
			require.NoError(t, err)
			if mode == "malformed" {
				raw = []byte("{")
			}
			require.NoError(t, writeSecureConfigFile(maintenancePermitPath(root), raw, true, nil))
			before, err := recoveryTreeIdentity(context.Background(), filepath.Join(root, maintenanceDirName))
			require.NoError(t, err)
			_, err = restoreStoppedWithRunners(context.Background(), owner.paths, op, owner.trust, run, redis)
			require.Error(t, err)
			after, err := recoveryTreeIdentity(context.Background(), filepath.Join(root, maintenanceDirName))
			require.NoError(t, err)
			require.Equal(t, before, after)
			require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)
		})
	}
}

func TestInterruptedRecoveryAdmissionPointerAndPhase(t *testing.T) {
	for _, tc := range []struct {
		phase   MaintenancePhase
		pointer string
		allowed bool
	}{
		{MaintenanceUpgrading, "old", true},
		{MaintenanceUpgrading, "candidate", false},
		{MaintenanceCommitting, "old", true},
		{MaintenanceCommitting, "candidate", true},
		{MaintenanceCommitting, "unknown", false},
		{MaintenancePreparing, "old", false},
		{MaintenanceAwaitingConfirmation, "old", false},
	} {
		t.Run(string(tc.phase)+"-"+tc.pointer, func(t *testing.T) {
			owner := completedUpgradeFixture(t)
			root := owner.paths.InstallDir
			journal, err := ReadMaintenanceJournal(root)
			require.NoError(t, err)
			journal.Phase = tc.phase
			raw, err := json.Marshal(journal)
			require.NoError(t, err)
			require.NoError(t, writeSecureConfigFile(filepath.Join(root, maintenanceDirName, "journal.json"), raw, true, nil))
			pointer, err := os.ReadFile(filepath.Join(journal.BackupRoot, "install", "current.json"))
			require.NoError(t, err)
			if tc.pointer == "candidate" {
				pointer, err = json.Marshal(releaseCurrentPointer{Version: journal.CandidateVersion})
				require.NoError(t, err)
			} else if tc.pointer == "unknown" {
				pointer = []byte(`{"version":"unknown"}`)
			}
			require.NoError(t, os.WriteFile(filepath.Join(root, "current.json"), pointer, 0600))
			before, err := recoveryTreeIdentity(context.Background(), filepath.Join(root, maintenanceDirName))
			require.NoError(t, err)
			got, err := admitInterruptedRecovery(context.Background(), root, journal.OperationID, owner.trust, backupPublish)
			if tc.allowed {
				require.NoError(t, err)
				require.Equal(t, MaintenanceRestoreRequired, got.Phase)
			} else {
				require.Error(t, err)
				after, err := recoveryTreeIdentity(context.Background(), filepath.Join(root, maintenanceDirName))
				require.NoError(t, err)
				require.Equal(t, before, after)
			}
			require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)
		})
	}
}

func TestInterruptedRecoveryRetirementOutcomes(t *testing.T) {
	for _, mode := range []string{"expired", "restore-required", "uncertain-success", "rename-denied", "target-exists", "both-exist", "both-missing", "wrong-archive"} {
		t.Run(mode, func(t *testing.T) {
			owner := completedUpgradeFixture(t)
			root, op := owner.paths.InstallDir, owner.journal.OperationID
			frame, err := IssueMaintenancePermit(root, op, "candidate_health", owner.journal.CandidateVersion)
			clear(frame)
			require.NoError(t, err)
			if mode == "expired" {
				envelope, err := readMaintenancePermitEnvelope(root)
				require.NoError(t, err)
				envelope.ExpiresAt = time.Now().Add(-time.Hour)
				raw, err := json.Marshal(envelope)
				require.NoError(t, err)
				require.NoError(t, writeSecureConfigFile(maintenancePermitPath(root), raw, true, nil))
			}
			if mode == "restore-required" {
				require.NoError(t, advanceMaintenance(root, op, MaintenanceCommitting, MaintenanceRestoreRequired))
			}
			raw, err := readSecureConfigFile(maintenancePermitPath(root))
			require.NoError(t, err)
			var archive string
			rename := func(source, target string) error {
				archive = target
				switch mode {
				case "rename-denied":
					return errors.New("sharing violation")
				case "target-exists":
					require.NoError(t, writeSecureConfigFile(target, []byte("preexisting"), false, nil))
					return backupPublish(source, target)
				case "both-exist":
					return writeSecureConfigFile(target, raw, false, nil)
				case "both-missing":
					return os.Remove(source)
				case "wrong-archive":
					require.NoError(t, backupPublish(source, target))
					return writeSecureConfigFile(target, []byte("wrong"), true, nil)
				default:
					require.NoError(t, backupPublish(source, target))
					if mode == "uncertain-success" {
						return errors.New("uncertain result")
					}
					return nil
				}
			}
			got, err := admitInterruptedRecovery(context.Background(), root, op, owner.trust, rename)
			if mode == "expired" || mode == "restore-required" || mode == "uncertain-success" {
				require.NoError(t, err)
				require.Equal(t, MaintenanceRestoreRequired, got.Phase)
				archived, err := readSecureConfigFile(archive)
				require.NoError(t, err)
				require.Equal(t, raw, archived)
				require.NoFileExists(t, maintenancePermitPath(root))
			} else {
				require.Error(t, err)
				journal, err := ReadMaintenanceJournal(root)
				require.NoError(t, err)
				require.Equal(t, MaintenanceCommitting, journal.Phase)
			}
			require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)
		})
	}
}

func TestInterruptedRecoveryRejectsIdentityDriftBeforeRetirement(t *testing.T) {
	for _, mode := range []string{"trust", "backup", "backup-identity", "release-set", "operation", "sealed-permit"} {
		t.Run(mode, func(t *testing.T) {
			owner := completedUpgradeFixture(t)
			root, op, trust := owner.paths.InstallDir, owner.journal.OperationID, owner.trust
			frame, err := IssueMaintenancePermit(root, op, "db_check", owner.journal.CandidateVersion)
			clear(frame)
			require.NoError(t, err)
			switch mode {
			case "trust":
				trust = ""
			case "backup":
				require.NoError(t, os.Remove(filepath.Join(owner.journal.BackupRoot, "complete.json")))
			case "backup-identity":
				journal, err := ReadMaintenanceJournal(root)
				require.NoError(t, err)
				journal.BackupManifestSHA256 = strings.Repeat("a", 64)
				raw, err := json.Marshal(journal)
				require.NoError(t, err)
				require.NoError(t, writeSecureConfigFile(filepath.Join(root, maintenanceDirName, "journal.json"), raw, true, nil))
			case "release-set":
				newTestReleaseFixtureAt(t, root, "unfrozen", false)
			case "operation":
				op = strings.Repeat("c", 64)
			case "sealed-permit":
				// Build a genuine sealed stage first, then inject a syntactically
				// valid old-version permit; sealed stages must never retire it.
				require.NoError(t, owner.Close())
				fixture := newRecoveryPublicationFixture(t)
				defer fixture.owner.Close()
				root, op, trust = fixture.paths.InstallDir, fixture.journal.OperationID, fixture.trust
				frame, err := IssueMaintenancePermit(root, op, "db_check", fixture.journal.OldVersion)
				clear(frame)
				require.NoError(t, err)
			}
			before, err := recoveryTreeIdentity(context.Background(), filepath.Join(root, maintenanceDirName))
			require.NoError(t, err)
			_, err = admitInterruptedRecovery(context.Background(), root, op, trust, func(string, string) error {
				t.Fatal("identity failure reached permit mutation")
				return nil
			})
			require.Error(t, err)
			after, err := recoveryTreeIdentity(context.Background(), filepath.Join(root, maintenanceDirName))
			require.NoError(t, err)
			require.Equal(t, before, after)
			require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)
		})
	}
}
