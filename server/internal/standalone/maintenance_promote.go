package standalone

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
)

// Promotion is the only preparing -> upgrading transition. A directory or a
// manifest alone never authorizes migration; the complete snapshot is verified.
func promotePreparedMaintenance(ctx context.Context, installDir, operationID, backupRoot string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := cleanAbsolute(backupRoot)
	if err != nil {
		return err
	}
	return withConfigLock(installDir, func() error {
		if err := requireMaintenanceInstanceLock(installDir); err != nil {
			return err
		}
		journal, err := ReadMaintenanceJournal(installDir)
		if err != nil {
			return err
		}
		if journal.Schema != 1 || journal.OperationID != operationID || journal.BackupRoot != root || (journal.Phase != MaintenancePreparing && journal.Phase != MaintenanceUpgrading) {
			return errors.New("maintenance preparation identity changed")
		}
		currentSHA, err := releaseFileSHA256(filepath.Join(installDir, "current.json"))
		if err != nil || currentSHA != journal.OldCurrentSHA256 {
			return errors.New("maintenance current pointer changed")
		}
		current, err := LoadRelease(installDir)
		if err != nil {
			return err
		}
		backup, err := VerifyBackup(ctx, root)
		if err != nil {
			return err
		}
		if backup.Version != journal.OldVersion || current.Version != journal.OldVersion || backup.SourceCommit != current.SourceCommit {
			return errors.New("maintenance backup release identity mismatch")
		}
		copiedCurrentSHA, err := releaseFileSHA256(filepath.Join(root, "install", "current.json"))
		if err != nil || copiedCurrentSHA != journal.OldCurrentSHA256 {
			return errors.New("maintenance backup pointer mismatch")
		}
		markerRaw, err := readSecureConfigFile(filepath.Join(root, "complete.json"))
		if err != nil {
			return err
		}
		var marker backupCompleteMarker
		if err := decodeBackupObject(markerRaw, &marker, "manifest_path", "manifest_sha256"); err != nil {
			return err
		}
		if marker.ManifestPath != backupManifestFile {
			return errors.New("maintenance backup manifest reference changed")
		}
		digest, err := releaseFileSHA256(filepath.Join(root, backupManifestFile))
		if err != nil || digest != marker.ManifestSHA256 {
			return errors.New("maintenance backup changed during verification")
		}
		if journal.Phase == MaintenanceUpgrading {
			if journal.BackupManifestSHA256 != digest {
				return errors.New("maintenance backup digest changed")
			}
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		journal.BackupManifestSHA256, journal.Phase = digest, MaintenanceUpgrading
		raw, err := json.Marshal(journal)
		if err != nil {
			return err
		}
		return writeSecureConfigFile(filepath.Join(installDir, maintenanceDirName, "journal.json"), raw, true, nil)
	})
}
