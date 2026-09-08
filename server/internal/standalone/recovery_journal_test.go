package standalone

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecoveryJournalRejectsIncompleteOrUnsealedProof(t *testing.T) {
	j := recoveryJournal{Schema: 1, OperationID: strings.Repeat("a", 64), BackupManifestSHA256: strings.Repeat("b", 64), OldVersion: "v1", OldCurrentSHA256: strings.Repeat("c", 64), ReleaseSetSHA256: strings.Repeat("d", 64), Phase: "staging"}
	raw, err := json.Marshal(j)
	require.NoError(t, err)
	for _, mode := range []string{"missing-field", "unknown-field", "unknown-phase", "incomplete-seal", "premature-seal"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			gate := filepath.Join(root, maintenanceDirName)
			require.NoError(t, os.Mkdir(gate, 0700))
			require.NoError(t, protectConfigDir(gate, true))
			var object map[string]any
			require.NoError(t, json.Unmarshal(raw, &object))
			switch mode {
			case "missing-field":
				delete(object, "failed_data_sha256")
			case "unknown-field":
				object["unexpected"] = true
			case "unknown-phase":
				object["phase"] = "complete"
			case "incomplete-seal":
				object["phase"] = "staged"
			case "premature-seal":
				object["staged_data_sha256"] = strings.Repeat("e", 64)
			}
			bad, err := json.Marshal(object)
			require.NoError(t, err)
			require.NoError(t, writeSecureConfigFile(filepath.Join(gate, "recovery.json"), bad, false, nil))
			_, err = readRecoveryJournal(root)
			require.Error(t, err)
			require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)
		})
	}
}

func TestRecoveryJournalRequiresOrderedOwnedTransitions(t *testing.T) {
	root := t.TempDir()
	lock, err := AcquireInstanceLock(root)
	require.NoError(t, err)
	defer lock.Close()
	outer := maintenanceTestJournal(root)
	outer.ReleaseSetSHA256 = strings.Repeat("d", 64)
	require.NoError(t, createMaintenanceJournal(root, outer))
	require.NoError(t, advanceMaintenance(root, outer.OperationID, MaintenanceUpgrading, MaintenanceRestoreRequired))
	j := recoveryJournal{Schema: 1, OperationID: outer.OperationID, BackupManifestSHA256: outer.BackupManifestSHA256,
		OldVersion: outer.OldVersion, OldCurrentSHA256: outer.OldCurrentSHA256, ReleaseSetSHA256: outer.ReleaseSetSHA256, Phase: "staging"}
	require.NoError(t, persistRecoveryJournal(root, nil, j))
	require.Error(t, persistRecoveryJournal(root, nil, j))
	loaded, err := readRecoveryJournal(root)
	require.NoError(t, err)
	require.Equal(t, j, loaded)
	next := j
	next.Phase = "staged"
	next.StagedConfigSHA256, next.StagedDataSHA256 = strings.Repeat("1", 64), strings.Repeat("2", 64)
	next.FailedConfigSHA256, next.FailedDataSHA256 = strings.Repeat("3", 64), strings.Repeat("4", 64)
	require.Error(t, persistRecoveryJournal(root, &j, next)) // outer phase has not entered restoring
	require.NoError(t, advanceMaintenance(root, outer.OperationID, MaintenanceRestoreRequired, MaintenanceRestoring))
	bad := next
	bad.OperationID = strings.Repeat("e", 64)
	require.Error(t, persistRecoveryJournal(root, &j, bad))
	bad = next
	bad.Phase = "config_published"
	require.Error(t, persistRecoveryJournal(root, &j, bad))
	require.NoError(t, persistRecoveryJournal(root, &j, next))
	require.Error(t, persistRecoveryJournal(root, &j, next)) // stale writer
	j = next
	for _, phase := range []string{"config_isolated", "config_published", "data_isolated", "data_published", "pointer_restored"} {
		next = j
		next.Phase = phase
		bad = next
		bad.StagedDataSHA256 = strings.Repeat("f", 64)
		require.Error(t, persistRecoveryJournal(root, &j, bad))
		require.NoError(t, persistRecoveryJournal(root, &j, next))
		j = next
	}
	require.NoError(t, lock.Close())
	require.Error(t, persistRecoveryJournal(root, &j, j))
	loaded, err = readRecoveryJournal(root)
	require.NoError(t, err)
	require.Equal(t, j, loaded)
	require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)
	path := filepath.Join(root, maintenanceDirName, "recovery.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"schema":1,"schema":1}`), 0600))
	_, err = readRecoveryJournal(root)
	require.Error(t, err)
}
