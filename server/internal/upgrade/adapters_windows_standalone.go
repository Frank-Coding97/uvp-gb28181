//go:build windows

package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// persistentEntries are the installation-root children the upgrade must never
// replace. They hold the customer's configuration, data, logs and the helper's
// own task state; every other top-level entry belongs to the release payload.
var persistentEntries = map[string]struct{}{
	"config":        {},
	"data":          {},
	"logs":          {},
	"run":           {},
	"resource":      {},
	"updater":       {},
	"updater-gate":  {},
	"updater-state": {},
}

// helperEntry is the one persistent directory that still has to move forward:
// the helper binary itself ships with every release. It cannot be replaced in
// place while the helper is running (Windows locks the image), so the new image
// is staged as a sidecar and the control script swaps it on the next start.
const (
	helperEntry        = "updater"
	helperImage        = "uvp-updater.exe"
	helperStagedSuffix = ".new"
)

// WindowsStandaloneAdapter upgrades a green-package Windows installation in
// place.
//
// The Unix adapter relies on a `current` symlink into a per-task release
// directory. That model cannot be reproduced here: creating directory symlinks
// on Windows needs SeCreateSymbolicLinkPrivilege, and the shipped control
// script resolves its binaries from the installation root, not from a link. So
// this adapter keeps the same durable workflow but swaps the payload inside the
// root:
//
//	Backup   → copy config/data (for the shared completeness check) plus a
//	           snapshot of every program entry
//	Migrate  → run the *candidate* uvp-server.exe -migrate-up against the live
//	           root, before anything is replaced
//	Activate → replace each program entry from the extracted package
//	Rollback → restore the snapshot, then the database
//
// The candidate is only ever executed during Migrate, so a broken artifact is
// rejected while the running installation is still intact.
type WindowsStandaloneAdapter struct {
	Layout         RegisteredLayout
	Runner         CommandRunner
	Probe          RuntimeIdentityProbe
	ExternalBackup *ExternalBackupRegistration
	BackupRunner   ExternalBackupRunner
}

func (a *WindowsStandaloneAdapter) runner() CommandRunner {
	if a.Runner != nil {
		return a.Runner
	}
	return OSCommandRunner{}
}

// runControl invokes the registered control script through cmd.exe. Go's
// CreateProcess cannot launch a .cmd image reliably, and quoting through
// cmd.exe is the only form that keeps a path with spaces intact.
func (a *WindowsStandaloneAdapter) runControl(ctx context.Context, args ...string) error {
	if err := a.Layout.validate(); err != nil {
		return err
	}
	if a.Layout.Control == "" {
		return errors.New("windows standalone control path is not registered")
	}
	if _, err := os.Stat(a.Layout.Control); err != nil {
		return fmt.Errorf("registered control script is unavailable: %w", err)
	}
	command := append([]string{"/c", a.Layout.Control}, args...)
	return a.runner().Run(ctx, "cmd.exe", command...)
}

func (a *WindowsStandaloneAdapter) Stop(ctx context.Context, _ TaskRecord) error {
	return a.runControl(ctx, "stop")
}

func (a *WindowsStandaloneAdapter) PreparePackage(ctx context.Context, record TaskRecord) error {
	if err := a.Layout.validate(); err != nil {
		return err
	}
	if record.StagingPath == "" || !filepath.IsAbs(record.StagingPath) {
		return errors.New("windows upgrade package path is missing or not absolute")
	}
	if _, err := os.Stat(record.StagingPath); err != nil {
		return fmt.Errorf("stat windows upgrade package: %w", err)
	}
	if err := validateSQLiteBackupSource(a.Layout, record); err != nil {
		return err
	}
	return validateExternalBackupPlan(a.Layout.State, record, a.ExternalBackup, a.BackupRunner)
}

