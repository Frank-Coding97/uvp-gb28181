package upgrade

import (
	"context"
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
	root, err := filepath.EvalSymlinks(a.Layout.Root)
	if err != nil {
		return err
	}
	releases, err := filepath.EvalSymlinks(a.Layout.Releases)
	if err != nil {
		return err
	}
	release, err = filepath.EvalSymlinks(release)
	if err != nil {
		return err
	}
	if !isWithin(root, releases) || !isWithin(releases, release) {
		return errors.New("active control script is outside registered releases")
	}
	source := filepath.Join(release, standaloneControlName)
	info, err := os.Lstat(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil // Older/custom packages may not ship a controller.
	}
	if err != nil {
		return fmt.Errorf("stat packaged standalone control: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Size() == 0 || info.Size() > 1<<20 {
		return errors.New("packaged standalone control is not a bounded nonempty executable file")
	}
	original, err := os.Lstat(a.Layout.Control)
	if err != nil {
		return fmt.Errorf("stat installed standalone control: %w", err)
	}
	if !original.Mode().IsRegular() {
		return errors.New("installed standalone control is not a regular file")
	}
	if same, err := filesHaveSameContent(source, a.Layout.Control); err != nil {
		return err
	} else if same {
		return nil
	}
	backup, err := a.controlBackupPath(record)
	if err != nil {
		return err
	}
	// A resumed activation must retain the original entrypoint, not back up
	// the candidate that an earlier attempt already installed.
	if info, err := os.Lstat(backup); errors.Is(err, os.ErrNotExist) {
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

// A legacy helper finishes the first upgrade before the new helper can run
// installControl. Repair that handoff after recovery, only for a succeeded
// active release with a normal gate. Never disturb an unfinished upgrade.
func (w *WorkflowExecutor) refreshStandaloneControl(ctx context.Context) error {
	a, ok := w.Adapter.(*StandaloneAdapter)
	if !ok || w.Gate == nil || a.Layout.Control != filepath.Join(a.Layout.Root, standaloneControlName) {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	lock, err := acquireUpgradeLock(filepath.Join(w.Store.root, "workflow.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	gate, err := w.Gate.Load()
	if err != nil || gate.Mode != GateNormal {
		return err
	}
	target, err := a.CurrentTarget()
	if err != nil || target == "" {
		return err
	}
	records, err := w.Store.List()
	if err != nil {
		return err
	}
	for _, record := range records {
		if isResumableApplyPhase(record.Phase) || record.Phase == PhaseRecoveryRequired {
			return nil
		}
	}
	for _, record := range records {
		if record.Phase != PhaseSucceeded || target != filepath.Join(a.Layout.Releases, record.TaskID) {
			continue
		}
		same, err := filesHaveSameContent(filepath.Join(target, standaloneControlName), a.Layout.Control)
		if errors.Is(err, os.ErrNotExist) || same {
			return nil
		} else if err != nil {
			return err
		}
		previous := record.PreviousTarget
		if previous == "" {
			previous = a.Layout.Root
		}
		if err := preserveZLMConfig(a.Layout.Root, previous); err != nil {
			return err
		}
		if err := a.installControl(target, record); err != nil {
			return err
		}
		// The old entrypoint has already started ZLM with the new template.
		// Reload the saved configuration immediately during this one-time handoff.
		return a.runner().Run(ctx, a.Layout.Control, "restart-zlm")
	}
	return nil
}
