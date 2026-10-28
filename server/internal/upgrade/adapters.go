package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CommandRunner is intentionally narrow so adapters can be tested without
// touching a host service manager or Docker daemon.
type CommandRunner interface {
	Run(context.Context, string, ...string) error
}

// CommandDirRunner runs an administrator-registered command with an explicit
// working directory. Standalone migrations need this boundary because the
// packaged control script resolves its active binary and relative config/data
// paths from its own directory.
type CommandDirRunner interface {
	CommandRunner
	RunDir(context.Context, string, string, ...string) error
}

type OSCommandRunner struct{}

func (OSCommandRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	return runCommandWithDiagnostic(cmd)
}

func (OSCommandRunner) RunDir(ctx context.Context, dir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	return runCommandWithDiagnostic(cmd)
}

type CommandOutputRunner interface {
	CommandRunner
	Output(context.Context, string, ...string) ([]byte, error)
}

// PackagePreparer is implemented by deployment adapters that must do package
// work while the current service is still running. The workflow invokes it
// before the maintenance gate and stop boundary when the adapter supports it.
type PackagePreparer interface {
	PreparePackage(context.Context, TaskRecord) error
}

func (OSCommandRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Output()
}

type RegisteredLayout struct {
	Root             string `json:"root"`
	Current          string `json:"current,omitempty"`
	Releases         string `json:"releases,omitempty"`
	State            string `json:"state,omitempty"`
	Control          string `json:"control,omitempty"`
	Service          string `json:"service,omitempty"`
	ProjectDir       string `json:"project_dir,omitempty"`
	Project          string `json:"project,omitempty"`
	MigrationService string `json:"migration_service,omitempty"`
	ReadinessURL     string `json:"readiness_url,omitempty"`
	SQLiteDatabase   string `json:"sqlite_database,omitempty"`
}

func (l *RegisteredLayout) validate() error {
	if l == nil || filepath.IsAbs(l.Root) == false || strings.TrimSpace(l.Root) == "" {
		return errors.New("registered deployment root must be absolute")
	}
	if l.Current == "" {
		l.Current = filepath.Join(l.Root, "current")
	}
	if l.Releases == "" {
		l.Releases = filepath.Join(l.Root, "releases")
	}
	if l.State == "" {
		l.State = filepath.Join(l.Root, "updater-state")
	}
	for name, path := range map[string]string{"current": l.Current, "releases": l.Releases} {
		if !filepath.IsAbs(path) || !isWithin(l.Root, path) {
			return fmt.Errorf("registered %s path must be inside deployment root", name)
		}
	}
	// State is administrator-owned updater data. Systemd deployments commonly
	// keep it under /var/lib rather than beneath an immutable /opt release root.
	if !filepath.IsAbs(l.State) {
		return errors.New("registered state path must be absolute")
	}
	if l.Control != "" && !filepath.IsAbs(l.Control) {
		return errors.New("registered control path must be absolute")
	}
	if l.ProjectDir != "" && !filepath.IsAbs(l.ProjectDir) {
		return errors.New("registered compose project directory must be absolute")
	}
	if l.MigrationService != "" && !validComposeServiceName(l.MigrationService) {
		return errors.New("registered migration service name is invalid")
	}
	if l.Service != "" && !validComposeServiceName(l.Service) {
		return errors.New("registered compose service name is invalid")
	}
	if l.ReadinessURL != "" {
		if _, err := parseRuntimeIdentityURL(l.ReadinessURL); err != nil {
			return err
		}
	}
	if l.SQLiteDatabase != "" && !filepath.IsAbs(l.SQLiteDatabase) {
		return errors.New("registered sqlite database path must be absolute")
	}
	return nil
}

// Validate exposes registration layout checks to the helper assembly without
// letting callers construct deployment commands from request data.
func (l *RegisteredLayout) Validate() error {
	return l.validate()
}

type StandaloneAdapter struct {
	Layout         RegisteredLayout
	Runner         CommandRunner
	Probe          RuntimeIdentityProbe
	ExternalBackup *ExternalBackupRegistration
	BackupRunner   ExternalBackupRunner
}

func (a *StandaloneAdapter) runner() CommandRunner {
	if a.Runner != nil {
		return a.Runner
	}
	return OSCommandRunner{}
}

func (a *StandaloneAdapter) Stop(ctx context.Context, _ TaskRecord) error {
	if err := a.Layout.validate(); err != nil {
		return err
	}
	if a.Layout.Control == "" {
		return errors.New("standalone control path is not registered")
	}
	return a.runner().Run(ctx, a.Layout.Control, "stop")
}

// PreparePackage validates database backup prerequisites before the
// workflow closes ingress or stops the service. The actual export is deferred
// to Backup, after Stop has completed, so the snapshot cannot race with
// writes from the old service.
func (a *StandaloneAdapter) PreparePackage(_ context.Context, record TaskRecord) error {
	if err := a.Layout.validate(); err != nil {
		return err
	}
	if err := validateSQLiteBackupSource(a.Layout, record); err != nil {
		return err
	}
	return validateExternalBackupPlan(a.Layout.State, record, a.ExternalBackup, a.BackupRunner)
}