// programEntries lists the release-owned top-level entries of an extracted
// package. Anything under persistentEntries is skipped; a payload that ships
// only persistent directories is rejected rather than silently "upgraded".
func programEntries(packageRoot string) ([]string, error) {
	items, err := os.ReadDir(packageRoot)
	if err != nil {
		return nil, err
	}
	var entries []string
	for _, item := range items {
		name := item.Name()
		if _, persistent := persistentEntries[name]; persistent {
			continue
		}
		entries = append(entries, name)
	}
	if len(entries) == 0 {
		return nil, errors.New("upgrade package contains no program files")
	}
	return entries, nil
}

func (a *WindowsStandaloneAdapter) backupRoot(record TaskRecord) (string, error) {
	if record.TaskID == "" {
		return "", errors.New("upgrade task ID is required")
	}
	root := filepath.Join(a.Layout.State, "backups", record.TaskID)
	if !isWithin(a.Layout.State, root) {
		return "", errors.New("backup path escapes updater state")
	}
	return root, nil
}

func (a *WindowsStandaloneAdapter) Backup(ctx context.Context, record TaskRecord) error {
	if err := a.Layout.validate(); err != nil {
		return err
	}
	backupRoot, err := a.backupRoot(record)
	if err != nil {
		return err
	}
	if err := prepareBackupRoot(backupRoot, record); err != nil {
		return err
	}
	if err := prepareExternalBackup(ctx, a.Layout.State, record, a.ExternalBackup, a.BackupRunner); err != nil {
		return err
	}
	sqliteDatabase, err := registeredSQLiteDatabase(a.Layout, record)
	if err != nil {
		return err
	}
	if complete, err := validateBackupComplete(backupRoot, sqliteDatabase); err != nil {
		return err
	} else if complete {
		return validatePreparedExternalBackup(a.Layout.State, record, a.ExternalBackup)
	}
	for _, name := range []string{"config", "data"} {
		source := filepath.Join(a.Layout.Root, name)
		if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if name == "data" {
			excluded, err := sqliteSidecarExclusions(source, sqliteDatabase)
			if err != nil {
				return err
			}
			if err := copyTreeExcept(source, filepath.Join(backupRoot, name), excluded); err != nil {
				return fmt.Errorf("backup %s: %w", name, err)
			}
			continue
		}
		if err := copyTree(source, filepath.Join(backupRoot, name)); err != nil {
			return fmt.Errorf("backup %s: %w", name, err)
		}
	}
	if sqliteDatabase != "" {
		if err := SQLiteBackup(ctx, sqliteDatabase, filepath.Join(backupRoot, "database.sqlite")); err != nil {
			return fmt.Errorf("backup sqlite database: %w", err)
		}
	}
	// Snapshot the program entries too: Rollback has no release directory to
	// switch back to, so these bytes are the only way to restore the previous
	// installation.
	snapshot := filepath.Join(backupRoot, "program")
	if err := os.MkdirAll(snapshot, 0o700); err != nil {
		return fmt.Errorf("create program snapshot: %w", err)
	}
	for _, name := range a.snapshotEntries() {
		source := filepath.Join(a.Layout.Root, name)
		if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := copyPath(source, filepath.Join(snapshot, name)); err != nil {
			return fmt.Errorf("snapshot %s: %w", name, err)
		}
	}
	return writeBackupComplete(backupRoot)
}

// snapshotEntries is every top-level entry that Activate may replace.
func (a *WindowsStandaloneAdapter) snapshotEntries() []string {
	var names []string
	items, err := os.ReadDir(a.Layout.Root)
	if err != nil {
		return nil
	}
	for _, item := range items {
		name := item.Name()
		if _, persistent := persistentEntries[name]; persistent {
			continue
		}
		names = append(names, name)
	}
	return names
}

