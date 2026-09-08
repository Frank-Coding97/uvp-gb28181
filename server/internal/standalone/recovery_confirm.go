package standalone

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Caller owns InstanceLock. Call again under ConfigLock after interactive
// confirmation, because neither a prompt nor a prior check authorizes changes.
func verifyRecoveryConfirmation(ctx context.Context, root, operation, trust string) (recoveryJournal, BackupManifest, error) {
	var empty recoveryJournal
	var manifest BackupManifest
	if err := requireMaintenanceInstanceLock(root); err != nil {
		return empty, manifest, err
	}
	outer, err := ReadMaintenanceJournal(root)
	if err != nil {
		return empty, manifest, err
	}
	if outer.OperationID != operation || outer.Phase != MaintenanceAwaitingConfirmation {
		return empty, manifest, errors.New("recovery is not awaiting this local confirmation")
	}
	j, err := readRecoveryJournal(root)
	if err != nil {
		return empty, manifest, err
	}
	if j.Phase != "pointer_restored" || j.OperationID != operation || j.BackupManifestSHA256 != outer.BackupManifestSHA256 || j.OldVersion != outer.OldVersion || j.OldCurrentSHA256 != outer.OldCurrentSHA256 || j.ReleaseSetSHA256 != outer.ReleaseSetSHA256 {
		return empty, manifest, errors.New("recovery confirmation identity mismatch")
	}
	if _, _, err := loadMaintenanceReleasesWithTrust(root, outer.CandidateVersion, trust); err != nil {
		return empty, manifest, err
	}
	releases, identity, err := installedMaintenanceReleaseSnapshot(root)
	if err != nil {
		return empty, manifest, err
	}
	if identity != outer.ReleaseSetSHA256 {
		return empty, manifest, errors.New("recovery release set changed")
	}
	if err := backupComponentsStopped(releases...); err != nil {
		return empty, manifest, err
	}
	if _, err := os.Lstat(maintenancePermitPath(root)); !errors.Is(err, os.ErrNotExist) {
		return empty, manifest, errors.New("recovery confirmation has outstanding permit")
	}
	manifest, err = VerifyBackup(ctx, outer.BackupRoot)
	if err != nil {
		return empty, manifest, err
	}
	backupSHA, err := releaseFileSHA256(filepath.Join(outer.BackupRoot, backupManifestFile))
	if err != nil {
		return empty, manifest, err
	}
	if backupSHA != outer.BackupManifestSHA256 {
		return empty, manifest, errors.New("recovery backup changed")
	}
	if err := checkRecoveryDirectoryState(ctx, root, j, 5); err != nil {
		return empty, manifest, err
	}
	current := filepath.Join(root, "current.json")
	if _, err := ensureReleaseChild(root, current, false); err != nil {
		return empty, manifest, err
	}
	raw, err := os.ReadFile(current)
	if err != nil {
		return empty, manifest, err
	}
	if currentPointerSHA256(raw) != outer.OldCurrentSHA256 {
		return empty, manifest, errors.New("recovery old pointer changed")
	}
	return j, manifest, ctx.Err()
}

type recoveryConfirmationReceipt struct {
	Schema               int       `json:"schema"`
	OperationID          string    `json:"operation_id"`
	BackupManifestSHA256 string    `json:"backup_manifest_sha256"`
	AdministratorID      int64     `json:"administrator_id"`
	ImpactSHA256         string    `json:"impact_sha256"`
	ConfirmedAt          time.Time `json:"confirmed_at"`
}

// Private final step, only called after administrator authentication and an
// explicit operation-bound acknowledgement. It never starts business services.
func archiveRecoveryConfirmation(ctx context.Context, root, operation, trust string, adminID int64, impactSHA string, recheck func() error, rename func(string, string) error) error {
	if adminID <= 0 || !validMaintenancePermitHex(impactSHA) || recheck == nil || rename == nil {
		return errors.New("invalid local recovery confirmation")
	}
	return withConfigLock(root, func() error {
		j, _, err := verifyRecoveryConfirmation(ctx, root, operation, trust)
		if err != nil {
			return err
		}
		if err := recheck(); err != nil {
			return err
		}
		source := filepath.Join(root, maintenanceDirName)
		target := filepath.Join(root, ".uvp-recovered-"+operation)
		if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
			return errors.New("recovery archive already exists or is inaccessible")
		}
		receiptPath := filepath.Join(source, "confirmation.json")
		receipt := recoveryConfirmationReceipt{1, operation, j.BackupManifestSHA256, adminID, impactSHA, time.Now().UTC()}
		raw, err := readSecureConfigFile(receiptPath)
		if err == nil {
			var previous recoveryConfirmationReceipt
			if len(raw) > 2048 || json.Unmarshal(raw, &previous) != nil || previous.Schema != 1 || previous.OperationID != operation || previous.BackupManifestSHA256 != j.BackupManifestSHA256 || previous.AdministratorID != adminID || previous.ImpactSHA256 != impactSHA || previous.ConfirmedAt.IsZero() {
				return errors.New("existing recovery confirmation conflicts")
			}
		} else if errors.Is(err, os.ErrNotExist) {
			raw, err = json.Marshal(receipt)
			if err != nil {
				return err
			}
			if err := writeSecureConfigFile(receiptPath, raw, false, nil); err != nil {
				return err
			}
		} else {
			return err
		}
		// Cover the entire retained gate including failed data, work, journal,
		// and receipt; successful rename alone is insufficient evidence.
		identity, err := recoveryTreeIdentity(ctx, source)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		renameErr := rename(source, target)
		_, sourceErr := os.Lstat(source)
		if !errors.Is(sourceErr, os.ErrNotExist) {
			return errors.Join(renameErr, errors.New("recovery gate remains present or inaccessible"))
		}
		// Do not let cancellation after the rename skip the safety check.
		actual, archiveErr := recoveryTreeIdentity(context.Background(), target)
		if archiveErr == nil && actual == identity {
			return nil
		}
		// Preserve the archive even if invalid, and restore a fail-closed gate.
		if err := os.Mkdir(source, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return errors.Join(renameErr, archiveErr, err)
		}
		if err := protectConfigDir(source, true); err != nil {
			return err
		}
		return errors.Join(renameErr, archiveErr, errors.New("recovery archive invalid; startup gate restored"))
	})
}
