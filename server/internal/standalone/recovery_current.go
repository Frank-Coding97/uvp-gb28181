package standalone

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Restore only the pointer after all six directory identities prove complete
// publication. The caller retains InstanceLock; business remains gated.
func restoreRecoveryCurrent(ctx context.Context, root, operation, trust string, hook func(string) error) error {
	var previous recoveryJournal
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
			return errors.New("pointer restoration requires owning restore operation")
		}
		if outer.Schema != 1 {
			return errors.New("unclean recovery must not restore a version pointer")
		}
		previous, err = readRecoveryJournal(root)
		if err != nil {
			return err
		}
		if !recoveryContextMatches(previous, outer) || previous.OperationID != operation || previous.BackupManifestSHA256 != outer.BackupManifestSHA256 || previous.OldVersion != outer.OldVersion || previous.OldCurrentSHA256 != outer.OldCurrentSHA256 || previous.ReleaseSetSHA256 != outer.ReleaseSetSHA256 {
			return errors.New("recovery pointer identity mismatch")
		}
		if previous.Phase != "data_published" && previous.Phase != "pointer_restored" {
			return errors.New("recovery directories have not been published")
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
			return errors.New("recovery pointer has outstanding permit")
		}
		if _, err := verifyRecoveryBackup(ctx, outer); err != nil {
			return err
		}
		backupSHA, err := releaseFileSHA256(filepath.Join(outer.BackupRoot, backupManifestFile))
		if err != nil {
			return err
		}
		if backupSHA != outer.BackupManifestSHA256 {
			return errors.New("recovery backup changed")
		}
		if err := checkRecoveryDirectoryState(ctx, root, previous, 5); err != nil {
			return err
		}
		old, err := os.ReadFile(filepath.Join(outer.BackupRoot, "install", "current.json"))
		if err != nil {
			return err
		}
		if currentPointerSHA256(old) != outer.OldCurrentSHA256 {
			return errors.New("backup old pointer identity mismatch")
		}
		candidate, err := json.Marshal(releaseCurrentPointer{Version: outer.CandidateVersion})
		if err != nil {
			return err
		}
		current := filepath.Join(root, "current.json")
		if _, err := ensureReleaseChild(root, current, false); err != nil {
			return err
		}
		actual, err := os.ReadFile(current)
		if err != nil {
			return err
		}
		if !bytes.Equal(actual, old) && !bytes.Equal(actual, candidate) {
			return errors.New("current pointer changed during recovery")
		}
		archive := filepath.Join(root, maintenanceDirName, "failed", operation, "current.json")
		archived, archiveErr := readSecureConfigFile(archive)
		if archiveErr == nil {
			if !bytes.Equal(archived, candidate) {
				return errors.New("failed pointer archive conflict")
			}
		} else if !errors.Is(archiveErr, os.ErrNotExist) {
			return archiveErr
		}
		if bytes.Equal(actual, old) {
			return nil
		}
		if previous.Phase == "pointer_restored" {
			return errors.New("restored pointer has changed")
		}
		if errors.Is(archiveErr, os.ErrNotExist) {
			if err := writeSecureConfigFile(archive, candidate, false, nil); err != nil {
				return err
			}
		}
		if hook != nil {
			if err := hook("after-archive"); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		writeErr := writeCurrentPointerAtomically(current, old)
		actual, err = os.ReadFile(current)
		if err != nil || !bytes.Equal(actual, old) {
			return errors.Join(writeErr, err, errors.New("old pointer restoration not confirmed"))
		}
		if hook != nil {
			return hook("after-pointer")
		}
		return nil
	})
	if err != nil || previous.Phase == "pointer_restored" {
		return err
	}
	next := previous
	next.Phase = "pointer_restored"
	return persistRecoveryJournal(root, &previous, next)
}
