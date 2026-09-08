package standalone

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// The first persistent state is published as a complete directory. Until its
// rename succeeds no database/config copy or mutation may start. Afterward the
// caller must check all installation component processes before taking backup.
func createPreparingMaintenanceJournal(installDir string, journal MaintenanceJournal) error {
	return createPreparingMaintenanceJournalWithHook(installDir, journal, nil)
}

func createPreparingMaintenanceJournalWithHook(installDir string, journal MaintenanceJournal, hook func(string) error) error {
	if journal.Schema != 1 {
		return errors.New("schema1 preparation requires schema1 maintenance journal")
	}
	if journal.Phase != MaintenancePreparing {
		return errors.New("expected preparing maintenance phase")
	}
	if err := validateMaintenanceJournal(journal); err != nil {
		return err
	}
	root, err := cleanAbsolute(installDir)
	if err != nil {
		return err
	}
	return withConfigLock(root, func() error {
		if err := requireMaintenanceInstanceLock(root); err != nil {
			return err
		}
		if err := CheckMaintenanceGate(root); err != nil {
			return err
		}
		stage, err := os.MkdirTemp(root, ".uvp-prepare-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage)
		if err := protectConfigDir(stage, true); err != nil {
			return err
		}
		raw, err := json.Marshal(journal)
		if err != nil {
			return err
		}
		if err := writeSecureConfigFile(filepath.Join(stage, "journal.json"), raw, false, nil); err != nil {
			return err
		}
		if hook != nil {
			if err := hook("before_publish"); err != nil {
				return err
			}
		}
		if err := backupPublish(stage, filepath.Join(root, maintenanceDirName)); err != nil {
			return err
		}
		if hook != nil {
			return hook("after_publish")
		}
		return nil
	})
}
