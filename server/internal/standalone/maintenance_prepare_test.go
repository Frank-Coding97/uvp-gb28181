package standalone

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPreparingMaintenancePublishesCompleteIdentityAtomically(t *testing.T) {
	for _, fault := range []string{"", "before_publish", "after_publish"} {
		t.Run(fault, func(t *testing.T) {
			root := t.TempDir()
			lock, err := AcquireInstanceLock(root)
			require.NoError(t, err)
			defer lock.Close()
			journal := maintenanceTestJournal(root)
			journal.Phase, journal.BackupManifestSHA256 = MaintenancePreparing, ""
			err = createPreparingMaintenanceJournalWithHook(root, journal, func(stage string) error {
				if stage == "before_publish" {
					require.NoError(t, CheckMaintenanceGate(root))
				}
				if stage == fault {
					return errors.New("injected publication failure")
				}
				return nil
			})
			if fault == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			if fault == "before_publish" {
				require.NoError(t, CheckMaintenanceGate(root))
			} else {
				require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)
				got, err := ReadMaintenanceJournal(root)
				require.NoError(t, err)
				require.Equal(t, journal, got)
				require.Error(t, createPreparingMaintenanceJournalWithHook(root, journal, nil))
				for _, purpose := range []string{"bootstrap_db", "migrate_up", "db_check", "candidate_health", "revoke_sessions"} {
					frame, err := IssueMaintenancePermit(root, journal.OperationID, purpose, journal.CandidateVersion)
					require.Error(t, err)
					require.Empty(t, frame)
				}
			}
			leftovers, err := filepath.Glob(filepath.Join(root, ".uvp-prepare-*"))
			require.NoError(t, err)
			require.Empty(t, leftovers)
		})
	}
}

func TestPreparingMaintenanceRejectsCompletedHashAndMissingOwnership(t *testing.T) {
	root := t.TempDir()
	journal := maintenanceTestJournal(root)
	journal.Phase = MaintenancePreparing
	require.Error(t, validateMaintenanceJournal(journal))
	journal.BackupManifestSHA256 = ""
	require.NoError(t, validateMaintenanceJournal(journal))
	require.Error(t, createPreparingMaintenanceJournalWithHook(root, journal, nil))
	_, err := os.Stat(filepath.Join(root, maintenanceDirName))
	require.ErrorIs(t, err, os.ErrNotExist)
	journal.Phase = MaintenanceUpgrading
	require.Error(t, validateMaintenanceJournal(journal))
}
