package standalone

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUncleanMaintenanceJournalKeepsSchema1Contract(t *testing.T) {
	root := t.TempDir()
	journal := maintenanceTestJournal(root)
	raw, err := json.Marshal(journal)
	require.NoError(t, err)
	fields, err := decodeStrictJSONObject(raw)
	require.NoError(t, err)
	require.Equal(t, map[string]struct{}{
		"schema": {}, "operation_id": {}, "old_version": {}, "candidate_version": {},
		"old_current_sha256": {}, "backup_root": {}, "backup_manifest_sha256": {},
		"phase": {}, "created_at": {},
	}, uncleanJSONKeySet(fields))
	require.NoError(t, validateMaintenanceJournal(journal))

	require.Error(t, validateMaintenanceJournal(MaintenanceJournal{
		Schema: 1, OperationID: journal.OperationID, OldVersion: journal.OldVersion,
		CandidateVersion: journal.CandidateVersion, Kind: maintenanceKindUnclean,
		OldCurrentSHA256: journal.OldCurrentSHA256, BackupRoot: journal.BackupRoot,
		BackupManifestSHA256: journal.BackupManifestSHA256, Phase: journal.Phase,
		CreatedAt: journal.CreatedAt,
	}))

	require.NoError(t, createMaintenanceJournal(root, journal))
	path := filepath.Join(root, maintenanceDirName, "journal.json")
	raw, err = readSecureConfigFile(path)
	require.NoError(t, err)
	fields, err = decodeStrictJSONObject(raw)
	require.NoError(t, err)
	for _, key := range []string{"kind", "run_marker_sha256", "source_config_sha256", "source_data_sha256"} {
		fields[key] = json.RawMessage(`""`)
		updated, marshalErr := json.Marshal(fields)
		require.NoError(t, marshalErr)
		require.NoError(t, writeSecureConfigFile(path, updated, true, nil))
		_, readErr := ReadMaintenanceJournal(root)
		require.Error(t, readErr, key)
		delete(fields, key)
		raw, err = json.Marshal(journal)
		require.NoError(t, err)
		fields, err = decodeStrictJSONObject(raw)
		require.NoError(t, err)
	}
}

func TestUncleanMaintenanceJournalUsesStrictSchema2KeysAndIdentities(t *testing.T) {
	root := t.TempDir()
	journal := uncleanMaintenanceTestJournal(root)
	require.NoError(t, validateMaintenanceJournal(journal))
	raw, err := json.Marshal(journal)
	require.NoError(t, err)
	fields, err := decodeStrictJSONObject(raw)
	require.NoError(t, err)
	require.Equal(t, map[string]struct{}{
		"schema": {}, "operation_id": {}, "old_version": {}, "kind": {}, "run_marker_sha256": {},
		"source_config_sha256": {}, "source_data_sha256": {}, "old_current_sha256": {},
		"backup_root": {}, "backup_manifest_sha256": {}, "phase": {}, "created_at": {},
		"release_set_sha256": {},
	}, uncleanJSONKeySet(fields))
	require.NotContains(t, fields, "candidate_version")

	lock, err := AcquireInstanceLock(root)
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Close()) }()
	require.NoError(t, createUncleanPreparingJournal(root, journal))
	got, err := ReadMaintenanceJournal(root)
	require.NoError(t, err)
	require.Equal(t, journal, got)

	path := filepath.Join(root, maintenanceDirName, "journal.json")
	mutations := []struct {
		name   string
		mutate func(map[string]json.RawMessage)
	}{
		{
			name: "empty candidate key",
			mutate: func(fields map[string]json.RawMessage) {
				fields["candidate_version"] = json.RawMessage(`""`)
			},
		},
		{
			name: "unknown key",
			mutate: func(fields map[string]json.RawMessage) {
				fields["unexpected"] = json.RawMessage(`true`)
			},
		},
		{
			name: "wrong kind",
			mutate: func(fields map[string]json.RawMessage) {
				fields["kind"] = json.RawMessage(`"upgrade"`)
			},
		},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			raw, err := readSecureConfigFile(path)
			require.NoError(t, err)
			fields, err := decodeStrictJSONObject(raw)
			require.NoError(t, err)
			mutation.mutate(fields)
			updated, err := json.Marshal(fields)
			require.NoError(t, err)
			require.NoError(t, writeSecureConfigFile(path, updated, true, nil))
			_, err = ReadMaintenanceJournal(root)
			require.Error(t, err)
			require.NoError(t, writeSecureConfigFile(path, raw, true, nil))
		})
	}

	for _, field := range []string{"OperationID", "OldCurrentSHA256", "ReleaseSetSHA256", "RunMarkerSHA256", "SourceConfigSHA256", "SourceDataSHA256"} {
		bad := journal
		switch field {
		case "OperationID":
			bad.OperationID = ""
		case "OldCurrentSHA256":
			bad.OldCurrentSHA256 = ""
		case "ReleaseSetSHA256":
			bad.ReleaseSetSHA256 = ""
		case "RunMarkerSHA256":
			bad.RunMarkerSHA256 = ""
		case "SourceConfigSHA256":
			bad.SourceConfigSHA256 = ""
		case "SourceDataSHA256":
			bad.SourceDataSHA256 = ""
		}
		require.Error(t, validateMaintenanceJournal(bad), field)
	}

	bad := journal
	bad.BackupManifestSHA256 = strings.Repeat("a", 64)
	bad.Phase = MaintenancePreparing
	// A preparing journal may not claim that a backup has completed.
	require.Error(t, validateMaintenanceJournal(bad))
}

