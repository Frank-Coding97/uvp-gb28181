package standalone

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

func (p *upgradePreparation) finish(ctx context.Context) error {
	return p.finishWithRename(ctx, backupPublish)
}

func (p *upgradePreparation) finishWithRename(ctx context.Context, rename func(string, string) error) error {
	if rename == nil {
		return errors.New("upgrade completion requires atomic publication")
	}
	return p.withCompletionReady(ctx, func(journal MaintenanceJournal) error {
		source := filepath.Join(p.paths.InstallDir, maintenanceDirName)
		target := filepath.Join(p.paths.InstallDir, ".uvp-completed-"+journal.OperationID)
		entries, err := os.ReadDir(source)
		if err != nil || len(entries) != 1 || entries[0].Name() != "journal.json" {
			return errors.New("upgrade completion has unexpected maintenance files")
		}
		if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
			return errors.New("upgrade completion archive already exists or is inaccessible")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// Same-root, no-replace, write-through rename is the gate-release point.
		// Both locks remain held through the postcondition checks below.
		renameErr := rename(source, target)
		_, sourceErr := os.Lstat(source)
		if !errors.Is(sourceErr, os.ErrNotExist) {
			return errors.Join(renameErr, errors.New("upgrade completion gate remains present or inaccessible"))
		}
		archived, archiveErr := readMaintenanceJournalFile(filepath.Join(target, "journal.json"))
		if archiveErr == nil && archived == journal {
			return nil // Verified success also resolves an uncertain API error.
		}
		// Never infer success from a missing source alone. Re-establish a gate
		// before returning an invalid/missing archive outcome to the caller.
		if err := os.Mkdir(source, 0700); err != nil {
			if errors.Is(err, os.ErrExist) {
				return errors.New("upgrade completion archive invalid; gate retained")
			}
			return errors.Join(renameErr, archiveErr, err)
		}
		if err := protectConfigDir(source, true); err != nil {
			return err // Even an empty gate blocks ordinary startup.
		}
		raw, err := json.Marshal(journal)
		if err != nil {
			return err
		}
		writeErr := writeSecureConfigFile(filepath.Join(source, "journal.json"), raw, false, nil)
		return errors.Join(renameErr, archiveErr, writeErr, errors.New("upgrade completion archive invalid; maintenance gate restored"))
	})
}