// Migrate runs the candidate backend against the live installation. The
// candidate is extracted into updater state, its config is overlaid with the
// live one, and it is started with the installation root as the working
// directory: uvp-gb28181 resolves app.BasePath from the working directory, so
// the live config and database are used while the new binary is exercised.
func (a *WindowsStandaloneAdapter) Migrate(ctx context.Context, record TaskRecord) error {
	if err := a.Layout.validate(); err != nil {
		return err
	}
	if record.StagingPath == "" || !filepath.IsAbs(record.StagingPath) {
		return errors.New("windows migration staging path is missing or not absolute")
	}
	if record.TaskID == "" {
		return errors.New("windows migration task ID is required")
	}
	candidate, err := a.stageCandidate(record)
	if err != nil {
		return err
	}
	binary := filepath.Join(candidate, "bin", serverBinaryFile)
	if _, err := os.Stat(binary); err != nil {
		return fmt.Errorf("candidate backend binary is unavailable: %w", err)
	}
	command := []string{"/c", a.Layout.Control, "migrate", binary}
	runner, ok := a.runner().(CommandDirRunner)
	if !ok {
		return errors.New("windows migration runner does not support a working directory")
	}
	if err := runner.RunDir(ctx, a.Layout.Root, "cmd.exe", command...); err != nil {
		return fmt.Errorf("run candidate migration: %w", err)
	}
	return nil
}

// stageCandidate extracts the artifact into a per-task directory under updater
// state and returns the package root.
func (a *WindowsStandaloneAdapter) stageCandidate(record TaskRecord) (string, error) {
	root := filepath.Join(a.Layout.State, "candidate", record.TaskID)
	if !isWithin(a.Layout.State, root) {
		return "", errors.New("candidate staging path escapes updater state")
	}
	if err := os.RemoveAll(root); err != nil {
		return "", fmt.Errorf("clear candidate staging: %w", err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("create candidate staging: %w", err)
	}
	if err := extractPackage(record.StagingPath, root); err != nil {
		return "", fmt.Errorf("extract candidate package: %w", err)
	}
	packageRoot, err := extractedPackageRoot(root)
	if err != nil {
		return "", fmt.Errorf("resolve candidate package root: %w", err)
	}
	return packageRoot, nil
}

func (a *WindowsStandaloneAdapter) Activate(_ context.Context, record TaskRecord) error {
	if err := a.Layout.validate(); err != nil {
		return err
	}
	packageRoot, err := a.stageCandidate(record)
	if err != nil {
		return err
	}
	entries, err := programEntries(packageRoot)
	if err != nil {
		return err
	}
	for _, name := range entries {
		source := filepath.Join(packageRoot, name)
		target := filepath.Join(a.Layout.Root, name)
		if !isWithin(a.Layout.Root, target) {
			return fmt.Errorf("package entry %q escapes installation root", name)
		}
		if err := replacePath(source, target); err != nil {
			return fmt.Errorf("replace %s: %w", name, err)
		}
	}
	// The helper image cannot be swapped while it is running; hand it to the
	// control script as a sidecar.
	stagedHelper := filepath.Join(packageRoot, helperEntry, helperImage)
	if _, err := os.Stat(stagedHelper); err == nil {
		destination := filepath.Join(a.Layout.Root, helperEntry, helperImage+helperStagedSuffix)
		if err := copyFileFromPath(stagedHelper, destination); err != nil {
			return fmt.Errorf("stage updater helper image: %w", err)
		}
	}
	if err := syncTree(a.Layout.Root); err != nil {
		return fmt.Errorf("sync activated installation: %w", err)
	}
	return nil
}

func (a *WindowsStandaloneAdapter) Validate(ctx context.Context, record TaskRecord) error {
	if err := a.runControl(ctx, "start"); err != nil {
		return err
	}
	if err := a.runControl(ctx, "status"); err != nil {
		return err
	}
	if err := a.validateReleaseIdentity(record); err != nil {
		return err
	}
	return validateRuntimeIdentity(ctx, a.Layout, record, a.Probe)
}

// validateReleaseIdentity reads version.json from the installation root. The
// Unix adapter resolves it through the current link; here the root is the
// release.
func (a *WindowsStandaloneAdapter) validateReleaseIdentity(record TaskRecord) error {
	if record.Version == "" && record.BuildID == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(a.Layout.Root, "version.json"))
	if err != nil {
		return fmt.Errorf("read active release identity: %w", err)
	}
	var identity struct {
		Version string `json:"version"`
		BuildID string `json:"build_id"`
	}
	if err := json.Unmarshal(data, &identity); err != nil {
		return fmt.Errorf("decode active release identity: %w", err)
	}
	if record.Version != "" && identity.Version != record.Version {
		return fmt.Errorf("active release version %q does not match task %q", identity.Version, record.Version)
	}
	if record.BuildID != "" && identity.BuildID != record.BuildID {
		return fmt.Errorf("active release build ID %q does not match task %q", identity.BuildID, record.BuildID)
	}
	return nil
}

