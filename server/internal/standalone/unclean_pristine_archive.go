package standalone

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type pristineRecoveryReceipt struct {
	Schema         int       `json:"schema"`
	Kind           string    `json:"kind"`
	OperationID    string    `json:"operation_id"`
	ContextSHA256  string    `json:"context_sha256"`
	SnapshotSHA256 string    `json:"snapshot_sha256"`
	ConfirmedAt    time.Time `json:"confirmed_at"`
}

func archivePristineUncleanRecovery(ctx context.Context, root, operation, trust string) error {
	return withConfigLock(root, func() error {
		j, _, err := verifyRecoveryConfirmation(ctx, root, operation, trust)
		if err != nil {
			return err
		}
		outer, err := ReadMaintenanceJournal(root)
		if err != nil {
			return err
		}
		if outer.Schema != 2 || outer.Kind != maintenanceKindUnclean {
			return errors.New("pristine confirmation is only valid for unclean recovery")
		}
		for _, db := range []string{filepath.Join(root, "data", "uvp.db"), filepath.Join(root, maintenanceDirName, "failed", operation, "data", "uvp.db"), filepath.Join(outer.BackupRoot, "data", "uvp.db")} {
			pristine, err := recoveryPristinePendingAdmin(ctx, db)
			if err != nil {
				return err
			}
			if !pristine {
				return errors.New("recovery requires local administrator confirmation")
			}
		}
		source := filepath.Join(root, maintenanceDirName)
		target := filepath.Join(root, ".uvp-recovered-"+operation)
		if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
			return errors.New("pristine recovery archive already exists or is inaccessible")
		}
		if _, err := os.Lstat(filepath.Join(source, "confirmation.json")); !errors.Is(err, os.ErrNotExist) {
			return errors.New("pristine recovery has conflicting administrator receipt")
		}
		receipt := pristineRecoveryReceipt{Schema: 2, Kind: "pristine_pending_admin", OperationID: operation, ContextSHA256: j.ContextSHA256, SnapshotSHA256: j.BackupManifestSHA256, ConfirmedAt: time.Now().UTC()}
		path := filepath.Join(source, "pristine-confirmation.json")
		raw, err := readSecureConfigFile(path)
		if err == nil {
			var previous pristineRecoveryReceipt
			if len(raw) > 2048 || decodeBackupObject(raw, &previous, "schema", "kind", "operation_id", "context_sha256", "snapshot_sha256", "confirmed_at") != nil || previous.ConfirmedAt.IsZero() {
				return errors.New("invalid pristine recovery receipt")
			}
			receipt.ConfirmedAt = previous.ConfirmedAt
			if receipt != previous {
				return errors.New("conflicting pristine recovery receipt")
			}
		} else if errors.Is(err, os.ErrNotExist) {
			raw, err = json.Marshal(receipt)
			if err != nil {
				return err
			}
			if err := writeSecureConfigFile(path, raw, false, nil); err != nil {
				return err
			}
		} else {
			return err
		}
		return publishRecoveryArchive(ctx, source, target, backupPublish)
	})
}
