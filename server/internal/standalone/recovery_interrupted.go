package standalone

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// The coordinator must itself own InstanceLock before entering this function.
// Retiring an unused capability and normalizing its operation happen under the
// same ConfigLock used by the maintenance child's permit consumer.
func admitInterruptedRecovery(ctx context.Context, root, operation, trust string, rename func(string, string) error) (result MaintenanceJournal, failure error) {
	failure = withConfigLock(root, func() error {
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
		if outer.OperationID != operation {
			return errors.New("interrupted recovery operation mismatch")
		}
		switch outer.Phase {
		case MaintenanceUpgrading, MaintenanceCommitting, MaintenanceRestoreRequired, MaintenanceRestoring:
		default:
			return errors.New("maintenance phase cannot enter interrupted recovery")
		}
		if _, _, err := loadMaintenanceReleasesWithTrust(root, outer.CandidateVersion, trust); err != nil {
			return err
		}
		releases, identity, err := installedMaintenanceReleaseSnapshot(root)
		if err != nil {
			return err
		}
		if identity != outer.ReleaseSetSHA256 {
			return errors.New("interrupted recovery release set changed")
		}
		if err := backupComponentsStopped(releases...); err != nil {
			return err
		}
		manifest, err := VerifyBackup(ctx, outer.BackupRoot)
		if err != nil {
			return err
		}
		backupSHA, err := releaseFileSHA256(filepath.Join(outer.BackupRoot, backupManifestFile))
		if err != nil {
			return err
		}
		if backupSHA != outer.BackupManifestSHA256 || manifest.Version != outer.OldVersion {
			return errors.New("interrupted recovery backup changed")
		}
		old, err := os.ReadFile(filepath.Join(outer.BackupRoot, "install", "current.json"))
		if err != nil {
			return err
		}
		if currentPointerSHA256(old) != outer.OldCurrentSHA256 {
			return errors.New("interrupted recovery old pointer changed")
		}
		currentPath := filepath.Join(root, "current.json")
		if _, err := ensureReleaseChild(root, currentPath, false); err != nil {
			return err
		}
		current, err := os.ReadFile(currentPath)
		if err != nil {
			return err
		}
		candidate, err := json.Marshal(releaseCurrentPointer{Version: outer.CandidateVersion})
		if err != nil {
			return err
		}
		if !bytes.Equal(current, old) && (outer.Phase == MaintenanceUpgrading || !bytes.Equal(current, candidate)) {
			return errors.New("interrupted recovery current pointer is inconsistent")
		}
		progress, progressErr := readRecoveryJournal(root)
		if progressErr != nil && !errors.Is(progressErr, os.ErrNotExist) {
			return progressErr
		}
		if progressErr == nil {
			if outer.Phase == MaintenanceUpgrading || outer.Phase == MaintenanceCommitting || (outer.Phase == MaintenanceRestoreRequired && progress.Phase != "staging") {
				return errors.New("interrupted recovery progress is inconsistent")
			}
			if progress.OperationID != operation || progress.BackupManifestSHA256 != backupSHA || progress.OldVersion != outer.OldVersion || progress.OldCurrentSHA256 != outer.OldCurrentSHA256 || progress.ReleaseSetSHA256 != identity {
				return errors.New("interrupted recovery progress identity mismatch")
			}
		} else if outer.Phase == MaintenanceRestoring {
			return errors.New("interrupted restoration has no progress journal")
		}
		if err := retireInterruptedRecoveryPermit(ctx, root, outer, progress, rename); err != nil {
			return err
		}
		if outer.Phase == MaintenanceUpgrading || outer.Phase == MaintenanceCommitting {
			if err := ctx.Err(); err != nil {
				return err
			}
			outer.Phase = MaintenanceRestoreRequired
			if err := validateMaintenanceJournal(outer); err != nil {
				return err
			}
			raw, err := json.Marshal(outer)
			if err != nil {
				return err
			}
			// ConfigLock still holds the identity/CAS checked above. Calling
			// advanceMaintenance here would recursively acquire that lock.
			if err := writeSecureConfigFile(filepath.Join(root, maintenanceDirName, "journal.json"), raw, true, nil); err != nil {
				return err
			}
		}
		result = outer
		return nil
	})
	return result, failure
}

func retireInterruptedRecoveryPermit(ctx context.Context, root string, outer MaintenanceJournal, progress recoveryJournal, rename func(string, string) error) error {
	envelope, err := readMaintenancePermitEnvelope(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if rename == nil || envelope.OperationID != outer.OperationID || envelope.ExpiresAt.After(time.Now().UTC().Add(maintenancePermitTTL)) {
		return errInvalidMaintenancePermit
	}
	selection := outer
	if outer.Phase == MaintenanceRestoreRequired {
		if progress.Phase != "" {
			return errInvalidMaintenancePermit
		}
		selection.Phase = MaintenanceUpgrading // permit left by failed candidate cleanup
	}
	if outer.Phase == MaintenanceRestoring && progress.Phase != "staging" {
		return errInvalidMaintenancePermit
	}
	if err := validateMaintenancePermitSelection(selection, envelope.Purpose, envelope.Version); err != nil {
		return err
	}
	release, err := LoadReleaseVersion(root, envelope.Version)
	if err != nil {
		return err
	}
	backendSHA, err := releaseFileSHA256(release.BackendExe)
	if err != nil {
		return err
	}
	if envelope.BackendSHA256 != backendSHA {
		return errInvalidMaintenancePermit
	}
	source := maintenancePermitPath(root)
	raw, err := readSecureConfigFile(source)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(raw)
	directory := filepath.Join(root, maintenanceDirName, "retired-permits")
	target := filepath.Join(directory, hex.EncodeToString(digest[:])+".json")
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		return errors.New("retired permit archive exists or is inaccessible")
	}
	if err := mkdirRecoveryTree(filepath.Join(root, maintenanceDirName), directory); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	renameErr := rename(source, target)
	_, sourceErr := os.Lstat(source)
	archived, archiveErr := readSecureConfigFile(target)
	if errors.Is(sourceErr, os.ErrNotExist) && archiveErr == nil && bytes.Equal(archived, raw) {
		return nil
	}
	return errors.Join(renameErr, archiveErr, errors.New("interrupted permit retirement was not confirmed"))
}