func (a *StandaloneAdapter) Backup(ctx context.Context, record TaskRecord) error {
	if err := a.Layout.validate(); err != nil {
		return err
	}
	sqliteDatabase, err := registeredSQLiteDatabase(a.Layout, record)
	if err != nil {
		return err
	}
	backupRoot := filepath.Join(a.Layout.State, "backups", record.TaskID)
	if err := prepareBackupRoot(backupRoot, record); err != nil {
		return err
	}
	if err := prepareExternalBackup(ctx, a.Layout.State, record, a.ExternalBackup, a.BackupRunner); err != nil {
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
	return writeBackupComplete(backupRoot)
}

func validateSQLiteBackupSource(layout RegisteredLayout, record TaskRecord) error {
	database, err := registeredSQLiteDatabase(layout, record)
	if err != nil || database == "" {
		return err
	}
	info, err := os.Stat(database)
	if err != nil {
		return fmt.Errorf("stat registered sqlite database before stop: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("registered sqlite database is not a regular file")
	}
	file, err := os.Open(database)
	if err != nil {
		return fmt.Errorf("open registered sqlite database before stop: %w", err)
	}
	return file.Close()
}

func registeredSQLiteDatabase(layout RegisteredLayout, record TaskRecord) (string, error) {
	if record.Preflight == nil || NormalizeDatabaseDialect(record.Preflight.DatabaseDialect) != "sqlite" {
		return "", nil
	}
	if layout.SQLiteDatabase == "" {
		return "", errors.New("registered sqlite database path is required for sqlite backup")
	}
	return layout.SQLiteDatabase, nil
}

type preparedExternalBackup struct {
	Version  int    `json:"version"`
	TaskID   string `json:"task_id"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	MaxBytes uint64 `json:"max_bytes"`
}

func prepareExternalBackup(ctx context.Context, state string, record TaskRecord, registration *ExternalBackupRegistration, runner ExternalBackupRunner) error {
	if registration == nil {
		return nil
	}
	if err := validateExternalBackupRegistration(registration); err != nil {
		return err
	}
	root, output, marker, err := preparedExternalBackupPaths(state, record.TaskID)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(marker); err == nil {
		return validatePreparedExternalBackup(state, record, registration)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat prepared external backup marker: %w", err)
	}
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("clear incomplete external backup: %w", err)
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return fmt.Errorf("create external backup staging: %w", err)
	}
	spec, err := registration.spec(output)
	if err != nil {
		return err
	}
	if err := ExternalBackup(ctx, spec, runner); err != nil {
		return err
	}
	info, err := os.Lstat(output)
	if err != nil {
		return fmt.Errorf("stat prepared external backup: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || uint64(info.Size()) > registration.MaxBackupBytes {
		return errors.New("prepared external backup is invalid")
	}
	digest, err := sha256File(output)
	if err != nil {
		return fmt.Errorf("hash prepared external backup: %w", err)
	}
	markerData, err := json.Marshal(preparedExternalBackup{Version: 1, TaskID: record.TaskID, Size: info.Size(), SHA256: digest, MaxBytes: registration.MaxBackupBytes})
	if err != nil {
		return err
	}
	return writeFileAtomically(marker, markerData, 0600)
}

// validateExternalBackupPlan checks everything that can be checked without
// taking a database snapshot. It intentionally does not contact the database:
// the service is still running at this point and may continue writing.
func validateExternalBackupPlan(state string, record TaskRecord, registration *ExternalBackupRegistration, runner ExternalBackupRunner) error {
	if registration == nil {
		return nil
	}
	if err := validateExternalBackupRegistration(registration); err != nil {
		return err
	}
	_, output, _, err := preparedExternalBackupPaths(state, record.TaskID)
	if err != nil {
		return err
	}
	spec, err := registration.spec(output)
	if err != nil {
		return err
	}
	var command string
	switch strings.ToLower(spec.Dialect) {
	case "mysql":
		command = "mysqldump"
	case "postgres", "postgresql":
		command = "pg_dump"
	case "sqlserver":
		return errors.New("automatic SQL Server backup is unsupported because its target file cannot be size-enforced")
	default:
		return fmt.Errorf("unsupported external database dialect %q", spec.Dialect)
	}
	// Tests and embedded deployments can provide a runner. The production
	// helper uses the OS runner and must fail before stopping if the dump tool
	// is absent.
	if runner == nil {
		if _, err := exec.LookPath(command); err != nil {
			return fmt.Errorf("external database backup tool %q is unavailable: %w", command, err)
		}
	}
	return nil
}

func validatePreparedExternalBackup(state string, record TaskRecord, registration *ExternalBackupRegistration) error {
	if registration == nil {
		return nil
	}
	if err := validateExternalBackupRegistration(registration); err != nil {
		return err
	}
	_, output, marker, err := preparedExternalBackupPaths(state, record.TaskID)
	if err != nil {
		return err
	}
	info, err := os.Lstat(marker)
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("external database backup is missing")
	}
	if err != nil {
		return fmt.Errorf("stat prepared external backup marker: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("prepared external backup marker is not a regular file")
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		return fmt.Errorf("read prepared external backup marker: %w", err)
	}
	var prepared preparedExternalBackup
	if err := decodeStrictJSON(data, &prepared); err != nil {
		return fmt.Errorf("decode prepared external backup marker: %w", err)
	}
	if prepared.Version != 1 || prepared.TaskID != record.TaskID || prepared.Size <= 0 || prepared.MaxBytes != registration.MaxBackupBytes || !validDigest(prepared.SHA256) {
		return errors.New("prepared external backup marker does not match registration")
	}
	outputInfo, err := os.Lstat(output)
	if err != nil {
		return fmt.Errorf("prepared external backup is missing: %w", err)
	}
	if !outputInfo.Mode().IsRegular() || outputInfo.Size() != prepared.Size || uint64(outputInfo.Size()) > registration.MaxBackupBytes {
		return errors.New("prepared external backup file is invalid")
	}
	digest, err := sha256File(output)
	if err != nil {
		return fmt.Errorf("hash prepared external backup: %w", err)
	}
	if digest != prepared.SHA256 {
		return errors.New("prepared external backup hash does not match completion marker")
	}
	return nil
}

func preparedExternalBackupPaths(state, taskID string) (string, string, string, error) {
	if !filepath.IsAbs(state) || !validTaskID(taskID) {
		return "", "", "", errors.New("external backup state path or task ID is invalid")
	}
	base := filepath.Join(state, "prepared-backups")
	root := filepath.Join(base, taskID)
	output := filepath.Join(root, "database.backup")
	marker := filepath.Join(root, ".complete.json")
	if !isWithin(state, base) || !isWithin(base, root) || !isWithin(root, output) || !isWithin(root, marker) {
		return "", "", "", errors.New("external backup path escapes updater state")
	}
	return root, output, marker, nil
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (a *StandaloneAdapter) Migrate(ctx context.Context, record TaskRecord) error {
	if err := a.Layout.validate(); err != nil {
		return err
	}
	if a.Layout.Control == "" {
		return errors.New("standalone control path is not registered")
	}
	if record.StagingPath == "" || !filepath.IsAbs(record.StagingPath) {
		return errors.New("standalone migration staging path is missing or not absolute")
	}
	if record.TaskID == "" {
		return errors.New("standalone migration task ID is required")
	}

	// Migrate runs before Activate. The registered control script resolves
	// SERVER_BIN from current, so invoking it from the installation root here
	// would silently run the old binary. Build a disposable package root and
	// invoke the control script extracted from that package instead. Linking
	// the persistent directories keeps the target binary on the same user data
	// and configuration while leaving current untouched until Activate.
	migrationRoot := filepath.Join(a.Layout.State, "migration-staging", record.TaskID)
	if !isWithin(a.Layout.State, migrationRoot) {
		return errors.New("standalone migration staging path escapes updater state")
	}
	if err := os.RemoveAll(migrationRoot); err != nil {
		return fmt.Errorf("clear standalone migration staging: %w", err)
	}
	if err := os.MkdirAll(migrationRoot, 0700); err != nil {
		return fmt.Errorf("create standalone migration staging: %w", err)
	}
	defer os.RemoveAll(migrationRoot)
	if err := extractPackage(record.StagingPath, migrationRoot); err != nil {
		return fmt.Errorf("extract standalone migration package: %w", err)
	}
	packageRoot, err := extractedPackageRoot(migrationRoot)
	if err != nil {
		return fmt.Errorf("resolve standalone migration package root: %w", err)
	}
	for _, name := range []string{"config", "data", "logs", "run"} {
		source := filepath.Join(a.Layout.Root, name)
		if err := os.MkdirAll(source, 0755); err != nil {
			return fmt.Errorf("create standalone persistent directory %q: %w", name, err)
		}
		target := filepath.Join(packageRoot, name)
		if !isWithin(packageRoot, target) {
			return fmt.Errorf("standalone migration directory %q escapes package root", name)
		}
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("replace standalone migration directory %q: %w", name, err)
		}
		if err := os.Symlink(source, target); err != nil {
			return fmt.Errorf("link standalone migration directory %q: %w", name, err)
		}
	}
	control := filepath.Join(packageRoot, filepath.Base(a.Layout.Control))
	info, err := os.Stat(control)
	if err != nil {
		return fmt.Errorf("target package control script is unavailable: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return errors.New("target package control script is not executable")
	}
	runner, ok := a.runner().(CommandDirRunner)
	if !ok {
		return errors.New("standalone migration runner does not support a working directory")
	}
	// The target control script owns application startup/configuration details
	// and invokes the packaged binary with its fixed -migrate-up mode. The
	// command is selected from the package, never from the wire request.
	return runner.RunDir(ctx, packageRoot, control, "migrate")
}

// CurrentTarget is read before stopping the service and is persisted in the
// task record by the workflow. A missing current link is allowed for first
// installation, but a malformed non-link is rejected.
func (a *StandaloneAdapter) CurrentTarget() (string, error) {
	if err := a.Layout.validate(); err != nil {
		return "", err
	}
	target, err := os.Readlink(a.Layout.Current)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read current release: %w", err)
	}
	if target == "" {
		return "", errors.New("current release points outside registered releases")
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(a.Layout.Current), target)
	}
	if !isWithin(a.Layout.Releases, target) {
		return "", errors.New("current release points outside registered releases")
	}
	return filepath.Clean(target), nil
}

func (a *StandaloneAdapter) Activate(_ context.Context, record TaskRecord) error {
	return a.activate(record, nil)
}

// activate installs the target release and flips current only after an
// optional deployment-specific preparation has completed inside the new
// release directory.
func (a *StandaloneAdapter) activate(record TaskRecord, prepare func(string) error) error {
	if err := a.Layout.validate(); err != nil {
		return err
	}
	if record.StagingPath == "" {
		return errors.New("staged artifact path is missing")
	}
	if err := os.MkdirAll(a.Layout.Releases, 0755); err != nil {
		return err
	}
	release := filepath.Join(a.Layout.Releases, record.TaskID)
	tmp := release + ".tmp"
	_ = os.RemoveAll(tmp)
	unpack := filepath.Join(tmp, "unpack")
	if err := os.MkdirAll(unpack, 0755); err != nil {
		return err
	}
	if err := extractPackage(record.StagingPath, unpack); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	packageRoot, err := extractedPackageRoot(unpack)
	if err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := moveTreeContents(packageRoot, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	_ = os.RemoveAll(unpack)
	if prepare != nil {
		if err := prepare(tmp); err != nil {
			_ = os.RemoveAll(tmp)
			return err
		}
	}
	if err := syncTree(tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("sync candidate release: %w", err)
	}
	_ = os.RemoveAll(release)
	if err := os.Rename(tmp, release); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := syncDirectory(a.Layout.Releases); err != nil {
		return fmt.Errorf("sync release directory: %w", err)
	}
	next := a.Layout.Current + ".next"
	_ = os.Remove(next)
	if err := os.Symlink(release, next); err != nil {
		return err
	}
	if info, err := os.Lstat(a.Layout.Current); err == nil && info.Mode()&os.ModeSymlink == 0 {
		_ = os.Remove(next)
		return errors.New("current deployment entry is not a symlink")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(next)
		return err
	}
	if err := os.Rename(next, a.Layout.Current); err != nil {
		_ = os.Remove(next)
		return err
	}
	if err := syncDirectory(filepath.Dir(a.Layout.Current)); err != nil {
		return fmt.Errorf("sync current release directory: %w", err)
	}
	return nil
}

func (a *StandaloneAdapter) Validate(ctx context.Context, record TaskRecord) error {
	if err := a.Layout.validate(); err != nil {
		return err
	}
	if a.Layout.Control == "" {
		return errors.New("standalone control path is not registered")
	}
	if err := a.runner().Run(ctx, a.Layout.Control, "start"); err != nil {
		return err
	}
	if err := a.runner().Run(ctx, a.Layout.Control, "status"); err != nil {
		return err
	}
	if err := validateReleaseIdentity(a.Layout, record); err != nil {
		return err
	}
	return validateRuntimeIdentity(ctx, a.Layout, record, a.Probe)
}

func validateReleaseIdentity(layout RegisteredLayout, record TaskRecord) error {
	return validateReleaseIdentityAt(layout, record, "version.json")
}

func validateReleaseIdentityAt(layout RegisteredLayout, record TaskRecord, relativePath string) error {
	if record.Version == "" && record.BuildID == "" {
		return nil
	}
	target, err := (&StandaloneAdapter{Layout: layout}).CurrentTarget()
	if err != nil {
		return err
	}
	if target == "" {
		return errors.New("active release is missing")
	}
	identityPath := filepath.Join(target, relativePath)
	if !isWithin(target, identityPath) {
		return errors.New("active release identity path escapes release")
	}
	data, err := os.ReadFile(identityPath)
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

func validateRuntimeIdentity(ctx context.Context, layout RegisteredLayout, record TaskRecord, probe RuntimeIdentityProbe) error {
	// Old/hand-constructed records without a task ID do not represent a
	// candidate process. Every workflow task has one, and therefore reaches
	// the live process check below.
	if record.TaskID == "" {
		return nil
	}
	expected, err := expectedRuntimeIdentity(record)
	if err != nil {
		return err
	}
	probe.URL = layout.ReadinessURL
	return probe.Validate(ctx, expected)
}

func expectedRuntimeIdentity(record TaskRecord) (RuntimeIdentity, error) {
	if record.Version == "" || record.BuildID == "" || !validTaskID(record.TaskID) {
		return RuntimeIdentity{}, errors.New("candidate task identity is incomplete")
	}
	expected := RuntimeIdentity{
		Version:       record.Version,
		BuildID:       record.BuildID,
		TaskID:        record.TaskID,
		DatabaseReady: true,
	}
	if record.Compatibility != nil {
		expected.MigrationDigest = record.Compatibility.Database.TargetMigrationDigest
		expected.DatabaseDialect = record.Compatibility.Database.Dialect
	}
	return expected, nil
}

func (a *StandaloneAdapter) Commit(_ context.Context, _ TaskRecord) error { return nil }

func (a *StandaloneAdapter) switchCurrent(record TaskRecord) error {
	if err := a.Layout.validate(); err != nil {
		return err
	}
	previous := record.PreviousTarget
	if previous == "" && !record.PreviousTargetCaptured {
		return errors.New("previous release is not recorded")
	}
	// A legacy flat installation has no current link: the running code is the
	// installation root itself. Restoring it means removing the candidate link
	// and exposing the root again; linking current back to Root would make the
	// active path contain itself and would fail the registered release checks.
	if previous == "" {
		info, err := os.Lstat(a.Layout.Current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("stat current release: %w", err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return errors.New("current deployment entry is not a symlink")
		}
		if err := os.Remove(a.Layout.Current); err != nil {
			return fmt.Errorf("remove candidate current release: %w", err)
		}
		return syncDirectory(filepath.Dir(a.Layout.Current))
	}
	next := a.Layout.Current + ".next"
	_ = os.Remove(next)
	if err := os.Symlink(previous, next); err != nil {
		return err
	}
	if err := os.Rename(next, a.Layout.Current); err != nil {
		_ = os.Remove(next)
		return err
	}
	if err := syncDirectory(filepath.Dir(a.Layout.Current)); err != nil {
		return fmt.Errorf("sync current release directory: %w", err)
	}
	return nil
}

func (a *StandaloneAdapter) Rollback(ctx context.Context, record TaskRecord) error {
	if err := a.Layout.validate(); err != nil {
		return err
	}
	if a.Layout.Control == "" {
		return errors.New("standalone control path is not registered")
	}
	if err := a.runner().Run(ctx, a.Layout.Control, "stop"); err != nil {
		return err
	}
	if err := restoreRollbackDatabase(ctx, a.Layout, record); err != nil {
		return err
	}
	if err := a.switchCurrent(record); err != nil {
		return err
	}
	if err := a.runner().Run(ctx, a.Layout.Control, "start"); err != nil {
		return err
	}
	return validateRollbackRuntime(ctx, a.Layout, record, a.Probe)
}

type SystemdAdapter struct {
	StandaloneAdapter
	Unit           string
	BusinessRunner CommandRunner
}

func (a *SystemdAdapter) systemdRunner() CommandRunner {
	// Tests and embedded callers may provide a command recorder. Registered
	// production adapters leave Runner unset, so service-manager operations
	// remain privileged while only application commands use BusinessRunner.
	if a != nil && a.Runner != nil {
		return a.Runner
	}
	return OSCommandRunner{}
}

const systemdBackendRelativePath = "backend"
const systemdBackendBinary = "uvp-gb28181"

func (a *SystemdAdapter) Migrate(ctx context.Context, record TaskRecord) error {
	if err := a.StandaloneAdapter.Layout.validate(); err != nil {
		return err
	}
	if err := validateSystemdUnit(a.Unit); err != nil {
		return err
	}
	if record.StagingPath == "" || !filepath.IsAbs(record.StagingPath) {
		return errors.New("systemd migration staging path is missing or not absolute")
	}
	if record.TaskID == "" {
		return errors.New("systemd migration task ID is required")
	}
	migrationRoot := filepath.Join(a.StandaloneAdapter.Layout.State, "migration-staging", record.TaskID)
	if !isWithin(a.StandaloneAdapter.Layout.State, migrationRoot) {
		return errors.New("systemd migration staging path escapes updater state")
	}
	if err := os.RemoveAll(migrationRoot); err != nil {
		return fmt.Errorf("clear systemd migration staging: %w", err)
	}
	if err := os.MkdirAll(migrationRoot, 0700); err != nil {
		return fmt.Errorf("create systemd migration staging: %w", err)
	}
	defer os.RemoveAll(migrationRoot)
	if err := extractPackage(record.StagingPath, migrationRoot); err != nil {
		return fmt.Errorf("extract systemd migration package: %w", err)
	}
	packageRoot, err := extractedPackageRoot(migrationRoot)
	if err != nil {
		return fmt.Errorf("resolve systemd migration package root: %w", err)
	}
	if err := a.prepareRelease(packageRoot); err != nil {
		return fmt.Errorf("prepare systemd migration package: %w", err)
	}
	backendRoot := filepath.Join(packageRoot, systemdBackendRelativePath)
	binary := filepath.Join(backendRoot, systemdBackendBinary)
	if !isWithin(packageRoot, binary) {
		return errors.New("systemd migration binary escapes package root")
	}
	info, err := os.Stat(binary)
	if err != nil {
		return fmt.Errorf("target systemd migration binary is unavailable: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return errors.New("target systemd migration binary is not executable")
	}
	runnerSource := a.BusinessRunner
	if runnerSource == nil {
		runnerSource = a.runner()
	}
	runner, ok := runnerSource.(CommandDirRunner)
	if !ok {
		return errors.New("systemd migration runner does not support a working directory")
	}
	return runner.RunDir(ctx, backendRoot, binary, "-migrate-up")
}

func (a *SystemdAdapter) Activate(_ context.Context, record TaskRecord) error {
	if err := validateSystemdUnit(a.Unit); err != nil {
		return err
	}
	return a.StandaloneAdapter.activate(record, a.prepareRelease)
}

// prepareRelease restores the fixed persistent directories that the bare
// systemd deployment keeps outside immutable release directories. It runs
// before current is switched, so a candidate never observes a partial layout.
func (a *SystemdAdapter) prepareRelease(release string) error {
	if err := a.StandaloneAdapter.Layout.validate(); err != nil {
		return err
	}
	backendRoot := filepath.Join(release, systemdBackendRelativePath)
	if !isWithin(release, backendRoot) {
		return errors.New("systemd backend path escapes release")
	}
	info, err := os.Stat(backendRoot)
	if err != nil {
		return fmt.Errorf("systemd package backend is unavailable: %w", err)
	}
	if !info.IsDir() {
		return errors.New("systemd package backend is not a directory")
	}
	persistentPaths := []struct {
		source      string
		destination string
		required    bool
	}{
		{source: filepath.Join(a.StandaloneAdapter.Layout.Root, "config"), destination: filepath.Join(backendRoot, "config"), required: true},
		{source: filepath.Join(a.StandaloneAdapter.Layout.Root, "data", "logs"), destination: filepath.Join(backendRoot, "resource", "logs")},
		{source: filepath.Join(a.StandaloneAdapter.Layout.Root, "data", "uploads"), destination: filepath.Join(backendRoot, "resource", "public", "uploads")},
	}
	for _, path := range persistentPaths {
		if !isWithin(a.StandaloneAdapter.Layout.Root, path.source) || !isWithin(backendRoot, path.destination) {
			return errors.New("systemd persistent path escapes registered layout")
		}
		if path.required {
			info, err := os.Stat(path.source)
			if err != nil {
				return fmt.Errorf("systemd persistent config is unavailable: %w", err)
			}
			if !info.IsDir() {
				return errors.New("systemd persistent config is not a directory")
			}
		} else if err := os.MkdirAll(path.source, 0755); err != nil {
			return fmt.Errorf("create systemd persistent directory: %w", err)
		}
		if err := os.RemoveAll(path.destination); err != nil {
			return fmt.Errorf("replace systemd persistent link: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(path.destination), 0755); err != nil {
			return fmt.Errorf("create systemd package parent: %w", err)
		}
		if err := os.Symlink(path.source, path.destination); err != nil {
			return fmt.Errorf("link systemd persistent directory: %w", err)
		}
	}
	return makeTreeWorldReadable(filepath.Join(release, "frontend"))
}

func makeTreeWorldReadable(root string) error {
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		mode := info.Mode().Perm() | 0004
		if info.IsDir() || info.Mode().Perm()&0111 != 0 {
			mode |= 0001
		}
		return os.Chmod(path, mode)
	})
}

func (a *SystemdAdapter) Stop(ctx context.Context, _ TaskRecord) error {
	if err := a.StandaloneAdapter.Layout.validate(); err != nil {
		return err
	}
	if err := validateSystemdUnit(a.Unit); err != nil {
		return err
	}
	return a.systemdRunner().Run(ctx, "systemctl", "stop", a.Unit)
}

func (a *SystemdAdapter) Validate(ctx context.Context, record TaskRecord) error {
	if err := a.StandaloneAdapter.Layout.validate(); err != nil {
		return err
	}
	if err := validateSystemdUnit(a.Unit); err != nil {
		return err
	}
	if err := a.systemdRunner().Run(ctx, "systemctl", "start", a.Unit); err != nil {
		return err
	}
	if err := a.systemdRunner().Run(ctx, "systemctl", "is-active", "--quiet", a.Unit); err != nil {
		return err
	}
	if err := validateReleaseIdentityAt(a.StandaloneAdapter.Layout, record, filepath.Join(systemdBackendRelativePath, "version.json")); err != nil {
		return err
	}
	return validateRuntimeIdentity(ctx, a.StandaloneAdapter.Layout, record, a.StandaloneAdapter.Probe)
}

func (a *SystemdAdapter) Rollback(ctx context.Context, record TaskRecord) error {
	if err := a.StandaloneAdapter.Layout.validate(); err != nil {
		return err
	}
	if err := validateSystemdUnit(a.Unit); err != nil {
		return err
	}
	if err := a.systemdRunner().Run(ctx, "systemctl", "stop", a.Unit); err != nil {
		return err
	}
	if err := restoreRollbackDatabase(ctx, a.StandaloneAdapter.Layout, record); err != nil {
		return err
	}
	if err := a.StandaloneAdapter.switchCurrent(record); err != nil {
		return err
	}
	if err := a.systemdRunner().Run(ctx, "systemctl", "start", a.Unit); err != nil {
		return err
	}
	if err := a.systemdRunner().Run(ctx, "systemctl", "is-active", "--quiet", a.Unit); err != nil {
		return err
	}
	return validateRollbackRuntime(ctx, a.StandaloneAdapter.Layout, record, a.StandaloneAdapter.Probe)
}

type ComposeAdapter struct {
	Layout         RegisteredLayout
	Runner         CommandRunner
	Probe          RuntimeIdentityProbe
	File           string
	ExternalBackup *ExternalBackupRegistration
	BackupRunner   ExternalBackupRunner
}

type preparedComposePackage struct {
	TaskID      string `json:"task_id"`
	StagingPath string `json:"staging_path"`
}

// Registration is an administrator-owned deployment binding. It is loaded by
// the updater process and is never accepted from the HTTP protocol.
type Registration struct {
	TrustedKeys   map[string]string `json:"trusted_keys,omitempty"`
	ReleaseSource string            `json:"release_source,omitempty"`
	StagingRoot   string            `json:"staging_root,omitempty"`
	AllowedUIDs   []uint32          `json:"allowed_uids,omitempty"`
	// BusinessUID and BusinessGID bind the handoff directory and socket
	// group to the registered service identity. They are written by the
	// administrator-owned install/registration command; the HTTP process
	// cannot change them through the updater protocol.
	BusinessUID    uint32                      `json:"business_uid,omitempty"`
	BusinessGID    uint32                      `json:"business_gid,omitempty"`
	Deployment     string                      `json:"deployment"`
	Standalone     RegisteredLayout            `json:"standalone,omitempty"`
	SystemdUnit    string                      `json:"systemd_unit,omitempty"`
	Compose        RegisteredLayout            `json:"compose,omitempty"`
	ComposeFile    string                      `json:"compose_file,omitempty"`
	DatabaseBackup *ExternalBackupRegistration `json:"database_backup,omitempty"`
}

const maxRegistrationBytes = 1 << 20

// ReadRegistration reads one administrator-owned registration snapshot. The
// platform reader verifies every path component before opening the file, so a
// business-process writable directory cannot redirect a privileged helper.
func ReadRegistration(path string) (Registration, error) {
	if path == "" || !filepath.IsAbs(path) {
		return Registration{}, errors.New("registration path must be absolute")
	}
	path = filepath.Clean(path)
	file, err := openTrustedRegistration(path)
	if err != nil {
		return Registration{}, fmt.Errorf("open trusted updater registration: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Registration{}, fmt.Errorf("stat opened updater registration: %w", err)
	}
	if info.Size() > maxRegistrationBytes {
		return Registration{}, errors.New("updater registration exceeds size limit")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxRegistrationBytes+1))
	if err != nil {
		return Registration{}, fmt.Errorf("read updater registration: %w", err)
	}
	if len(data) > maxRegistrationBytes {
		return Registration{}, errors.New("updater registration exceeds size limit")
	}
	var registration Registration
	if err := decodeStrictJSON(data, &registration); err != nil {
		return Registration{}, fmt.Errorf("decode updater registration: %w", err)
	}
	if registration.Deployment == "" {
		return Registration{}, errors.New("updater registration deployment is required")
	}
	return registration, nil
}

// NewRegisteredAdapter constructs an adapter from a registration that was
// already read by ReadRegistration. It deliberately performs no filesystem
// reads, so callers can bind all helper subsystems to one trusted snapshot.
func NewRegisteredAdapter(registration Registration) (DeploymentAdapter, error) {
	if registration.Deployment == "" {
		return nil, errors.New("updater registration deployment is required")
	}
	switch registration.Deployment {
	case "standalone":
		return newStandaloneAdapter(registration), nil
	case "systemd":
		return &SystemdAdapter{StandaloneAdapter: StandaloneAdapter{Layout: registration.Standalone, ExternalBackup: registration.DatabaseBackup}, Unit: registration.SystemdUnit, BusinessRunner: registeredBusinessRunner(registration)}, nil
	case "compose":
		return &ComposeAdapter{Layout: registration.Compose, File: registration.ComposeFile, ExternalBackup: registration.DatabaseBackup}, nil
	default:
		return nil, fmt.Errorf("unsupported registered deployment %q", registration.Deployment)
	}
}

func registeredBusinessRunner(registration Registration) CommandRunner {
	return NewBusinessCommandRunner(registration.BusinessUID, registration.BusinessGID)
}

// Layout returns the only deployment layout selected by the registration.
func (r Registration) Layout() (RegisteredLayout, error) {
	switch r.Deployment {
	case "standalone", "systemd":
		return r.Standalone, nil
	case "compose":
		return r.Compose, nil
	default:
		return RegisteredLayout{}, fmt.Errorf("unsupported registered deployment %q", r.Deployment)
	}
}

// StagingInbox resolves the only inbox a helper may accept from its socket
// clients. A missing field retains the historical state/staging default.
func (r Registration) StagingInbox(state string) (string, error) {
	inbox := r.StagingRoot
	if inbox == "" {
		inbox = filepath.Join(state, "staging")
	}
	if !filepath.IsAbs(inbox) {
		return "", errors.New("registered staging root must be absolute")
	}
	return filepath.Clean(inbox), nil
}

func LoadRegistration(path string) (DeploymentAdapter, error) {
	registration, err := ReadRegistration(path)
	if err != nil {
		return nil, err
	}
	return NewRegisteredAdapter(registration)
}

func (a *ComposeAdapter) runner() CommandRunner {
	if a.Runner != nil {
		return a.Runner
	}
	return OSCommandRunner{}
}

func (a *ComposeAdapter) Stop(ctx context.Context, _ TaskRecord) error {
	if err := a.validate(); err != nil {
		return err
	}
	service, err := a.candidateService()
	if err != nil {
		return err
	}
	return a.compose(ctx, "stop", service)
}

func (a *ComposeAdapter) Backup(ctx context.Context, record TaskRecord) error {
	if err := a.validate(); err != nil {
		return err
	}
	if err := prepareExternalBackup(ctx, a.Layout.State, record, a.ExternalBackup, a.BackupRunner); err != nil {
		return err
	}
	sqliteDatabase, err := registeredSQLiteDatabase(a.Layout, record)
	if err != nil {
		return err
	}
	backupRoot := filepath.Join(a.Layout.State, "backups", record.TaskID)
	if err := prepareBackupRoot(backupRoot, record); err != nil {
		return err
	}
	if complete, err := a.validateBackupComplete(backupRoot, sqliteDatabase); err != nil {
		return err
	} else if complete {
		return nil
	}
	relCompose, err := a.composeRelativePath()
	if err != nil {
		return err
	}
	backupCompose := filepath.Join(backupRoot, "compose", relCompose)
	if !isWithin(filepath.Join(backupRoot, "compose"), backupCompose) {
		return errors.New("compose backup path escapes updater state")
	}
	if err := copyFile(a.File, backupCompose, 0600); err != nil {
		return err
	}
	if sqliteDatabase != "" {
		if err := SQLiteBackup(ctx, sqliteDatabase, filepath.Join(backupRoot, "database.sqlite")); err != nil {
			return fmt.Errorf("backup sqlite database: %w", err)
		}
	}
	return writeBackupComplete(backupRoot)
}

// validateBackupComplete confirms that a durable Compose backup includes the
// sole updater-owned project file. The generic completion marker alone cannot
// prove that a prior interrupted copy left a rollback source behind.
func (a *ComposeAdapter) validateBackupComplete(backupRoot, sqliteDatabase string) (bool, error) {
	complete, err := validateBackupComplete(backupRoot, sqliteDatabase)
	if err != nil || !complete {
		return complete, err
	}
	relCompose, err := a.composeRelativePath()
	if err != nil {
		return false, err
	}
	backupCompose := filepath.Join(backupRoot, "compose", relCompose)
	if !isWithin(filepath.Join(backupRoot, "compose"), backupCompose) {
		return false, errors.New("compose backup path escapes updater state")
	}
	info, err := os.Lstat(backupCompose)
	if err != nil {
		return false, fmt.Errorf("complete compose backup is missing registered compose file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("complete compose backup registered file is not regular")
	}
	return true, nil
}

func (a *ComposeAdapter) Migrate(ctx context.Context, record TaskRecord) error {
	if err := a.validate(); err != nil {
		return err
	}
	_, targetCompose, err := a.preparedComposeStage(ctx, record)
	if err != nil {
		return err
	}
	service := a.Layout.MigrationService
	if service == "" {
		service = "backend"
	}
	// Run the packaged application's fixed migration mode in an isolated
	// one-off target-image container before its Compose file is activated.
	// Compose owns database credentials and networks; the updater supplies no
	// SQL or arbitrary command text.
	return a.composeWithFile(ctx, targetCompose, "run", "--rm", "--no-deps", "--pull", "never", service, "-migrate-up")
}

func (a *ComposeAdapter) Activate(ctx context.Context, record TaskRecord) error {
	if err := a.validate(); err != nil {
		return err
	}
	_, targetCompose, err := a.preparedComposeStage(ctx, record)
	if err != nil {
		return err
	}
	// The updater owns the registered Compose file. Package sources, customer
	// overrides, and any bind-mounted data remain in the project directory.
	if err := replaceFileAtomically(targetCompose, a.File, 0600); err != nil {
		return fmt.Errorf("activate registered compose file: %w", err)
	}
	return nil
}

func (a *ComposeAdapter) Validate(ctx context.Context, record TaskRecord) error {
	if err := a.validate(); err != nil {
		return err
	}
	if err := a.compose(ctx, "up", "-d", "--pull", "never", "--no-build"); err != nil {
		return err
	}
	if record.TaskID != "" {
		return a.validateRuntimeIdentity(ctx, record)
	}
	return nil
}

func (a *ComposeAdapter) Commit(_ context.Context, _ TaskRecord) error { return nil }

func (a *ComposeAdapter) Rollback(ctx context.Context, record TaskRecord) error {
	if err := a.validate(); err != nil {
		return err
	}
	backupRoot := filepath.Join(a.Layout.State, "backups", record.TaskID)
	var sqliteDatabase string
	if !failedBeforeMigration(record) {
		var err error
		sqliteDatabase, err = registeredSQLiteDatabase(a.Layout, record)
		if err != nil {
			return err
		}
	}
	complete, backupErr := a.validateBackupComplete(backupRoot, sqliteDatabase)
	if backupErr != nil && !failedBeforeMigration(record) {
		return backupErr
	}
	if !complete && !failedBeforeMigration(record) {
		return errors.New("compose rollback backup is incomplete")
	}
	backup := filepath.Join(backupRoot, "compose")
	service, err := a.candidateService()
	if err != nil {
		return err
	}
	if err := a.compose(ctx, "stop", service); err != nil {
		return err
	}
	if err := restoreRollbackDatabase(ctx, a.Layout, record); err != nil {
		return err
	}
	if complete {
		relCompose, err := a.composeRelativePath()
		if err != nil {
			return err
		}
		backupCompose := filepath.Join(backup, relCompose)
		if !isWithin(backup, backupCompose) {
			return errors.New("compose backup path escapes registered backup")
		}
		info, err := os.Stat(backupCompose)
		if err != nil {
			return fmt.Errorf("compose backup is missing registered compose file: %w", err)
		}
		if !info.Mode().IsRegular() {
			return errors.New("compose backup registered file is not regular")
		}
		// Only the administrator-registered Compose file is updater-owned. Keep
		// customer-managed files and bind-mounted data in place during rollback.
		if err := replaceFileAtomically(backupCompose, a.File, 0600); err != nil {
			return fmt.Errorf("restore registered compose file: %w", err)
		}
	}
	if err := a.compose(ctx, "up", "-d", "--pull", "never", "--no-build"); err != nil {
		return err
	}
	return a.validateRollbackRuntime(ctx, record)
}

func (a *ComposeAdapter) compose(ctx context.Context, args ...string) error {
	return a.composeWithFile(ctx, a.File, args...)
}

func (a *ComposeAdapter) composeWithFile(ctx context.Context, composeFile string, args ...string) error {
	if err := a.validate(); err != nil {
		return err
	}
	if !filepath.IsAbs(composeFile) || strings.TrimSpace(composeFile) == "" {
		return errors.New("compose file must be absolute")
	}
	base := []string{"compose", "--project-directory", a.Layout.ProjectDir, "--project-name", a.Layout.Project, "-f", composeFile}
	return a.runner().Run(ctx, "docker", append(base, args...)...)
}

func (a *ComposeAdapter) composeOutput(ctx context.Context, args ...string) ([]byte, error) {
	return a.composeOutputWithFile(ctx, a.File, args...)
}

func (a *ComposeAdapter) composeOutputWithFile(ctx context.Context, composeFile string, args ...string) ([]byte, error) {
	if err := a.validate(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(composeFile) || strings.TrimSpace(composeFile) == "" {
		return nil, errors.New("compose file must be absolute")
	}
	runner, ok := a.runner().(CommandOutputRunner)
	if !ok {
		return nil, errors.New("compose candidate verification requires an output-capable docker runner")
	}
	base := []string{"compose", "--project-directory", a.Layout.ProjectDir, "--project-name", a.Layout.Project, "-f", composeFile}
	return runner.Output(ctx, "docker", append(base, args...)...)
}

func (a *ComposeAdapter) composeRelativePath() (string, error) {
	relCompose, err := filepath.Rel(a.Layout.ProjectDir, a.File)
	if err != nil || relCompose == "." || strings.HasPrefix(relCompose, ".."+string(os.PathSeparator)) {
		return "", errors.New("compose file must be relative to registered project directory")
	}
	return relCompose, nil
}

func (a *ComposeAdapter) candidateService() (string, error) {
	service := a.Layout.Service
	if service == "" {
		service = a.Layout.MigrationService
	}
	if service == "" {
		service = "backend"
	}
	if !validComposeServiceName(service) {
		return "", errors.New("registered compose candidate service is invalid")
	}
	return service, nil
}

const backupCompleteMarker = ".complete.json"

type completedBackup struct {
	Version int `json:"version"`
}

func prepareBackupRoot(root string, record TaskRecord) error {
	if !validTaskID(record.TaskID) {
		return errors.New("backup task ID is invalid")
	}
	if !filepath.IsAbs(root) {
		return errors.New("backup root must be absolute")
	}
	marker := filepath.Join(root, backupCompleteMarker)
	if _, err := os.Lstat(marker); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat backup completion marker: %w", err)
	}
	if _, err := os.Lstat(root); err == nil {
		if record.Phase != PhaseBackingUp {
			return errors.New("incomplete backup can only be rebuilt during backup phase")
		}
		if err := os.RemoveAll(root); err != nil {
			return fmt.Errorf("clear incomplete backup: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat backup root: %w", err)
	}
	return os.MkdirAll(root, 0700)
}

func writeBackupComplete(root string) error {
	data, err := json.Marshal(completedBackup{Version: 1})
	if err != nil {
		return err
	}
	return writeFileAtomically(filepath.Join(root, backupCompleteMarker), data, 0600)
}

func validateBackupComplete(root, sqliteDatabase string) (bool, error) {
	marker := filepath.Join(root, backupCompleteMarker)
	info, err := os.Lstat(marker)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat backup completion marker: %w", err)
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("backup completion marker is not a regular file")
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		return false, fmt.Errorf("read backup completion marker: %w", err)
	}
	var completed completedBackup
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&completed); err != nil {
		return false, fmt.Errorf("decode backup completion marker: %w", err)
	}
	if completed.Version != 1 {
		return false, errors.New("backup completion marker version is unsupported")
	}
	if sqliteDatabase != "" {
		snapshot := filepath.Join(root, "database.sqlite")
		info, err := os.Lstat(snapshot)
		if err != nil {
			return false, fmt.Errorf("complete backup sqlite snapshot is missing: %w", err)
		}
		if !info.Mode().IsRegular() {
			return false, errors.New("complete backup sqlite snapshot is not a regular file")
		}
		if err := checkSQLiteIntegrity(context.Background(), snapshot); err != nil {
			return false, fmt.Errorf("complete backup sqlite snapshot is corrupt: %w", err)
		}
	}
	return true, nil
}

func sqliteSidecarExclusions(dataRoot, database string) (map[string]struct{}, error) {
	excluded := map[string]struct{}{}
	if database == "" || !isWithin(dataRoot, database) {
		return excluded, nil
	}
	rel, err := filepath.Rel(dataRoot, database)
	if err != nil {
		return nil, err
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		excluded[rel+suffix] = struct{}{}
	}
	return excluded, nil
}

// PreparePackage extracts the already verified package into updater-owned
// state, imports its images and records completion before the service is
// stopped. A completed stage is immutable from the workflow's point of view:
// later migration and activation only consume it.
func (a *ComposeAdapter) PreparePackage(ctx context.Context, record TaskRecord) error {
	if err := a.validate(); err != nil {
		return err
	}
	if err := validateSQLiteBackupSource(a.Layout, record); err != nil {
		return err
	}
	if err := reserveComposeBackupCapacity(a.File, a.Layout.State, probeFilesystemCapacity); err != nil {
		return fmt.Errorf("reserve compose backup capacity: %w", err)
	}
	// The external database remains writable while the old Compose service is
	// running. Only validate the bounded backup plan here; Backup performs the
	// task-bound export after Stop has completed.
	if err := validateExternalBackupPlan(a.Layout.State, record, a.ExternalBackup, a.BackupRunner); err != nil {
		return err
	}
	stage, marker, err := a.composeStagePaths(record)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(marker); err == nil {
		_, _, err := a.preparedComposeStage(ctx, record)
		return err
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat prepared compose marker: %w", err)
	}

	// A prior interrupted preparation has no completion marker and is safe to
	// discard. Never remove a stage once its marker exists.
	if err := os.RemoveAll(stage); err != nil {
		return fmt.Errorf("clear incomplete compose staging: %w", err)
	}
	if err := os.MkdirAll(stage, 0700); err != nil {
		return fmt.Errorf("create compose staging: %w", err)
	}
	if err := extractPackage(record.StagingPath, stage); err != nil {
		_ = os.RemoveAll(stage)
		return fmt.Errorf("extract compose package: %w", err)
	}
	packageRoot, err := extractedPackageRoot(stage)
	if err != nil {
		_ = os.RemoveAll(stage)
		return fmt.Errorf("resolve compose package root: %w", err)
	}
	if _, err := a.stagedComposeFile(packageRoot); err != nil {
		_ = os.RemoveAll(stage)
		return err
	}
	for _, image := range record.Images {
		archive := filepath.Join(packageRoot, filepath.FromSlash(image.Archive))
		if !isWithin(packageRoot, archive) {
			_ = os.RemoveAll(stage)
			return fmt.Errorf("compose image archive %q escapes staged package", image.Archive)
		}
		info, statErr := os.Stat(archive)
		if statErr != nil || !info.Mode().IsRegular() {
			if statErr == nil {
				statErr = errors.New("image archive is not a regular file")
			}
			_ = os.RemoveAll(stage)
			return fmt.Errorf("compose image archive %q is unavailable: %w", image.Archive, statErr)
		}
		if err := a.runner().Run(ctx, "docker", "load", "--input", archive); err != nil {
			_ = os.RemoveAll(stage)
			return fmt.Errorf("load compose image %q: %w", image.Reference, err)
		}
		if err := a.verifyImage(ctx, image); err != nil {
			_ = os.RemoveAll(stage)
			return err
		}
	}
	if err := writePreparedComposeMarker(marker, preparedComposePackage{TaskID: record.TaskID, StagingPath: record.StagingPath}); err != nil {
		return fmt.Errorf("persist prepared compose marker: %w", err)
	}
	return nil
}

func reserveComposeBackupCapacity(composeFile, state string, probe func(string) (filesystemCapacity, error)) error {
	info, err := os.Stat(composeFile)
	if err != nil {
		return fmt.Errorf("stat registered compose file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("registered compose file is not regular")
	}
	return checkCapacity([]spaceRequirement{{
		Path:  state,
		Bytes: allocatedStorage(uint64(info.Size())) + capacityReserve,
	}}, probe)
}

// preparedComposeStage returns only a stage that was fully prepared before
// the stop boundary. It intentionally never extracts a package or imports an
// image, so recovery cannot extend downtime by repeating those operations.
func (a *ComposeAdapter) preparedComposeStage(ctx context.Context, record TaskRecord) (string, string, error) {
	if record.StagingPath == "" || !filepath.IsAbs(record.StagingPath) {
		return "", "", errors.New("staged compose artifact path is missing or not absolute")
	}
	stage, marker, err := a.composeStagePaths(record)
	if err != nil {
		return "", "", err
	}
	prepared, err := readPreparedComposeMarker(marker)
	if err != nil {
		return "", "", err
	}
	if prepared.TaskID != record.TaskID || filepath.Clean(prepared.StagingPath) != filepath.Clean(record.StagingPath) {
		return "", "", errors.New("prepared compose marker does not match upgrade task")
	}
	packageRoot, err := extractedPackageRoot(stage)
	if err != nil {
		return "", "", fmt.Errorf("resolve prepared compose package root: %w", err)
	}
	composePath, err := a.stagedComposeFile(packageRoot)
	if err != nil {
		return "", "", err
	}
	for _, image := range record.Images {
		if err := a.verifyImage(ctx, image); err != nil {
			return "", "", err
		}
	}
	return stage, composePath, nil
}

func (a *ComposeAdapter) composeStagePaths(record TaskRecord) (string, string, error) {
	if record.StagingPath == "" || !filepath.IsAbs(record.StagingPath) {
		return "", "", errors.New("staged compose artifact path is missing or not absolute")
	}
	if !validTaskID(record.TaskID) {
		return "", "", errors.New("compose staging task ID is invalid")
	}
	stageRoot := filepath.Join(a.Layout.State, "compose-staging")
	stage := filepath.Join(stageRoot, record.TaskID)
	marker := filepath.Join(stageRoot, record.TaskID+".prepared.json")
	if !isWithin(a.Layout.State, stageRoot) || !isWithin(stageRoot, stage) || !isWithin(stageRoot, marker) {
		return "", "", errors.New("compose staging path escapes updater state")
	}
	return stage, marker, nil
}

func (a *ComposeAdapter) stagedComposeFile(packageRoot string) (string, error) {
	relCompose, err := a.composeRelativePath()
	if err != nil {
		return "", err
	}
	composePath := filepath.Join(packageRoot, relCompose)
	if !isWithin(packageRoot, composePath) {
		return "", errors.New("staged compose path escapes updater state")
	}
	info, err := os.Lstat(composePath)
	if err != nil {
		return "", fmt.Errorf("upgrade package is missing registered compose file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("upgrade package compose file is not regular")
	}
	data, err := os.ReadFile(composePath)
	if err != nil {
		return "", fmt.Errorf("read upgrade package compose file: %w", err)
	}
	if err := validateComposeReferences(data); err != nil {
		return "", err
	}
	return composePath, nil
}

func writePreparedComposeMarker(path string, marker preparedComposePackage) error {
	data, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	return writeFileAtomically(path, data, 0600)
}

func readPreparedComposeMarker(path string) (preparedComposePackage, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return preparedComposePackage{}, errors.New("compose package was not prepared before service stop")
		}
		return preparedComposePackage{}, fmt.Errorf("stat prepared compose marker: %w", err)
	}
	if !info.Mode().IsRegular() {
		return preparedComposePackage{}, errors.New("prepared compose marker is not a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return preparedComposePackage{}, fmt.Errorf("read prepared compose marker: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var marker preparedComposePackage
	if err := decoder.Decode(&marker); err != nil {
		return preparedComposePackage{}, fmt.Errorf("decode prepared compose marker: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return preparedComposePackage{}, errors.New("prepared compose marker contains multiple JSON values")
		}
		return preparedComposePackage{}, fmt.Errorf("decode prepared compose marker trailing data: %w", err)
	}
	return marker, nil
}

// validateRuntimeIdentity runs the packaged fixed readiness command inside
// the running backend container. It never probes a host-published port, which
// could refer to a different process than the candidate Compose service.
func (a *ComposeAdapter) validateRuntimeIdentity(ctx context.Context, record TaskRecord) error {
	expected, err := expectedRuntimeIdentity(record)
	if err != nil {
		return err
	}
	return a.validateRuntimeIdentityExpected(ctx, expected)
}

func (a *ComposeAdapter) validateRollbackRuntime(ctx context.Context, record TaskRecord) error {
	expected, err := expectedRollbackIdentity(record)
	if err != nil {
		return err
	}
	return a.validateRuntimeIdentityExpected(ctx, expected)
}

func (a *ComposeAdapter) validateRuntimeIdentityExpected(ctx context.Context, expected RuntimeIdentity) error {
	service, err := a.candidateService()
	if err != nil {
		return err
	}
	return validateRuntimeIdentityResponses(ctx, expected, a.Probe.Attempts, a.Probe.Interval, a.Probe.Timeout, func(ctx context.Context) (RuntimeIdentity, error) {
		output, err := a.composeOutput(ctx, "exec", "-T", service, "/app/uvp-gb28181", "-upgrade-readiness")
		if err != nil {
			return RuntimeIdentity{}, fmt.Errorf("run in-container candidate readiness command: %w", err)
		}
		if len(output) > maxRuntimeIdentityPayload {
			return RuntimeIdentity{}, errors.New("in-container readiness response exceeds size limit")
		}
		var actual RuntimeIdentity
		if err := decodeStrictJSON(output, &actual); err != nil {
			return RuntimeIdentity{}, fmt.Errorf("decode in-container readiness response: %w", err)
		}
		return actual, nil
	})
}

func (a *ComposeAdapter) validate() error {
	if err := a.Layout.validate(); err != nil {
		return err
	}
	if a.Layout.ProjectDir == "" || a.Layout.Project == "" || a.File == "" {
		return errors.New("compose project registration is incomplete")
	}
	if !isWithin(a.Layout.ProjectDir, a.File) || !filepath.IsAbs(a.File) {
		return errors.New("compose file must be inside registered project directory")
	}
	if strings.ContainsAny(a.Layout.Project, "/\\ \t\n") || a.Layout.Project == "." || a.Layout.Project == ".." {
		return errors.New("compose project name is invalid")
	}
	return nil
}

func validComposeServiceName(value string) bool {
	if value == "" || value == "." || value == ".." || strings.ContainsAny(value, "/\\ \t\n") {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func (a *ComposeAdapter) verifyImage(ctx context.Context, image ImageArtifact) error {
	if image.Digest == "" && image.ConfigID == "" {
		return nil
	}
	runner, ok := a.runner().(CommandOutputRunner)
	if !ok {
		return errors.New("compose image verification requires an output-capable docker runner")
	}
	if image.ConfigID != "" {
		output, err := runner.Output(ctx, "docker", "image", "inspect", "--format={{.Id}}", image.Reference)
		if err != nil {
			return fmt.Errorf("inspect compose image config %q: %w", image.Reference, err)
		}
		if strings.TrimSpace(string(output)) != image.ConfigID {
			return fmt.Errorf("compose image %q config ID does not match signed metadata", image.Reference)
		}
	}
	if image.Digest != "" {
		output, err := runner.Output(ctx, "docker", "image", "inspect", "--format={{index .RepoDigests 0}}", image.Reference)
		if err != nil {
			return fmt.Errorf("inspect compose image digest %q: %w", image.Reference, err)
		}
		value := strings.TrimSpace(string(output))
		// `docker load` commonly restores the image ID/config ID without a
		// RepoDigest. When the signed manifest binds a config_id, that local
		// identity is sufficient for an offline archive; a missing RepoDigest
		// must not turn an otherwise verified image into a false rejection.
		if value == "" && image.ConfigID != "" {
			return nil
		}
		if !strings.HasSuffix(value, "@"+image.Digest) && value != image.Digest {
			return fmt.Errorf("compose image %q digest does not match signed metadata", image.Reference)
		}
	}
	return nil
}

func validateSystemdUnit(unit string) error {
	if unit == "" || filepath.Base(unit) != unit || strings.ContainsAny(unit, "/\\ \t\n") {
		return errors.New("systemd unit is invalid")
	}
	return nil
}

func validateComposeReferences(data []byte) error {
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "image:") {
			continue
		}
		image := strings.TrimSpace(strings.TrimPrefix(trimmed, "image:"))
		if image == "" || image == "latest" || strings.HasSuffix(image, ":latest") {
			return errors.New("compose image must not use latest")
		}
	}
	return nil
}

func extractedPackageRoot(directory string) (string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", err
	}
	packageRoot := directory
	if len(entries) == 1 && entries[0].IsDir() {
		packageRoot = filepath.Join(directory, entries[0].Name())
	}
	if !isWithin(directory, packageRoot) {
		return "", errors.New("extracted package root escapes destination")
	}
	return packageRoot, nil
}

func extractPackage(path, destination string) error {
	if filepath.IsAbs(destination) == false || strings.TrimSpace(destination) == "" {
		return errors.New("package destination must be absolute")
	}
	archive, err := openPackageArchive(path)
	if err != nil {
		return err
	}
	defer archive.Close()
	seen := make(map[string]struct{})
	entries := 0
	var expanded int64
	for {
		entry, err := archive.Next()
		if errors.Is(err, io.EOF) {
			if closeErr := archive.Close(); closeErr != nil {
				return fmt.Errorf("verify package archive: %w", closeErr)
			}
			if err := syncTree(destination); err != nil {
				return fmt.Errorf("sync extracted package: %w", err)
			}
			return nil
		}
		if err != nil {
			return err
		}
		entries++
		if entries > 10000 {
			return errors.New("package contains too many entries")
		}
		name, err := safeArchiveNameWithDirs(entry.Name)
		if err != nil {
			return err
		}
		path := filepath.Join(destination, name)
		if !isWithin(destination, path) {
			return errors.New("archive path escapes destination")
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("package contains duplicate entry %q", name)
		}
		seen[name] = struct{}{}
		if entry.Size < 0 || entry.Size > 8<<30 || expanded > (8<<30)-entry.Size {
			return errors.New("package expanded size exceeds limit")
		}
		expanded += entry.Size
		if entry.Dir {
			if err := os.MkdirAll(path, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		mode := os.FileMode(0600)
		if entry.Exec {
			mode = 0700
		}
		if err := copyFileFromReader(entry.Body, path, entry.Size, mode); err != nil {
			return err
		}
	}
}

func safeArchiveNameWithDirs(name string) (string, error) {
	name = filepath.ToSlash(name)
	if name == "" || filepath.IsAbs(name) || strings.HasPrefix(name, "../") || name == ".." || strings.Contains(name, "\\") {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	return clean, nil
}

func isWithin(root, path string) bool {
	root, path = filepath.Clean(root), filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func copyTree(source, destination string) error {
	return copyTreeExcept(source, destination, nil)
}

func copyTreeExcept(source, destination string, excluded map[string]struct{}) error {
	if err := filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if _, skip := excluded[rel]; skip {
			return nil
		}
		target := filepath.Join(destination, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported backup entry %q", rel)
		}
		return copyFile(path, target, 0600)
	}); err != nil {
		return err
	}
	return syncTree(destination)
}

func copyFile(source, destination string, mode os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	if copyErr != nil {
		_ = out.Close()
		return copyErr
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func copyFileFromReader(reader io.Reader, destination string, size int64, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(out, io.LimitReader(reader, size))
	if copyErr != nil {
		_ = out.Close()
		return copyErr
	}
	if written != size {
		_ = out.Close()
		return errors.New("package entry is truncated")
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// replaceFileAtomically makes a registered file durable before exposing it to
// the deployment. The temporary file is created in the destination directory
// so rename and the directory sync share one filesystem boundary.
func replaceFileAtomically(source, destination string, mode os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("replacement source is not a regular file")
	}
	dir := filepath.Dir(destination)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(destination)+".tmp-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if _, err := io.Copy(tmp, in); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, destination); err != nil {
		return err
	}
	if err := syncDirectory(dir); err != nil {
		return err
	}
	return nil
}

func writeFileAtomically(destination string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(destination)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(destination)+".tmp-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, destination); err != nil {
		return err
	}
	return syncDirectory(dir)
}

func syncTree(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	if info.Mode().IsRegular() {
		return syncFile(root)
	}
	if !info.IsDir() {
		return fmt.Errorf("cannot sync unsupported filesystem entry %q", root)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := syncTree(filepath.Join(root, entry.Name())); err != nil {
			return err
		}
	}
	return syncDirectory(root)
}

func syncFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func moveTreeContents(source, destination string) error {
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		from := filepath.Join(source, entry.Name())
		to := filepath.Join(destination, entry.Name())
		if _, err := os.Lstat(to); err == nil {
			return fmt.Errorf("release contains duplicate entry %q", entry.Name())
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Rename(from, to); err != nil {
			return err
		}
	}
	return nil
}
