package standalone

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpgradeFinishArchivesOnlyVerifiedCompletion(t *testing.T) {
	for _, mode := range []string{"complete", "destination-exists", "rename-fails", "rename-succeeded-with-error", "both-missing", "both-present"} {
		t.Run(mode, func(t *testing.T) {
			owner := completedUpgradeFixture(t)
			destination := filepath.Join(owner.paths.InstallDir, ".uvp-completed-"+owner.journal.OperationID)
			if mode == "destination-exists" {
				require.NoError(t, os.Mkdir(destination, 0700))
			}
			rename := func(source, target string) error {
				switch mode {
				case "rename-fails":
					return errors.New("injected rename failure")
				case "both-missing":
					require.NoError(t, os.RemoveAll(source)) // isolated fault injection only
					return errors.New("injected missing source and target")
				case "both-present":
					require.NoError(t, os.Mkdir(target, 0700))
					return errors.New("injected competing destination")
				}
				if err := backupPublish(source, target); err != nil {
					return err
				}
				if mode == "rename-succeeded-with-error" {
					return errors.New("injected uncertain success")
				}
				return nil
			}
			err := owner.finishWithRename(context.Background(), rename)
			if mode == "complete" || mode == "rename-succeeded-with-error" {
				require.NoError(t, err)
				require.NoError(t, CheckMaintenanceGate(owner.paths.InstallDir))
				archive, err := readMaintenanceJournalFile(filepath.Join(destination, "journal.json"))
				require.NoError(t, err)
				require.Equal(t, owner.journal.OperationID, archive.OperationID)
				require.Equal(t, MaintenanceCompletionReady, archive.Phase)
			} else {
				require.Error(t, err)
				require.ErrorIs(t, CheckMaintenanceGate(owner.paths.InstallDir), ErrMaintenanceRequired)
			}
			_, err = AcquireInstanceLock(owner.paths.InstallDir)
			require.ErrorIs(t, err, ErrInstanceRunning)
			require.NoError(t, owner.Close())
			lock, err := AcquireInstanceLock(owner.paths.InstallDir)
			require.NoError(t, err)
			require.NoError(t, lock.Close())
		})
	}
}