func TestUncleanMaintenanceJournalTransitionsCannotUseUpgradePhases(t *testing.T) {
	root := t.TempDir()
	journal := uncleanMaintenanceTestJournal(root)
	lock, err := AcquireInstanceLock(root)
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Close()) }()
	require.NoError(t, createUncleanPreparingJournal(root, journal))

	for _, next := range []MaintenancePhase{MaintenanceUpgrading, MaintenanceCommitting, MaintenanceCompletionReady} {
		require.Error(t, advanceMaintenance(root, journal.OperationID, MaintenancePreparing, next), next)
	}
	journal.Phase = MaintenanceRestoreRequired
	journal.BackupManifestSHA256 = strings.Repeat("0", 64)
	uncleanWriteMaintenanceJournal(t, root, journal)
	require.Error(t, advanceMaintenance(root, journal.OperationID, MaintenanceRestoreRequired, MaintenanceUpgrading))
	require.Error(t, advanceMaintenance(root, journal.OperationID, MaintenanceRestoreRequired, MaintenanceCommitting))
	require.NoError(t, advanceMaintenance(root, journal.OperationID, MaintenanceRestoreRequired, MaintenanceRestoring))
	require.Error(t, advanceMaintenance(root, journal.OperationID, MaintenanceRestoring, MaintenanceCommitting))
	require.NoError(t, advanceMaintenance(root, journal.OperationID, MaintenanceRestoring, MaintenanceAwaitingConfirmation))
	require.Error(t, advanceMaintenance(root, journal.OperationID, MaintenanceAwaitingConfirmation, MaintenanceCompletionReady))
}

func TestUncleanMaintenanceJournalCreationRequiresOwnerAndRetainsGate(t *testing.T) {
	root := t.TempDir()
	journal := uncleanMaintenanceTestJournal(root)
	require.Error(t, createUncleanPreparingJournal(root, journal))
	_, err := os.Stat(filepath.Join(root, maintenanceDirName))
	require.ErrorIs(t, err, os.ErrNotExist)

	lock, err := AcquireInstanceLock(root)
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Close()) }()
	require.NoError(t, createUncleanPreparingJournal(root, journal))
	require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)
	require.Error(t, createUncleanPreparingJournal(root, journal))
	require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)
	got, err := ReadMaintenanceJournal(root)
	require.NoError(t, err)
	require.Equal(t, journal, got)
}

func TestUncleanMaintenanceRejectsSchema1PreparationEntrypoints(t *testing.T) {
	root := t.TempDir()
	journal := uncleanMaintenanceTestJournal(root)
	lock, err := AcquireInstanceLock(root)
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Close()) }()

	require.Error(t, createPreparingMaintenanceJournal(root, journal))
	_, err = os.Stat(filepath.Join(root, maintenanceDirName))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestUncleanMaintenanceRejectsSchema1PromotionWithoutChangingJournal(t *testing.T) {
	root := t.TempDir()
	journal := uncleanMaintenanceTestJournal(root)
	lock, err := AcquireInstanceLock(root)
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Close()) }()
	require.NoError(t, createUncleanPreparingJournal(root, journal))

	path := filepath.Join(root, maintenanceDirName, "journal.json")
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Error(t, promotePreparedMaintenance(context.Background(), root, journal.OperationID, journal.BackupRoot))
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestMaintenanceSourceSelectionUsesValidatedJournalVersion(t *testing.T) {
	root := t.TempDir()
	schema1 := maintenanceTestJournal(root)
	require.NoError(t, validateMaintenanceJournal(schema1))
	require.Equal(t, schema1.CandidateVersion, maintenanceSourceSelection(schema1))

	schema2 := uncleanMaintenanceTestJournal(root)
	require.NoError(t, validateMaintenanceJournal(schema2))
	require.Equal(t, schema2.OldVersion, maintenanceSourceSelection(schema2))
}

