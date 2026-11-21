package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// VerifyPendingBackupSnapshots finishes the integrity scan that the upgrade
// stop window deliberately skips.
//
// Producing a SQLite snapshot and reading it back for verification cost about
// the same on a multi-gigabyte database, so doing both after the service has
// already stopped doubled the time real installations were unavailable (a
// 2.9 GB database spent 110 s producing the snapshot and 99 s verifying it,
// out of a 243 s stop window). The scan protects a rollback source that only
// matters later, so it runs once the new release is serving.
//
// The helper restarts after every successful upgrade, which is exactly when
// this becomes due. A snapshot marked failed keeps its bytes for diagnosis,
// but a restore refuses it: SQLiteRestore verifies its source before it
// replaces the live database.
func VerifyPendingBackupSnapshots(ctx context.Context, store *TaskStore, layout RegisteredLayout, events UpgradeEventSink) error {
	if store == nil {
		return errors.New("verification store is required")
	}
	if err := layout.validate(); err != nil {
		return err
	}
	if filepath.Clean(store.root) != filepath.Clean(layout.State) {
		return errors.New("verification store does not match registered state")
	}
	root := filepath.Join(layout.State, "backups")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list retained backups: %w", err)
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() || !validTaskID(entry.Name()) {
			continue
		}
		taskID := entry.Name()
		dir := filepath.Join(root, taskID)
		completed, present, err := readCompletedBackup(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !present || completed.Integrity != backupIntegrityPending {
			continue
		}
		state := backupIntegrityOK
		reason := ""
		if err := checkSQLiteIntegrity(ctx, filepath.Join(dir, "database.sqlite")); err != nil {
			state, reason = backupIntegrityFailed, err.Error()
		}
		// Cleanup prunes superseded backups concurrently with this scan. The
		// directory may be gone by now; there is nothing left to record.
		if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
			continue
		}
		completed.Integrity = state
		data, err := json.Marshal(completed)
		if err != nil {
			return err
		}
		if err := writeFileAtomically(filepath.Join(dir, backupCompleteMarker), data, 0600); err != nil {
			return err
		}
		if state == backupIntegrityFailed && events != nil {
			if err := events.Append(ctx, UpgradeEvent{
				Event:  "backup_verification_failed",
				TaskID: taskID,
				Reason: reason,
				At:     time.Now().UTC(),
			}); err != nil {
				return err
			}
		}
	}
	return nil
}
