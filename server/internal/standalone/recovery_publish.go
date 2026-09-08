package standalone

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Caller retains InstanceLock throughout. Each move and its identity checks
// share ConfigLock. A crash before progress persistence is reconciled from the
// complete six-directory state on retry; this never clears the outer gate.
func publishRecoveryDirectories(ctx context.Context, root, operation, trust string, hook func(string) error) error {
	for {
		var previous, next recoveryJournal
		finished := false
		err := withConfigLock(root, func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := requireMaintenanceInstanceLock(root); err != nil {
				return err
			}
			outer, err := ReadMaintenanceJournal(root)
			if err != nil {
				return err
			}
			if outer.OperationID != operation || outer.Phase != MaintenanceRestoring {
				return errors.New("recovery publication requires owning restore operation")
			}
			previous, err = readRecoveryJournal(root)
			if err != nil {
				return err
			}
			if previous.OperationID != operation || previous.BackupManifestSHA256 != outer.BackupManifestSHA256 || previous.OldVersion != outer.OldVersion || previous.OldCurrentSHA256 != outer.OldCurrentSHA256 || previous.ReleaseSetSHA256 != outer.ReleaseSetSHA256 {
				return errors.New("recovery publication identity mismatch")
			}
			phase := recoveryPhaseIndex(previous.Phase)
			if phase < 1 || phase > 5 {
				return errors.New("recovery directories are not sealed for publication")
			}
			if _, _, err := loadMaintenanceReleasesWithTrust(root, outer.CandidateVersion, trust); err != nil {
				return err
			}
			releases, identity, err := installedMaintenanceReleaseSnapshot(root)
			if err != nil {
				return err
			}
			if identity != outer.ReleaseSetSHA256 {
				return errors.New("recovery release set changed")
			}
			if err := backupComponentsStopped(releases...); err != nil {
				return err
			}
			if _, err := os.Lstat(maintenancePermitPath(root)); !errors.Is(err, os.ErrNotExist) {
				return errors.New("recovery publication has outstanding permit")
			}
			if _, err := VerifyBackup(ctx, outer.BackupRoot); err != nil {
				return err
			}
			backupSHA, err := releaseFileSHA256(filepath.Join(outer.BackupRoot, backupManifestFile))
			if err != nil {
				return err
			}
			if backupSHA != outer.BackupManifestSHA256 {
				return errors.New("recovery backup changed")
			}
			currentSHA, err := releaseFileSHA256(filepath.Join(root, "current.json"))
			if err != nil {
				return err
			}
			candidateRaw, err := json.Marshal(releaseCurrentPointer{Version: outer.CandidateVersion})
			if err != nil {
				return err
			}
			if currentSHA != outer.OldCurrentSHA256 && currentSHA != currentPointerSHA256(candidateRaw) {
				return errors.New("recovery current pointer changed")
			}
			if err := checkRecoveryDirectoryState(ctx, root, previous, phase); err != nil {
				if phase == 5 {
					return err
				}
				if postErr := checkRecoveryDirectoryState(ctx, root, previous, phase+1); postErr != nil {
					return errors.Join(err, postErr)
				}
			}
			if phase == 5 {
				finished = true
				return nil
			}
			work := filepath.Join(root, maintenanceDirName, "work", operation)
			failed := filepath.Join(root, maintenanceDirName, "failed", operation)
			moves := [][3]string{
				{filepath.Join(root, "config"), filepath.Join(failed, "config"), previous.FailedConfigSHA256},
				{filepath.Join(work, "config"), filepath.Join(root, "config"), previous.StagedConfigSHA256},
				{filepath.Join(root, "data"), filepath.Join(failed, "data"), previous.FailedDataSHA256},
				{filepath.Join(work, "data"), filepath.Join(root, "data"), previous.StagedDataSHA256},
			}
			move := moves[phase-1]
			if err := recoveryMoveTree(ctx, move[0], move[1], move[2], backupPublish); err != nil {
				return err
			}
			if err := checkRecoveryDirectoryState(ctx, root, previous, phase+1); err != nil {
				return err
			}
			next = previous
			next.Phase = []string{"config_isolated", "config_published", "data_isolated", "data_published"}[phase-1]
			if hook != nil {
				return hook(next.Phase)
			}
			return nil
		})
		if err != nil || finished {
			return err
		}
		if err := persistRecoveryJournal(root, &previous, next); err != nil {
			return err
		}
	}
}

func checkRecoveryDirectoryState(ctx context.Context, root string, j recoveryJournal, phase int) error {
	work := filepath.Join(root, maintenanceDirName, "work", j.OperationID)
	failed := filepath.Join(root, maintenanceDirName, "failed", j.OperationID)
	paths := []string{filepath.Join(root, "config"), filepath.Join(root, "data"), filepath.Join(work, "config"), filepath.Join(work, "data"), filepath.Join(failed, "config"), filepath.Join(failed, "data")}
	expected := []string{j.FailedConfigSHA256, j.FailedDataSHA256, j.StagedConfigSHA256, j.StagedDataSHA256, "", ""}
	if phase >= 2 {
		expected[0], expected[4] = "", j.FailedConfigSHA256
	}
	if phase >= 3 {
		expected[0], expected[2] = j.StagedConfigSHA256, ""
	}
	if phase >= 4 {
		expected[1], expected[5] = "", j.FailedDataSHA256
	}
	if phase >= 5 {
		expected[1], expected[3] = j.StagedDataSHA256, ""
	}
	for i, path := range paths {
		if expected[i] == "" {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				return errors.New("unexpected recovery directory present")
			}
			continue
		}
		actual, err := recoveryTreeIdentity(ctx, path)
		if err != nil {
			return err
		}
		if actual != expected[i] {
			return errors.New("recovery directory inventory mismatch")
		}
	}
	return nil
}