func (a *WindowsStandaloneAdapter) Commit(_ context.Context, _ TaskRecord) error { return nil }

// CurrentTarget reports the running release. A flat Windows installation is
// the root itself, so there is no separate target to switch back to; Rollback
// uses the program snapshot instead.
func (a *WindowsStandaloneAdapter) CurrentTarget() (string, error) {
	if err := a.Layout.validate(); err != nil {
		return "", err
	}
	return "", nil
}

func (a *WindowsStandaloneAdapter) Rollback(ctx context.Context, record TaskRecord) error {
	if err := a.runControl(ctx, "stop"); err != nil {
		return err
	}
	if err := restoreRollbackDatabase(ctx, a.Layout, record); err != nil {
		return err
	}
	if err := a.restoreProgramSnapshot(record); err != nil {
		return err
	}
	if err := a.runControl(ctx, "start"); err != nil {
		return err
	}
	return validateRollbackRuntime(ctx, a.Layout, record, a.Probe)
}

func (a *WindowsStandaloneAdapter) restoreProgramSnapshot(record TaskRecord) error {
	backupRoot, err := a.backupRoot(record)
	if err != nil {
		return err
	}
	snapshot := filepath.Join(backupRoot, "program")
	items, err := os.ReadDir(snapshot)
	if err != nil {
		return fmt.Errorf("read program snapshot: %w", err)
	}
	// Remove payload entries that did not exist before the upgrade, so an
	// additional directory from the new release cannot linger.
	for _, name := range a.snapshotEntries() {
		if _, err := os.Stat(filepath.Join(snapshot, name)); errors.Is(err, os.ErrNotExist) {
			if removeErr := os.RemoveAll(filepath.Join(a.Layout.Root, name)); removeErr != nil {
				return fmt.Errorf("remove new entry %s: %w", name, removeErr)
			}
		}
	}
	for _, item := range items {
		name := item.Name()
		target := filepath.Join(a.Layout.Root, name)
		if !isWithin(a.Layout.Root, target) {
			return fmt.Errorf("snapshot entry %q escapes installation root", name)
		}
		if err := replacePath(filepath.Join(snapshot, name), target); err != nil {
			return fmt.Errorf("restore %s: %w", name, err)
		}
	}
	return syncTree(a.Layout.Root)
}

// replacePath swaps target with source. Windows cannot rename over an existing
// directory or a locked file, so the old entry is removed first; the caller has
// already validated that the new entry extracted cleanly.
func replacePath(source, target string) error {
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := os.Rename(source, target); err != nil {
		return err
	}
	return nil
}

// copyPath copies a file or a whole directory tree.
func copyPath(source, destination string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyTree(source, destination)
	}
	return copyFileFromPath(source, destination)
}

func copyFileFromPath(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o700)
	if err != nil {
		return err
	}
	written, err := io.Copy(out, in)
	if err != nil {
		_ = out.Close()
		return err
	}
	if written != info.Size() {
		_ = out.Close()
		return io.ErrShortWrite
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