func TestUncleanMaintenancePermitSelectionBindsOldVersionAndPurpose(t *testing.T) {
	journal := uncleanMaintenanceTestJournal(t.TempDir())
	for _, phase := range []MaintenancePhase{MaintenancePreparing, MaintenanceRestoreRequired, MaintenanceAwaitingConfirmation} {
		journal.Phase = phase
		require.Error(t, validateMaintenancePermitSelection(journal, "revoke_sessions", journal.OldVersion), phase)
		require.Error(t, validateMaintenancePermitSelection(journal, "db_check", journal.OldVersion), phase)
	}

	journal.Phase = MaintenanceRestoring
	for _, purpose := range []string{"revoke_sessions", "db_check"} {
		require.NoError(t, validateMaintenancePermitSelection(journal, purpose, journal.OldVersion), purpose)
	}
	for _, purpose := range []string{"bootstrap_db", "migrate_up", "candidate_health", "migrate_down"} {
		require.Error(t, validateMaintenancePermitSelection(journal, purpose, journal.OldVersion), purpose)
	}
	require.Error(t, validateMaintenancePermitSelection(journal, "db_check", "v2"))
}

func TestUncleanMaintenancePermitIssueAndConsumeUsesOldVersion(t *testing.T) {
	fixture := newMaintenancePermitTestFixture(t)
	root := fixture.current.installDir
	unclean := uncleanMaintenanceTestJournal(root)
	unclean.OldVersion = fixture.current.manifest.Version
	unclean.BackupRoot = filepath.Join(root, "backup")
	unclean.OldCurrentSHA256 = maintenancePermitCurrentPointerSHA(t, root)
	unclean.Phase = MaintenanceRestoring
	unclean.BackupManifestSHA256 = strings.Repeat("0", 64)
	path := filepath.Join(root, maintenanceDirName, "journal.json")
	raw, err := json.Marshal(unclean)
	require.NoError(t, err)
	require.NoError(t, writeSecureConfigFile(path, raw, true, nil))

	frame, err := IssueMaintenancePermit(root, unclean.OperationID, "db_check", unclean.OldVersion)
	require.NoError(t, err)
	claims, err := consumeMaintenancePermitAt(root, "db_check", bytes.NewReader(frame), maintenancePermitBackendPath(fixture.current), time.Now)
	require.NoError(t, err)
	require.Equal(t, MaintenancePermitClaims{OperationID: unclean.OperationID, Purpose: "db_check", Version: unclean.OldVersion}, claims)

	_, err = IssueMaintenancePermit(root, unclean.OperationID, "db_check", "v2")
	require.Error(t, err)
}

func uncleanMaintenanceTestJournal(root string) MaintenanceJournal {
	return MaintenanceJournal{
		Schema:               2,
		OperationID:          strings.Repeat("a", 64),
		OldVersion:           "v1",
		Kind:                 maintenanceKindUnclean,
		RunMarkerSHA256:      strings.Repeat("b", 64),
		SourceConfigSHA256:   strings.Repeat("c", 64),
		SourceDataSHA256:     strings.Repeat("d", 64),
		OldCurrentSHA256:     strings.Repeat("e", 64),
		BackupRoot:           filepath.Join(root, "external-backup"),
		BackupManifestSHA256: "",
		Phase:                MaintenancePreparing,
		CreatedAt:            time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC),
		ReleaseSetSHA256:     strings.Repeat("f", 64),
	}
}

func uncleanJSONKeySet(fields map[string]json.RawMessage) map[string]struct{} {
	keys := make(map[string]struct{}, len(fields))
	for key := range fields {
		keys[key] = struct{}{}
	}
	return keys
}

func maintenancePermitCurrentPointerSHA(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "current.json"))
	require.NoError(t, err)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func uncleanWriteMaintenanceJournal(t *testing.T, root string, journal MaintenanceJournal) {
	t.Helper()
	raw, err := json.Marshal(journal)
	require.NoError(t, err)
	require.NoError(t, writeSecureConfigFile(filepath.Join(root, maintenanceDirName, "journal.json"), raw, true, nil))
}
