package standalone

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecoveryConfirmationArchivesEntireFailedScene(t *testing.T) {
	for _, mode := range []string{"complete", "uncertain-success", "rename-failed", "target-exists", "archive-damaged", "state-changed"} {
		t.Run(mode, func(t *testing.T) {
			owner := completedUpgradeFixture(t)
			root, op := owner.paths.InstallDir, owner.journal.OperationID
			require.NoError(t, advanceMaintenance(root, op, MaintenanceCommitting, MaintenanceRestoreRequired))
			require.NoError(t, owner.Close())
			_, err := restoreStoppedWithRunners(context.Background(), owner.paths, op, owner.trust,
				func(context.Context, Paths, string, string, string) error { return nil },
				func(_ context.Context, _, _, target, _ string, _ int) error {
					return os.WriteFile(filepath.Join(target, "fixture"), []byte("fresh"), 0600)
				})
			require.NoError(t, err)
			lock, err := AcquireInstanceLock(root)
			require.NoError(t, err)
			defer lock.Close()
			j, _, err := verifyRecoveryConfirmation(context.Background(), root, op, owner.trust)
			require.NoError(t, err)
			archive := filepath.Join(root, ".uvp-recovered-"+op)
			if mode == "target-exists" {
				require.NoError(t, os.Mkdir(archive, 0700))
			}
			if mode == "state-changed" {
				require.NoError(t, os.WriteFile(filepath.Join(owner.paths.DataDir, "unexpected"), []byte("change"), 0600))
			}
			err = archiveRecoveryConfirmation(context.Background(), root, op, owner.trust, 1, strings.Repeat("a", 64), func() error { return nil }, func(source, target string) error {
				if mode == "rename-failed" {
					return errors.New("injected failure")
				}
				if err := backupPublish(source, target); err != nil {
					return err
				}
				if mode == "archive-damaged" {
					require.NoError(t, os.WriteFile(filepath.Join(target, "unexpected"), []byte("change"), 0600))
				}
				if mode == "uncertain-success" {
					return errors.New("injected uncertain result")
				}
				return nil
			})
			if mode == "complete" || mode == "uncertain-success" {
				require.NoError(t, err)
				require.NoError(t, CheckMaintenanceGate(root))
				failed := filepath.Join(archive, "failed", op)
				identity, err := recoveryTreeIdentity(context.Background(), filepath.Join(failed, "data"))
				require.NoError(t, err)
				require.Equal(t, j.FailedDataSHA256, identity)
				_, err = readSecureConfigFile(filepath.Join(archive, "confirmation.json"))
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)
			}
		})
	}
}
