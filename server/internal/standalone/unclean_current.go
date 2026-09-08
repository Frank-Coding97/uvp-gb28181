package standalone

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

func finishRecoveryPointer(ctx context.Context, root, operation, trust string, outer MaintenanceJournal) error {
	if outer.Schema == 1 {
		return restoreRecoveryCurrent(ctx, root, operation, trust, nil)
	}
	return verifyUncleanRecoveryCurrent(ctx, root, operation, trust)
}

// Same-version recovery never writes or archives a replacement current.json.
func verifyUncleanRecoveryCurrent(ctx context.Context, root, operation, trust string) error {
	var previous recoveryJournal
	err := withConfigLock(root, func() error {
		if err := requireMaintenanceInstanceLock(root); err != nil {
			return err
		}
		outer, err := ReadMaintenanceJournal(root)
		if err != nil {
			return err
		}
		if outer.Schema != 2 || outer.Kind != maintenanceKindUnclean || outer.OperationID != operation || outer.Phase != MaintenanceRestoring {
			return errors.New("invalid unclean recovery pointer verification")
		}
		previous, err = readRecoveryJournal(root)
		if err != nil {
			return err
		}
		if !recoveryContextMatches(previous, outer) || previous.OperationID != operation || (previous.Phase != "data_published" && previous.Phase != "pointer_unchanged_verified") {
			return errors.New("unclean recovery directories are not published")
		}
		if _, _, err := loadMaintenanceReleasesWithTrust(root, maintenanceSourceSelection(outer), trust); err != nil {
			return err
		}
		releases, identity, err := installedMaintenanceReleaseSnapshot(root)
		if err != nil {
			return err
		}
		if identity != outer.ReleaseSetSHA256 {
			return errors.New("unclean recovery release set changed")
		}
		if err := backupComponentsStopped(releases...); err != nil {
			return err
		}
		if _, err := os.Lstat(maintenancePermitPath(root)); !errors.Is(err, os.ErrNotExist) {
			return errors.New("unclean recovery has outstanding permit")
		}
		if _, err := verifyRecoveryBackup(ctx, outer); err != nil {
			return err
		}
		if err := checkRecoveryDirectoryState(ctx, root, previous, 5); err != nil {
			return err
		}
		currentSHA, err := releaseFileSHA256(filepath.Join(root, "current.json"))
		if err != nil {
			return err
		}
		if currentSHA != outer.OldCurrentSHA256 {
			return errors.New("unclean recovery current pointer changed")
		}
		return ctx.Err()
	})
	if err != nil || previous.Phase == "pointer_unchanged_verified" {
		return err
	}
	next := previous
	next.Phase = "pointer_unchanged_verified"
	return persistRecoveryJournal(root, &previous, next)
}
