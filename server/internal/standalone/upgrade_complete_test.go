package standalone

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func completedUpgradeFixture(t *testing.T) *upgradePreparation {
	t.Helper()
	paths := newBackupTestPaths(t)
	candidate := newTestReleaseFixtureAt(t, paths.InstallDir, "t25.3", false)
	current, err := LoadRelease(paths.InstallDir)
	require.NoError(t, err)
	oldHash, err := releaseFileSHA256(current.BackendExe)
	require.NoError(t, err)
	newHash, err := releaseFileSHA256(filepath.Join(candidate.releaseDir, filepath.FromSlash(releaseBackendPath)))
	require.NoError(t, err)
	prepared, err := prepareUpgradeStoppedWithTrust(context.Background(), paths, candidate.manifest.Version, filepath.Join(filepath.Dir(paths.InstallDir), "complete-backup"), oldHash+","+newHash)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, prepared.Close()) })
	require.NoError(t, prepared.execute(context.Background(), func(context.Context, Paths, string, string, string) error { return nil }))
	return prepared
}

func TestUpgradeCompletionProofRetainsGate(t *testing.T) {
	for _, mode := range []string{"complete", "closed", "no-success-proof", "permit", "current-changed", "release-changed", "backup-changed"} {
		t.Run(mode, func(t *testing.T) {
			owner := completedUpgradeFixture(t)
			before, err := ReadMaintenanceJournal(owner.paths.InstallDir)
			require.NoError(t, err)
			require.NotEmpty(t, before.ReleaseSetSHA256)
			switch mode {
			case "closed":
				require.NoError(t, owner.Close())
			case "no-success-proof":
				owner.committed = false
			case "permit":
				require.NoError(t, os.WriteFile(maintenancePermitPath(owner.paths.InstallDir), []byte("unexpected permit"), 0600))
			case "current-changed":
				require.NoError(t, os.WriteFile(filepath.Join(owner.paths.InstallDir, "current.json"), []byte(`{"version":"t25.3"}`+"\n"), 0600))
			case "release-changed":
				newTestReleaseFixtureAt(t, owner.paths.InstallDir, "t25.4", false)
			case "backup-changed":
				require.NoError(t, os.Remove(filepath.Join(owner.journal.BackupRoot, "complete.json")))
			}
			err = owner.markCompletionReady(context.Background())
			after, readErr := ReadMaintenanceJournal(owner.paths.InstallDir)
			require.NoError(t, readErr)
			if mode == "complete" {
				require.NoError(t, err)
				require.Equal(t, MaintenanceCompletionReady, after.Phase)
				require.NotEmpty(t, after.CompletedAt)
				require.Len(t, after.CandidateCurrentSHA256, 64)
				require.NoError(t, owner.markCompletionReady(context.Background()))
				_, err := IssueMaintenancePermit(owner.paths.InstallDir, owner.journal.OperationID, "candidate_health", owner.journal.CandidateVersion)
				require.Error(t, err)
			} else {
				require.Error(t, err)
				require.Equal(t, before, after)
			}
			require.ErrorIs(t, CheckMaintenanceGate(owner.paths.InstallDir), ErrMaintenanceRequired)
		})
	}
}

func TestCompletionJournalCannotGrantOtherPhasesSuccess(t *testing.T) {
	owner := completedUpgradeFixture(t)
	journal := owner.journal
	journal.CandidateCurrentSHA256 = strings.Repeat("a", 64)
	require.Error(t, validateMaintenanceJournal(journal))
	journal.Phase = MaintenanceCompletionReady
	require.Error(t, validateMaintenanceJournal(journal))
}
