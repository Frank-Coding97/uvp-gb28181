//go:build !windows

package upgrade

// 这里覆盖的是 Linux 侧的三种部署形态：standalone 软链接切换、systemd 单元、
// compose 项目。它们都建立在符号链接、POSIX 权限位与外部控制脚本之上，Windows
// 无法构造同样的前提（建不了软链接、没有 POSIX 位）。Windows 走的是另一套
// 实现 —— WindowsStandaloneAdapter 的就地换目录，见 adapters_windows_standalone.go，
// 其行为由 cmd/uvp-updater 的端到端验证与 Windows 上的真实升级任务覆盖。

import (
	"archive/tar"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeCommandRunner struct {
	calls [][]string
	dirs  []string
	fail  string
}

func (f *fakeCommandRunner) Run(_ context.Context, name string, args ...string) error {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	if strings.Join(call, " ") == f.fail {
		return os.ErrPermission
	}
	return nil
}

func (f *fakeCommandRunner) RunDir(ctx context.Context, dir, name string, args ...string) error {
	f.dirs = append(f.dirs, dir)
	return f.Run(ctx, name, args...)
}

type fakeCommandOutputRunner struct {
	fakeCommandRunner
	outputs map[string][]byte
	errors  map[string]error
}

type composeLifecycleRunner struct {
	calls      [][]string
	identity   []byte
	identities [][]byte
}

func (r *composeLifecycleRunner) Run(_ context.Context, name string, args ...string) error {
	r.calls = append(r.calls, append([]string{name}, args...))
	return nil
}

func (r *composeLifecycleRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	if len(r.identities) > 0 {
		identity := r.identities[0]
		r.identities = r.identities[1:]
		return append([]byte(nil), identity...), nil
	}
	return append([]byte(nil), r.identity...), nil
}

func TestComposeLifecycleFakeRunnerCoversPrepareStopBackupMigrateActivateValidate(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	state := filepath.Join(root, "state")
	require.NoError(t, os.MkdirAll(project, 0700))
	require.NoError(t, os.MkdirAll(state, 0700))
	composeFile := filepath.Join(project, "compose.yml")
	require.NoError(t, os.WriteFile(composeFile, []byte("services:\n  backend:\n    image: uvp:old\n"), 0600))
	artifact := filepath.Join(root, "upgrade.tar")
	writeUpgradeTar(t, artifact)
	taskID := strings.Repeat("a", 32)
	runner := &composeLifecycleRunner{identity: []byte(`{"version":"1.2.3","build_id":"build-1","task_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","database_ready":true}`)}
	a := &ComposeAdapter{
		Layout: RegisteredLayout{Root: root, State: state, ProjectDir: project, Project: "uvp-prod", Service: "backend", MigrationService: "backend"},
		Runner: runner,
		Probe:  RuntimeIdentityProbe{Attempts: 1, Timeout: time.Second},
		File:   composeFile,
	}
	record := TaskRecord{TaskID: taskID, StagingPath: artifact, Version: "1.2.3", BuildID: "build-1", Phase: PhaseBackingUp}
	require.NoError(t, a.PreparePackage(context.Background(), record))
	require.NoError(t, a.Stop(context.Background(), record))
	require.NoError(t, a.Backup(context.Background(), record))
	require.NoError(t, a.Migrate(context.Background(), record))
	require.NoError(t, a.Activate(context.Background(), record))
	require.NoError(t, a.Validate(context.Background(), record))

	data, err := os.ReadFile(composeFile)
	require.NoError(t, err)
	require.Contains(t, string(data), "uvp:1.2.3")
	require.Len(t, runner.calls, 4)
	require.Equal(t, []string{"docker", "compose"}, runner.calls[0][:2])
	require.Contains(t, strings.Join(runner.calls[1], " "), "run --rm --no-deps --pull never backend -migrate-up")
	require.Contains(t, strings.Join(runner.calls[2], " "), "up -d --pull never --no-build")
	require.Contains(t, strings.Join(runner.calls[3], " "), "exec -T backend /app/uvp-gb28181 -upgrade-readiness")
}

func TestComposeLifecycleFakeRunnerRollsBackFailedCandidate(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	state := filepath.Join(root, "state")
	require.NoError(t, os.MkdirAll(project, 0700))
	require.NoError(t, os.MkdirAll(state, 0700))
	composeFile := filepath.Join(project, "compose.yml")
	previousCompose := "services:\n  app:\n    image: uvp:1.0.0\n"
	require.NoError(t, os.WriteFile(composeFile, []byte(previousCompose), 0600))
	artifact := filepath.Join(root, "upgrade.tar")
	writeUpgradeTar(t, artifact)
	store, record := prepareWorkflowTask(t)
	taskID := record.TaskID
	targetMigration := strings.Repeat("d", 64)
	identity := func(version, buildID string) []byte {
		data, err := json.Marshal(RuntimeIdentity{
			Version: version, BuildID: buildID, TaskID: taskID, DatabaseReady: true,
			DatabaseDialect: "mysql", MigrationDigest: targetMigration,
		})
		require.NoError(t, err)
		return data
	}
	runner := &composeLifecycleRunner{identities: [][]byte{
		identity("1.2.3", "unexpected-build"),
		identity("1.0.0", "build-0"),
	}}
	backupRunner := &fakeExternalBackupRunner{}
	backupPassword := "UVP_TEST_UPGRADE_BACKUP_PASSWORD"
	t.Setenv(backupPassword, "test-only-password")
	adapter := &ComposeAdapter{
		Layout: RegisteredLayout{Root: root, State: state, ProjectDir: project, Project: "uvp-prod", Service: "app", MigrationService: "app"},
		Runner: runner,
		Probe:  RuntimeIdentityProbe{Attempts: 1, Timeout: time.Second},
		File:   composeFile,
		ExternalBackup: &ExternalBackupRegistration{
			Dialect: "mysql", Host: "db.internal", Port: 3306, Database: "uvp", User: "backup", PasswordEnv: backupPassword, MaxBackupBytes: 1 << 20,
		},
		BackupRunner: backupRunner,
	}
	compatibility := &CompatibilityDeclaration{
		SchemaVersion: 1, AutomaticAllowed: true,
		Database: DatabaseCompatibility{Dialect: "mysql", TargetMigrationDigest: targetMigration},
		Rollback: &RollbackCompatibility{AutomaticAllowed: true, DatabaseCompatible: true, DependenciesCompatible: true},
	}
	preflight := &PreflightSnapshot{CurrentVersion: "1.0.0", CurrentBuildID: "build-0", DatabaseDialect: "mysql", MigrationDigest: strings.Repeat("b", 64)}
	require.NoError(t, store.UpdateMetadata(record.TaskID, func(stored *TaskRecord) error {
		stored.StagingPath, stored.Version, stored.BuildID = artifact, "1.2.3", "build-1"
		stored.Preflight, stored.Compatibility = preflight, compatibility
		return nil
	}))
	gate, err := OpenGateStore(filepath.Join(state, "gate.json"))
	require.NoError(t, err)
	workflow := &WorkflowExecutor{
		Store: store, Adapter: adapter, Gate: gate,
		Preflight: func(context.Context) (PreflightSnapshot, error) { return *preflight, nil },
	}
	err = workflow.Apply(context.Background(), record)
	require.Error(t, err)
	require.Contains(t, err.Error(), "automatically rolled back")

	updated, err := store.Get(record.TaskID)
	require.NoError(t, err)
	require.Equal(t, PhaseRolledBack, updated.Phase)
	require.Equal(t, PhaseValidating, updated.FailurePhase)
	require.Equal(t, 1, backupRunner.calls)
	restored, err := os.ReadFile(composeFile)
	require.NoError(t, err)
	require.Equal(t, previousCompose, string(restored))
	gateState, err := gate.Load()
	require.NoError(t, err)
	require.Equal(t, GateNormal, gateState.Mode)

	commands := make([]string, 0, len(runner.calls))
	for _, call := range runner.calls {
		commands = append(commands, strings.Join(call, " "))
	}
	joined := strings.Join(commands, "\n")
	require.Contains(t, joined, " run --rm --no-deps --pull never app -migrate-up")
	require.Contains(t, joined, " exec -T app /app/uvp-gb28181 -upgrade-readiness")
	require.Equal(t, 2, strings.Count(joined, "stop app"))
	require.Equal(t, 2, strings.Count(joined, "up -d --pull never --no-build"))
}

func (f *fakeCommandOutputRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	key := strings.Join(call, " ")
	if err := f.errors[key]; err != nil {
		return nil, err
	}
	return f.outputs[key], nil
}

func TestComposeVerifyImageAllowsOfflineLoadWithoutRepoDigestWhenConfigIDMatches(t *testing.T) {
	runner := &fakeCommandOutputRunner{outputs: map[string][]byte{
		"docker image inspect --format={{.Id}} uvp:test":                  []byte("sha256:config-id\n"),
		"docker image inspect --format={{index .RepoDigests 0}} uvp:test": nil,
	}, errors: map[string]error{}}
	a := &ComposeAdapter{Runner: runner}
	if err := a.verifyImage(context.Background(), ImageArtifact{Reference: "uvp:test", Digest: "sha256:release", ConfigID: "sha256:config-id"}); err != nil {
		t.Fatalf("offline image with matching config ID rejected: %v", err)
	}
}

func TestComposeVerifyImageRequiresDigestWhenConfigIDIsAbsent(t *testing.T) {
	runner := &fakeCommandOutputRunner{outputs: map[string][]byte{
		"docker image inspect --format={{index .RepoDigests 0}} uvp:test": nil,
	}, errors: map[string]error{}}
	a := &ComposeAdapter{Runner: runner}
	if err := a.verifyImage(context.Background(), ImageArtifact{Reference: "uvp:test", Digest: "sha256:release"}); err == nil {
		t.Fatal("image without config ID or RepoDigest was accepted")
	}
}

func TestComposeVerifyImageRejectsMismatchedConfigIDBeforeDigest(t *testing.T) {
	runner := &fakeCommandOutputRunner{outputs: map[string][]byte{
		"docker image inspect --format={{.Id}} uvp:test": []byte("sha256:other\n"),
	}, errors: map[string]error{}}
	a := &ComposeAdapter{Runner: runner}
	if err := a.verifyImage(context.Background(), ImageArtifact{Reference: "uvp:test", ConfigID: "sha256:config-id"}); err == nil {
		t.Fatal("mismatched config ID was accepted")
	}
}

func writeUpgradeTar(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	for name, body := range map[string]string{"bin/app": "new", "compose.yml": "services:\n  app:\n    image: registry.local/uvp:1.2.3@sha256:abc\n"} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0700, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStandaloneActivateAtomicallyReplacesCurrentAndRejectsUnsafeCurrent(t *testing.T) {
	root := t.TempDir()
	layout := RegisteredLayout{Root: root, Control: filepath.Join(root, "ctl")}
	if err := os.MkdirAll(filepath.Join(root, "releases", "old"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "releases", "old"), filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(root, "upgrade.tar")
	writeUpgradeTar(t, artifact)
	a := &StandaloneAdapter{Layout: layout, Runner: &fakeCommandRunner{}}
	if err := a.Activate(context.Background(), TaskRecord{TaskID: strings.Repeat("a", 32), StagingPath: artifact}); err != nil {
		t.Fatal(err)
	}
	target, err := a.CurrentTarget()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(target, strings.Repeat("a", 32)) {
		t.Fatalf("target = %q", target)
	}
	if _, err := os.Stat(filepath.Join(target, "bin", "app")); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "current"), []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.Activate(context.Background(), TaskRecord{TaskID: strings.Repeat("b", 32), StagingPath: artifact}); err == nil {
		t.Fatal("non-symlink current was accepted")
	}
}

func TestStandaloneRollbackRestoresInitialFlatInstallationWithoutSelfLink(t *testing.T) {
	root := t.TempDir()
	a := &StandaloneAdapter{Layout: RegisteredLayout{Root: root}}
	previous, err := a.CurrentTarget()
	if err != nil {
		t.Fatal(err)
	}
	if previous != "" {
		t.Fatalf("initial flat target = %q, want empty", previous)
	}
	taskID := strings.Repeat("h", 32)
	candidate := filepath.Join(root, "releases", taskID)
	if err := os.MkdirAll(candidate, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(candidate, filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	if err := a.switchCurrent(TaskRecord{TaskID: taskID, PreviousTargetCaptured: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(root, "current")); !os.IsNotExist(err) {
		t.Fatalf("current after flat rollback = %v, want missing link", err)
	}
}

func TestReplaceFileAtomicallyReplacesDestinationWithoutLeavingTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "candidate.yml")
	destination := filepath.Join(dir, "compose.yml")
	if err := os.WriteFile(source, []byte("services:\n  backend:\n    image: uvp:new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("services:\n  backend:\n    image: uvp:old\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := replaceFileAtomically(source, destination, 0600); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "services:\n  backend:\n    image: uvp:new\n"; got != want {
		t.Fatalf("destination = %q, want %q", got, want)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".compose.yml.tmp-") {
			t.Fatalf("temporary compose file remains: %q", entry.Name())
		}
	}
}

func writeStandaloneMigrationTar(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	entries := []struct {
		name string
		mode int64
		body string
	}{
		{name: "uvp-gb28181-ctl.sh", mode: 0700, body: "#!/bin/sh\nexit 0\n"},
		{name: "bin/uvp-server", mode: 0700, body: "target-binary"},
	}
	for _, entry := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: entry.name, Mode: entry.mode, Size: int64(len(entry.body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeSystemdMigrationTar(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	entries := []struct {
		name string
		mode int64
		body string
	}{
		{name: "release/backend/uvp-gb28181", mode: 0700, body: "target-binary"},
		{name: "release/backend/resource/public/placeholder", mode: 0600, body: "placeholder"},
	}
	for _, entry := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: entry.name, Mode: entry.mode, Size: int64(len(entry.body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStandaloneMigrateUsesStagedControlAndKeepsCurrentRelease(t *testing.T) {
	root := t.TempDir()
	runner := &fakeCommandRunner{}
	old := filepath.Join(root, "releases", "old")
	if err := os.MkdirAll(old, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(old, filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(root, "upgrade.tar")
	writeStandaloneMigrationTar(t, artifact)
	a := &StandaloneAdapter{Layout: RegisteredLayout{Root: root, Control: filepath.Join(root, "uvp-gb28181-ctl.sh")}, Runner: runner}
	record := TaskRecord{TaskID: strings.Repeat("m", 32), StagingPath: artifact}
	if err := a.Migrate(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "updater-state", "migration-staging", record.TaskID, "uvp-gb28181-ctl.sh") + " migrate"
	if len(runner.calls) != 1 || strings.Join(runner.calls[0], " ") != want {
		t.Fatalf("calls = %#v, want target control %q", runner.calls, want)
	}
	if len(runner.dirs) != 1 || runner.dirs[0] != filepath.Join(root, "updater-state", "migration-staging", record.TaskID) {
		t.Fatalf("working directories = %#v", runner.dirs)
	}
	current, err := os.Readlink(filepath.Join(root, "current"))
	if err != nil {
		t.Fatal(err)
	}
	if current != old {
		t.Fatalf("migration changed current release to %q", current)
	}
}

func TestSystemdMigrateUsesTargetBinaryAndKeepsCurrentRelease(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "updater-state")
	old := filepath.Join(root, "releases", "old")
	if err := os.MkdirAll(filepath.Join(root, "config"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(old, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(old, filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(root, "upgrade.tar")
	writeSystemdMigrationTar(t, artifact)
	runner := &fakeCommandRunner{}
	record := TaskRecord{TaskID: strings.Repeat("s", 32), StagingPath: artifact}
	a := &SystemdAdapter{
		StandaloneAdapter: StandaloneAdapter{Layout: RegisteredLayout{Root: root, State: state}, Runner: runner},
		Unit:              "uvp-backend.service",
	}
	if err := a.Migrate(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(state, "migration-staging", record.TaskID, "release", "backend", "uvp-gb28181") + " -migrate-up"
	if len(runner.calls) != 1 || strings.Join(runner.calls[0], " ") != want {
		t.Fatalf("calls = %#v, want target migration %q", runner.calls, want)
	}
	if len(runner.dirs) != 1 || runner.dirs[0] != filepath.Join(state, "migration-staging", record.TaskID, "release", "backend") {
		t.Fatalf("working directories = %#v", runner.dirs)
	}
	current, err := os.Readlink(filepath.Join(root, "current"))
	if err != nil {
		t.Fatal(err)
	}
	if current != old {
		t.Fatalf("migration changed current release to %q", current)
	}
}

func TestSystemdActivateLinksPersistentRuntimePathsBeforeSwitch(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{
		filepath.Join(root, "config"),
		filepath.Join(root, "data", "logs"),
		filepath.Join(root, "data", "uploads"),
		filepath.Join(root, "releases", "old"),
	} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "releases", "old"), filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(root, "upgrade.tar")
	writeSystemdMigrationTar(t, artifact)
	record := TaskRecord{TaskID: strings.Repeat("a", 32), StagingPath: artifact}
	a := &SystemdAdapter{
		StandaloneAdapter: StandaloneAdapter{Layout: RegisteredLayout{Root: root}},
		Unit:              "uvp-backend.service",
	}
	if err := a.Activate(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	target, err := a.CurrentTarget()
	if err != nil {
		t.Fatal(err)
	}
	for relative, want := range map[string]string{
		"backend/config":                  filepath.Join(root, "config"),
		"backend/resource/logs":           filepath.Join(root, "data", "logs"),
		"backend/resource/public/uploads": filepath.Join(root, "data", "uploads"),
	} {
		got, err := os.Readlink(filepath.Join(target, relative))
		if err != nil {
			t.Fatalf("read persistent link %s: %v", relative, err)
		}
		if got != want {
			t.Fatalf("persistent link %s = %q, want %q", relative, got, want)
		}
	}
}

func TestSystemdValidateReadsBackendReleaseIdentity(t *testing.T) {
	root := t.TempDir()
	release := filepath.Join(root, "releases", "task")
	if err := os.MkdirAll(filepath.Join(release, "backend"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(release, "backend", "version.json"), []byte(`{"version":"1.2.3","build_id":"build-1"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(release, filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	runner := &fakeCommandRunner{}
	a := &SystemdAdapter{
		StandaloneAdapter: StandaloneAdapter{Layout: RegisteredLayout{Root: root}, Runner: runner},
		Unit:              "uvp-backend.service",
	}
	if err := a.Validate(context.Background(), TaskRecord{Version: "1.2.3", BuildID: "build-1"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(runner.calls[0], " "); got != "systemctl start uvp-backend.service" {
		t.Fatalf("start call = %q", got)
	}
	if got := strings.Join(runner.calls[1], " "); got != "systemctl is-active --quiet uvp-backend.service" {
		t.Fatalf("active call = %q", got)
	}
}

func TestStandaloneRollbackPropagatesStartFailure(t *testing.T) {
	root := t.TempDir()
	previous := filepath.Join(root, "releases", "old")
	if err := os.MkdirAll(previous, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "releases", "new"), filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	control := filepath.Join(root, "ctl")
	runner := &fakeCommandRunner{fail: control + " start"}
	a := &StandaloneAdapter{Layout: RegisteredLayout{Root: root, Control: control}, Runner: runner}
	record := TaskRecord{
		TaskID:         strings.Repeat("a", 32),
		PreviousTarget: previous,
		Phase:          PhaseStopping,
		Preflight: &PreflightSnapshot{
			CurrentVersion: "1.1.0", CurrentBuildID: "source-build", DatabaseDialect: "sqlite", MigrationDigest: strings.Repeat("b", 64),
		},
	}
	err := a.Rollback(context.Background(), record)
	if err == nil {
		t.Fatal("rollback reported success after service restart failed")
	}
	current, readErr := os.Readlink(filepath.Join(root, "current"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if current != previous {
		t.Fatalf("current release = %q, want previous %q", current, previous)
	}
}

func TestStandaloneValidateChecksActiveBuildIdentity(t *testing.T) {
	root := t.TempDir()
	release := filepath.Join(root, "releases", "task")
	if err := os.MkdirAll(release, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(release, "version.json"), []byte(`{"version":"1.2.3","build_id":"build-1"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(release, filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	runner := &fakeCommandRunner{}
	a := &StandaloneAdapter{Layout: RegisteredLayout{Root: root, Control: filepath.Join(root, "ctl")}, Runner: runner}
	if err := a.Validate(context.Background(), TaskRecord{Version: "1.2.3", BuildID: "wrong"}); err == nil {
		t.Fatal("mismatched active build was accepted")
	}
	if err := a.Validate(context.Background(), TaskRecord{Version: "1.2.3", BuildID: "build-1"}); err != nil {
		t.Fatal(err)
	}
}

func TestStandaloneValidateChecksRunningCandidateIdentity(t *testing.T) {
	root := t.TempDir()
	release := filepath.Join(root, "releases", "task")
	if err := os.MkdirAll(release, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(release, "version.json"), []byte(`{"version":"1.2.3","build_id":"build-1"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(release, filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	taskID := strings.Repeat("a", 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != RuntimeIdentityPath {
			t.Fatalf("probe path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"version":"1.2.3","build_id":"build-1","task_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","database_ready":true}`))
	}))
	defer server.Close()
	runner := &fakeCommandRunner{}
	a := &StandaloneAdapter{
		Layout: RegisteredLayout{Root: root, Control: filepath.Join(root, "ctl"), ReadinessURL: server.URL + RuntimeIdentityPath},
		Runner: runner,
		Probe:  RuntimeIdentityProbe{Attempts: 1},
	}
	if err := a.Validate(context.Background(), TaskRecord{TaskID: taskID, Version: "1.2.3", BuildID: "build-1"}); err != nil {
		t.Fatal(err)
	}
}

func TestStandaloneValidateChecksCandidateTargetMigrationIdentity(t *testing.T) {
	root := t.TempDir()
	release := filepath.Join(root, "releases", "task")
	if err := os.MkdirAll(release, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(release, "version.json"), []byte(`{"version":"1.2.3","build_id":"build-1"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(release, filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	taskID := strings.Repeat("a", 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":"1.2.3","build_id":"build-1","task_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","database_ready":true,"migration_digest":"` + strings.Repeat("b", 64) + `","database_dialect":"sqlite"}`))
	}))
	defer server.Close()
	a := &StandaloneAdapter{
		Layout: RegisteredLayout{Root: root, Control: filepath.Join(root, "ctl"), ReadinessURL: server.URL + RuntimeIdentityPath},
		Runner: &fakeCommandRunner{},
		Probe:  RuntimeIdentityProbe{Attempts: 1},
	}
	record := TaskRecord{
		TaskID:  taskID,
		Version: "1.2.3",
		BuildID: "build-1",
		Compatibility: &CompatibilityDeclaration{Database: DatabaseCompatibility{
			Dialect:               "sqlite",
			TargetMigrationDigest: strings.Repeat("c", 64),
		}},
	}
	if err := a.Validate(context.Background(), record); err == nil {
		t.Fatal("candidate with an undeclared target migration state was accepted")
	}
}

func TestRegisteredLayoutAllowsExternalUpdaterState(t *testing.T) {
	root := t.TempDir()
	layout := RegisteredLayout{Root: root, State: filepath.Join(t.TempDir(), "updater-state")}
	if err := layout.validate(); err != nil {
		t.Fatalf("external updater state was rejected: %v", err)
	}
	layout.State = "relative-state"
	if err := layout.validate(); err == nil {
		t.Fatal("relative updater state was accepted")
	}
}

func TestComposeUsesRegisteredProjectAndNeverLatest(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	runner := &fakeCommandRunner{}
	a := &ComposeAdapter{Layout: RegisteredLayout{Root: root, State: filepath.Join(root, "state"), ProjectDir: project, Project: "uvp-prod"}, Runner: runner, File: filepath.Join(project, "compose.yml")}
	if err := a.Stop(context.Background(), TaskRecord{}); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 || runner.calls[0][0] != "docker" || runner.calls[0][1] != "compose" {
		t.Fatalf("calls = %#v", runner.calls)
	}
	joined := strings.Join(runner.calls[0], " ")
	if !strings.Contains(joined, "--project-directory "+project) || !strings.Contains(joined, "--project-name uvp-prod") {
		t.Fatalf("unregistered compose invocation: %s", joined)
	}
	if err := os.WriteFile(filepath.Join(project, "compose.yml"), []byte("services:\n  app:\n    image: latest\n"), 0600); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(root, "compose.tar")
	writeUpgradeTar(t, artifact)
	record := TaskRecord{TaskID: strings.Repeat("c", 32), StagingPath: artifact}
	if err := a.PreparePackage(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if err := a.Activate(context.Background(), record); err != nil {
		t.Fatal(err)
	}
}

func TestComposeMigrateUsesRegisteredServiceWithoutDependencies(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	sourceCompose := "services:\n  app:\n    image: registry.local/uvp:1.1.0@sha256:old\n"
	if err := os.WriteFile(filepath.Join(project, "compose.yml"), []byte(sourceCompose), 0600); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(root, "compose.tar")
	writeUpgradeTar(t, artifact)
	runner := &fakeCommandRunner{}
	a := &ComposeAdapter{
		Layout: RegisteredLayout{Root: root, State: filepath.Join(root, "state"), ProjectDir: project, Project: "uvp-prod", MigrationService: "app"},
		Runner: runner,
		File:   filepath.Join(project, "compose.yml"),
	}
	record := TaskRecord{TaskID: strings.Repeat("a", 32), StagingPath: artifact}
	if err := a.PreparePackage(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if err := a.Migrate(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %#v", runner.calls)
	}
	joined := strings.Join(runner.calls[0], " ")
	targetCompose := filepath.Join(root, "state", "compose-staging", record.TaskID, "compose.yml")
	if !strings.Contains(joined, " -f "+targetCompose+" run --rm --no-deps --pull never app -migrate-up") {
		t.Fatalf("migration command = %s", joined)
	}
	data, err := os.ReadFile(filepath.Join(project, "compose.yml"))
	if err != nil || string(data) != sourceCompose {
		t.Fatalf("target compose was activated before migration: data=%q err=%v", data, err)
	}
}

func TestComposeMigrateRequiresPreparedPackage(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(root, "compose.tar")
	writeUpgradeTar(t, artifact)
	a := &ComposeAdapter{
		Layout: RegisteredLayout{Root: root, State: filepath.Join(root, "state"), ProjectDir: project, Project: "uvp-prod", MigrationService: "app"},
		Runner: &fakeCommandRunner{},
		File:   filepath.Join(project, "compose.yml"),
	}
	if err := a.Migrate(context.Background(), TaskRecord{TaskID: strings.Repeat("a", 32), StagingPath: artifact}); err == nil {
		t.Fatal("migration prepared the compose package after service stop")
	}
}

func TestComposeBackupCopiesOnlyRegisteredComposeFileAndReusesCompleteSnapshot(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(filepath.Join(project, "customer-data"), 0755); err != nil {
		t.Fatal(err)
	}
	composeFile := filepath.Join(project, "compose.yml")
	if err := os.WriteFile(composeFile, []byte("services:\n  backend:\n    image: uvp:old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "customer-data", "keep.txt"), []byte("customer-owned"), 0600); err != nil {
		t.Fatal(err)
	}
	record := TaskRecord{TaskID: strings.Repeat("d", 32), Phase: PhaseBackingUp}
	a := &ComposeAdapter{
		Layout: RegisteredLayout{Root: root, State: state, ProjectDir: project, Project: "uvp-prod"},
		Runner: &fakeCommandRunner{},
		File:   composeFile,
	}
	if err := a.Backup(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(state, "backups", record.TaskID)
	backupCompose := filepath.Join(backup, "compose", "compose.yml")
	if data, err := os.ReadFile(backupCompose); err != nil || string(data) != "services:\n  backend:\n    image: uvp:old\n" {
		t.Fatalf("compose backup = %q, err=%v", data, err)
	}
	if _, err := os.Stat(filepath.Join(backup, "compose", "customer-data", "keep.txt")); !os.IsNotExist(err) {
		t.Fatalf("customer project data was copied into updater backup: %v", err)
	}
	if err := os.WriteFile(composeFile, []byte("services:\n  backend:\n    image: uvp:new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	record.Phase = PhaseMigrating
	if err := a.Backup(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(backupCompose); err != nil || string(data) != "services:\n  backend:\n    image: uvp:old\n" {
		t.Fatalf("complete compose backup was overwritten: %q, err=%v", data, err)
	}
}

func TestStandaloneBackupUsesRegisteredSQLiteSnapshotAndRejectsIncompleteRetryAfterBackupPhase(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(root, "custom", "installed.sqlite")
	if err := os.MkdirAll(filepath.Join(root, "config"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "data"), 0755); err != nil {
		t.Fatal(err)
	}
	createSQLiteValue(t, database, "before-upgrade")
	record := TaskRecord{TaskID: strings.Repeat("e", 32), Phase: PhaseBackingUp, Preflight: &PreflightSnapshot{DatabaseDialect: "sqlite"}}
	a := &StandaloneAdapter{Layout: RegisteredLayout{Root: root, State: filepath.Join(root, "state"), SQLiteDatabase: database}}
	if err := a.Backup(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(root, "state", "backups", record.TaskID, "database.sqlite")
	if got := readSQLiteValue(t, snapshot); got != "before-upgrade" {
		t.Fatalf("snapshot value = %q", got)
	}
	createSQLiteValue(t, database, "after-upgrade")
	record.Phase = PhaseMigrating
	if err := a.Backup(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if got := readSQLiteValue(t, snapshot); got != "before-upgrade" {
		t.Fatalf("complete snapshot was overwritten: %q", got)
	}

	partial := TaskRecord{TaskID: strings.Repeat("f", 32), Phase: PhaseMigrating, Preflight: &PreflightSnapshot{DatabaseDialect: "sqlite"}}
	partialRoot := filepath.Join(root, "state", "backups", partial.TaskID)
	if err := os.MkdirAll(partialRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partialRoot, "partial"), []byte("interrupted"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.Backup(context.Background(), partial); err == nil {
		t.Fatal("incomplete backup was rebuilt outside the backup phase")
	}
}

func createSQLiteValue(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE IF NOT EXISTS values_table (value TEXT); DELETE FROM values_table; INSERT INTO values_table(value) VALUES (?)", value); err != nil {
		t.Fatal(err)
	}
}

func readSQLiteValue(t *testing.T, path string) string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var value string
	if err := db.QueryRow("SELECT value FROM values_table LIMIT 1").Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestComposePreparePackageResolvesOuterReleaseDirectory(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "compose.yml"), []byte("services:\n  app:\n    image: registry.local/uvp:1.1.0@sha256:old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(root, "compose.tar")
	f, err := os.Create(artifact)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(f)
	body := "services:\n  app:\n    image: registry.local/uvp:1.2.3@sha256:abc\n"
	if err := tw.WriteHeader(&tar.Header{Name: "release/compose.yml", Mode: 0600, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	record := TaskRecord{TaskID: strings.Repeat("b", 32), StagingPath: artifact}
	a := &ComposeAdapter{
		Layout: RegisteredLayout{Root: root, State: filepath.Join(root, "state"), ProjectDir: project, Project: "uvp-prod", MigrationService: "app"},
		Runner: &fakeCommandRunner{},
		File:   filepath.Join(project, "compose.yml"),
	}
	if err := a.PreparePackage(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	prepared := filepath.Join(root, "state", "compose-staging", record.TaskID, "release", "compose.yml")
	if _, err := os.Stat(prepared); err != nil {
		t.Fatalf("prepared release compose is missing: %v", err)
	}
}

func TestComposePreparePackageDefersExternalBackupUntilAfterStop(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(root, "compose.tar")
	writeUpgradeTar(t, artifact)
	composeFile := filepath.Join(project, "compose.yml")
	if err := os.WriteFile(composeFile, []byte("services:\n  app:\n    image: uvp:old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UVP_UPGRADE_DB_PASSWORD", "test-password")
	runner := &fakeExternalBackupRunner{}
	record := TaskRecord{TaskID: strings.Repeat("c", 32), StagingPath: artifact, Phase: PhaseBackingUp}
	a := &ComposeAdapter{
		Layout: RegisteredLayout{Root: root, State: filepath.Join(root, "state"), ProjectDir: project, Project: "uvp-prod"},
		Runner: &fakeCommandRunner{},
		File:   composeFile,
		ExternalBackup: &ExternalBackupRegistration{
			Dialect: "mysql", Host: "db.internal", Port: 3306, Database: "uvp", User: "backup",
			PasswordEnv: "UVP_UPGRADE_DB_PASSWORD", MaxBackupBytes: 1024,
		},
		BackupRunner: runner,
	}
	if err := a.PreparePackage(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if runner.calls != 0 {
		t.Fatalf("external backup calls before stop = %d, want 0", runner.calls)
	}
	if err := a.Stop(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if err := a.Backup(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if runner.calls != 1 {
		t.Fatalf("external backup calls after stop = %d, want 1", runner.calls)
	}
	if err := validatePreparedExternalBackup(a.Layout.State, record, a.ExternalBackup); err != nil {
		t.Fatalf("prepared external backup: %v", err)
	}
}

func TestComposeBackupRejectsExternalDatabaseWithoutPassword(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	composeFile := filepath.Join(project, "compose.yml")
	if err := os.WriteFile(composeFile, []byte("services:\n  app:\n    image: uvp:old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeExternalBackupRunner{}
	a := &ComposeAdapter{
		Layout: RegisteredLayout{Root: root, State: filepath.Join(root, "state"), ProjectDir: project, Project: "uvp-prod"},
		Runner: &fakeCommandRunner{},
		File:   composeFile,
		ExternalBackup: &ExternalBackupRegistration{
			Dialect: "postgresql", Host: "db.internal", Port: 5432, Database: "uvp", User: "backup",
			PasswordEnv: "UVP_UPGRADE_DB_PASSWORD", MaxBackupBytes: 1024,
		},
		BackupRunner: runner,
	}
	record := TaskRecord{TaskID: strings.Repeat("d", 32), Phase: PhaseBackingUp}
	if err := a.Backup(context.Background(), record); err == nil {
		t.Fatal("backup accepted an external database without a password")
	}
	if runner.calls != 0 {
		t.Fatalf("backup phase invoked an external database export before validating its plan: %d calls", runner.calls)
	}
}

func TestComposeBackupReexportsMissingExternalDatabaseBackupAfterRetry(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(root, "compose.tar")
	writeUpgradeTar(t, artifact)
	composeFile := filepath.Join(project, "compose.yml")
	if err := os.WriteFile(composeFile, []byte("services:\n  app:\n    image: uvp:old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UVP_UPGRADE_DB_PASSWORD", "test-password")
	runner := &fakeExternalBackupRunner{}
	record := TaskRecord{TaskID: strings.Repeat("e", 32), StagingPath: artifact, Phase: PhaseBackingUp}
	a := &ComposeAdapter{
		Layout: RegisteredLayout{Root: root, State: filepath.Join(root, "state"), ProjectDir: project, Project: "uvp-prod"},
		Runner: &fakeCommandRunner{},
		File:   composeFile,
		ExternalBackup: &ExternalBackupRegistration{
			Dialect: "mysql", Host: "db.internal", Port: 3306, Database: "uvp", User: "backup",
			PasswordEnv: "UVP_UPGRADE_DB_PASSWORD", MaxBackupBytes: 1024,
		},
		BackupRunner: runner,
	}
	if err := a.PreparePackage(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if runner.calls != 0 {
		t.Fatalf("external backup calls before stop = %d, want 0", runner.calls)
	}
	if err := a.Stop(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if err := a.Backup(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if runner.calls != 1 {
		t.Fatalf("external backup calls after stop = %d, want 1", runner.calls)
	}
	_, _, marker, err := preparedExternalBackupPaths(a.Layout.State, record.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	record.Phase = PhaseMigrating
	if err := a.Backup(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if runner.calls != 2 {
		t.Fatalf("external backup retry calls = %d, want 2", runner.calls)
	}
}

func TestComposeBackupCreatesRegisteredSQLiteSnapshotAfterStop(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	database := filepath.Join(root, "volume", "uvp.sqlite")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "compose.yml"), []byte("services:\n  backend:\n    image: uvp:old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	createSQLiteValue(t, database, "before-stop")
	a := &ComposeAdapter{
		Layout: RegisteredLayout{
			Root: root, State: filepath.Join(root, "state"), ProjectDir: project, Project: "uvp-prod",
			SQLiteDatabase: database,
		},
		Runner: &fakeCommandRunner{},
		File:   filepath.Join(project, "compose.yml"),
	}
	record := TaskRecord{
		TaskID: strings.Repeat("g", 32), Phase: PhaseBackingUp,
		Preflight: &PreflightSnapshot{DatabaseDialect: "sqlite"},
	}
	if err := a.Backup(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(a.Layout.State, "backups", record.TaskID, "database.sqlite")
	if got := readSQLiteValue(t, snapshot); got != "before-stop" {
		t.Fatalf("compose sqlite snapshot value = %q", got)
	}
	if complete, err := a.validateBackupComplete(filepath.Dir(snapshot), database); err != nil || !complete {
		t.Fatalf("compose sqlite backup validation = complete:%v err:%v", complete, err)
	}
}

func TestComposeBackupRejectsCompleteMarkerWithoutRegisteredComposeFile(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	record := TaskRecord{TaskID: strings.Repeat("f", 32), Phase: PhaseMigrating}
	backupRoot := filepath.Join(root, "state", "backups", record.TaskID)
	if err := os.MkdirAll(backupRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeBackupComplete(backupRoot); err != nil {
		t.Fatal(err)
	}
	a := &ComposeAdapter{
		Layout: RegisteredLayout{Root: root, State: filepath.Join(root, "state"), ProjectDir: project, Project: "uvp-prod"},
		Runner: &fakeCommandRunner{},
		File:   filepath.Join(project, "compose.yml"),
	}
	if err := a.Backup(context.Background(), record); err == nil {
		t.Fatal("complete backup accepted a missing registered compose file")
	}
}

func TestComposeValidateNeverRemovesOrphansOrPullsImages(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	runner := &fakeCommandRunner{}
	a := &ComposeAdapter{
		Layout: RegisteredLayout{Root: root, ProjectDir: project, Project: "uvp-prod"},
		Runner: runner,
		File:   filepath.Join(project, "compose.yml"),
	}
	if err := a.Validate(context.Background(), TaskRecord{}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(runner.calls[0], " ")
	if strings.Contains(joined, "--remove-orphans") || !strings.Contains(joined, "--pull never") || !strings.Contains(joined, "--no-build") {
		t.Fatalf("compose validation flags = %s", joined)
	}
}

func TestComposeValidateChecksCandidateIdentityInsideRegisteredBackend(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	composeFile := filepath.Join(project, "compose.yml")
	taskID := strings.Repeat("a", 32)
	command := "docker compose --project-directory " + project + " --project-name uvp-prod -f " + composeFile + " exec -T backend /app/uvp-gb28181 -upgrade-readiness"
	runner := &fakeCommandOutputRunner{outputs: map[string][]byte{
		command: []byte(`{"version":"1.2.3","build_id":"build-1","task_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","database_ready":true}`),
	}, errors: map[string]error{}}
	a := &ComposeAdapter{
		Layout: RegisteredLayout{Root: root, ProjectDir: project, Project: "uvp-prod", Service: "backend"},
		Runner: runner,
		Probe:  RuntimeIdentityProbe{Attempts: 1, Timeout: time.Second},
		File:   composeFile,
	}
	if err := a.Validate(context.Background(), TaskRecord{TaskID: taskID, Version: "1.2.3", BuildID: "build-1"}); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls = %#v", runner.calls)
	}
	if got := strings.Join(runner.calls[1], " "); got != command {
		t.Fatalf("candidate readiness command = %q", got)
	}
}

func TestComposeValidateRejectsNonJSONCandidateCommandOutput(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	composeFile := filepath.Join(project, "compose.yml")
	command := "docker compose --project-directory " + project + " --project-name uvp-prod -f " + composeFile + " exec -T backend /app/uvp-gb28181 -upgrade-readiness"
	runner := &fakeCommandOutputRunner{outputs: map[string][]byte{
		command: []byte("startup log\\n{}"),
	}, errors: map[string]error{}}
	a := &ComposeAdapter{
		Layout: RegisteredLayout{Root: root, ProjectDir: project, Project: "uvp-prod", Service: "backend"},
		Runner: runner,
		Probe:  RuntimeIdentityProbe{Attempts: 1, Interval: time.Millisecond, Timeout: 10 * time.Millisecond},
		File:   composeFile,
	}
	if err := a.Validate(context.Background(), TaskRecord{TaskID: strings.Repeat("a", 32), Version: "1.2.3", BuildID: "build-1"}); err == nil {
		t.Fatal("non-JSON in-container readiness output was accepted")
	}
}

func TestComposeRollbackPropagatesRestartFailure(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	state := filepath.Join(root, "state")
	taskID := strings.Repeat("a", 32)
	backupRoot := filepath.Join(state, "backups", taskID)
	backup := filepath.Join(backupRoot, "compose")
	if err := os.MkdirAll(backup, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "compose.yml"), []byte("services:\n  app:\n    image: uvp:test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeBackupComplete(backupRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	composeFile := filepath.Join(project, "compose.yml")
	if err := os.WriteFile(composeFile, []byte("services:\n  app:\n    image: uvp:new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeCommandRunner{}
	a := &ComposeAdapter{
		Layout: RegisteredLayout{Root: root, State: state, ProjectDir: project, Project: "uvp-prod"},
		Runner: runner,
		File:   composeFile,
	}
	finalUp := "docker compose --project-directory " + project + " --project-name uvp-prod -f " + composeFile + " up -d --pull never --no-build"
	runner.fail = finalUp
	record := TaskRecord{
		TaskID: taskID,
		Phase:  PhaseStopping,
		Preflight: &PreflightSnapshot{
			CurrentVersion: "1.1.0", CurrentBuildID: "source-build", DatabaseDialect: "sqlite", MigrationDigest: strings.Repeat("b", 64),
		},
	}
	if err := a.Rollback(context.Background(), record); err == nil {
		t.Fatal("rollback reported success after compose restart failed")
	}
	if _, err := os.Stat(filepath.Join(project, "compose.yml")); err != nil {
		t.Fatalf("restored compose file is missing after restart failure: %v", err)
	}
}

func TestComposeRollbackRestoresOnlyRegisteredComposeFile(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	state := filepath.Join(root, "state")
	taskID := strings.Repeat("b", 32)
	backupRoot := filepath.Join(state, "backups", taskID)
	backup := filepath.Join(backupRoot, "compose")
	if err := os.MkdirAll(filepath.Join(backup, "data"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "compose.yml"), []byte("services:\n  backend:\n    image: uvp:old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeBackupComplete(backupRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "data", "customer.txt"), []byte("old backup"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, "data"), 0755); err != nil {
		t.Fatal(err)
	}
	composeFile := filepath.Join(project, "compose.yml")
	if err := os.WriteFile(composeFile, []byte("services:\n  backend:\n    image: uvp:new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "data", "customer.txt"), []byte("live customer data"), 0600); err != nil {
		t.Fatal(err)
	}
	command := "docker compose --project-directory " + project + " --project-name uvp-prod -f " + composeFile + " exec -T backend /app/uvp-gb28181 -upgrade-readiness"
	runner := &fakeCommandOutputRunner{outputs: map[string][]byte{
		command: []byte(`{"version":"1.1.0","build_id":"source-build","task_id":"` + taskID + `","database_ready":true,"migration_digest":"` + strings.Repeat("b", 64) + `","database_dialect":"sqlite"}`),
	}, errors: map[string]error{}}
	a := &ComposeAdapter{
		Layout: RegisteredLayout{Root: root, State: state, ProjectDir: project, Project: "uvp-prod"},
		Runner: runner,
		Probe:  RuntimeIdentityProbe{Attempts: 1, Timeout: time.Second},
		File:   composeFile,
	}
	record := TaskRecord{
		TaskID: taskID,
		Phase:  PhaseStopping,
		Preflight: &PreflightSnapshot{
			CurrentVersion: "1.1.0", CurrentBuildID: "source-build", DatabaseDialect: "sqlite", MigrationDigest: strings.Repeat("b", 64),
		},
	}
	if err := a.Rollback(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	compose, err := os.ReadFile(composeFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(compose) != "services:\n  backend:\n    image: uvp:old\n" {
		t.Fatalf("restored compose = %q", compose)
	}
	data, err := os.ReadFile(filepath.Join(project, "data", "customer.txt"))
	if err != nil {
		t.Fatalf("customer data was removed: %v", err)
	}
	if string(data) != "live customer data" {
		t.Fatalf("customer data = %q", data)
	}
}

func TestLoadRegistrationRejectsUnknownFieldsAndBuildsFixedAdapter(t *testing.T) {
	path := filepath.Join(secureRegistrationTestDir(t), "registration.json")
	if err := os.WriteFile(path, []byte(`{"deployment":"systemd","systemd_unit":"uvp.service","standalone":{"root":"/opt/uvp"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	adapter, err := LoadRegistration(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := adapter.(*SystemdAdapter); !ok {
		t.Fatalf("adapter type = %T", adapter)
	}
	if err := os.WriteFile(path, []byte(`{"deployment":"systemd","systemd_unit":"uvp.service","standalone":{"root":"/opt/uvp"},"command":"rm -rf /"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRegistration(path); err == nil {
		t.Fatal("unknown registration field accepted")
	}
}
