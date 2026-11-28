package upgrade

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const standaloneControlName = "uvp-gb28181-ctl.sh"

// The installed entrypoint derives ROOT from its own directory. Keep it in
// the installation root, but update its implementation with the release so
// historical installations also load config/zlm.ini instead of the template.
func (a *StandaloneAdapter) installControl(release string, record TaskRecord) error {
	if a.Layout.Control != filepath.Join(a.Layout.Root, standaloneControlName) {
		return nil // Administrator-provided controllers are not packaged files.
	}
	source := filepath.Join(release, standaloneControlName)
	info, err := os.Lstat(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil // Older/custom packages may not ship a controller.
	}
	if err != nil {
		return fmt.Errorf("stat packaged standalone control: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Size() == 0 {
		return errors.New("packaged standalone control is not a nonempty executable file")
	}
	backup, err := a.controlBackupPath(record)
	if err != nil {
		return err
	}
	// A resumed activation must retain the original entrypoint, not back up
	// the candidate that an earlier attempt already installed.
	if info, err := os.Lstat(backup); errors.Is(err, os.ErrNotExist) {
		original, err := os.Lstat(a.Layout.Control)
		if err != nil {
			return fmt.Errorf("stat installed standalone control: %w", err)
		}
		if !original.Mode().IsRegular() {
			return errors.New("installed standalone control is not a regular file")
		}
		if err := os.MkdirAll(filepath.Dir(backup), 0700); err != nil {
			return fmt.Errorf("create standalone control backup directory: %w", err)
		}
		if err := copyFileAtomically(a.Layout.Control, backup, 0700); err != nil {
			return fmt.Errorf("backup standalone control: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("stat standalone control backup: %w", err)
	} else if !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.New("standalone control backup is not a nonempty regular file")
	}
	if err := copyFileAtomically(source, a.Layout.Control, 0755); err != nil {
		return fmt.Errorf("install standalone control: %w", err)
	}
	return nil
}

func (a *StandaloneAdapter) controlBackupPath(record TaskRecord) (string, error) {
	if !validTaskID(record.TaskID) {
		return "", errors.New("standalone control backup task ID is invalid")
	}
	return filepath.Join(a.Layout.State, "backups", record.TaskID, standaloneControlName), nil
}

func (a *StandaloneAdapter) restoreControl(record TaskRecord) error {
	if a.Layout.Control != filepath.Join(a.Layout.Root, standaloneControlName) {
		return nil
	}
	backup, err := a.controlBackupPath(record)
	if err != nil {
		return err
	}
	info, err := os.Lstat(backup)
	if errors.Is(err, os.ErrNotExist) {
		return nil // Activation never replaced the entrypoint, or a legacy task.
	}
	if err != nil {
		return fmt.Errorf("stat standalone control backup: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.New("standalone control backup is not a nonempty regular file")
	}
	if err := copyFileAtomically(backup, a.Layout.Control, 0755); err != nil {
		return fmt.Errorf("restore standalone control: %w", err)
	}
	return nil
}
