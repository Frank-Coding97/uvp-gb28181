//go:build windows

package standalone

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type maintenanceBackendTestFixture struct {
	current   testReleaseFixture
	candidate testReleaseFixture
	paths     Paths
	journal   MaintenanceJournal
	lock      *InstanceLock
}

type maintenanceBackendTestResult struct {
	exitCode int
	stdout   string
	stderr   string
	args     []string
	env      []string
}

func TestWindowsMaintenanceBackendConsumesPermitAndRejectsReplay(t *testing.T) {
	backend := maintenanceBackendTestPath(t)
	fixture := newMaintenanceBackendTestFixture(t, backend)
	frame, err := IssueMaintenancePermit(fixture.current.installDir, fixture.journal.OperationID, "db_check", fixture.candidate.manifest.Version)
	require.NoError(t, err)

	result := runMaintenanceBackend(t, fixture.candidateBackendPath(), fixture.paths, []string{"-db-check"}, frame)
	require.Equal(t, 0, result.exitCode)
	assertMaintenanceCompleteOutput(t, result.stdout)
	assertMaintenanceBackendNoPermitLeak(t, result, frame)
	require.FileExists(t, fixture.paths.DatabasePath)
	assertMaintenancePermitMissing(t, fixture.current.installDir)
	require.ErrorIs(t, CheckMaintenanceGate(fixture.current.installDir), ErrMaintenanceRequired)

	replay := runMaintenanceBackend(t, fixture.candidateBackendPath(), fixture.paths, []string{"-db-check"}, frame)
	require.NotEqual(t, 0, replay.exitCode)
	assertMaintenanceBackendNoPermitLeak(t, replay, frame)
	require.ErrorIs(t, CheckMaintenanceGate(fixture.current.installDir), ErrMaintenanceRequired)
}

func TestWindowsMaintenanceBackendRejectsWrongPurposeWithoutDatabase(t *testing.T) {
	backend := maintenanceBackendTestPath(t)
	fixture := newMaintenanceBackendTestFixture(t, backend)
	frame, err := IssueMaintenancePermit(fixture.current.installDir, fixture.journal.OperationID, "db_check", fixture.candidate.manifest.Version)
	require.NoError(t, err)

	result := runMaintenanceBackend(t, fixture.candidateBackendPath(), fixture.paths, []string{"-bootstrap-db"}, frame)
	require.NotEqual(t, 0, result.exitCode)
	assertMaintenanceBackendNoPermitLeak(t, result, frame)
	assertMaintenanceDatabaseMissing(t, fixture.paths.DatabasePath)
	assertMaintenancePermitPresent(t, fixture.current.installDir)
	require.ErrorIs(t, CheckMaintenanceGate(fixture.current.installDir), ErrMaintenanceRequired)
}

func TestWindowsMaintenanceBackendRejectsMissingPermitWithoutDatabase(t *testing.T) {
	backend := maintenanceBackendTestPath(t)
	fixture := newMaintenanceBackendTestFixture(t, backend)

	result := runMaintenanceBackend(t, fixture.candidateBackendPath(), fixture.paths, []string{"-db-check"}, nil)
	require.NotEqual(t, 0, result.exitCode)
	assertMaintenanceBackendNoPermitLeak(t, result, nil)
	assertMaintenanceDatabaseMissing(t, fixture.paths.DatabasePath)
	assertMaintenancePermitMissing(t, fixture.current.installDir)
	require.ErrorIs(t, CheckMaintenanceGate(fixture.current.installDir), ErrMaintenanceRequired)
}

func TestWindowsMaintenanceBackendRejectsPermitWithoutParentLock(t *testing.T) {
	backend := maintenanceBackendTestPath(t)
	fixture := newMaintenanceBackendTestFixture(t, backend)
	frame, err := IssueMaintenancePermit(fixture.current.installDir, fixture.journal.OperationID, "db_check", fixture.candidate.manifest.Version)
	require.NoError(t, err)
	require.NoError(t, fixture.lock.Close())

	result := runMaintenanceBackend(t, fixture.candidateBackendPath(), fixture.paths, []string{"-db-check"}, frame)
	require.NotEqual(t, 0, result.exitCode)
	assertMaintenanceBackendNoPermitLeak(t, result, frame)
	assertMaintenanceDatabaseMissing(t, fixture.paths.DatabasePath)
	assertMaintenancePermitPresent(t, fixture.current.installDir)
	require.ErrorIs(t, CheckMaintenanceGate(fixture.current.installDir), ErrMaintenanceRequired)
}

func maintenanceBackendTestPath(t *testing.T) string {
	t.Helper()
	path := strings.TrimSpace(os.Getenv("UVP_MAINTENANCE_BACKEND_PATH"))
	if path == "" {
		t.Skip("requires UVP_MAINTENANCE_BACKEND_PATH pointing to a built backend")
	}
	absolute, err := filepath.Abs(path)
	require.NoError(t, err)
	info, err := os.Stat(absolute)
	require.NoError(t, err)
	require.False(t, info.IsDir())
	return absolute
}

func newMaintenanceBackendTestFixture(t *testing.T, backend string) maintenanceBackendTestFixture {
	t.Helper()
	current := newTestReleaseFixture(t, "1.2.3-win10")
	candidate := newTestReleaseFixtureAt(t, current.installDir, "2.0.0-win10", false)
	backendData, err := os.ReadFile(backend)
	require.NoError(t, err)
	candidateBackend := filepath.Join(candidate.releaseDir, filepath.FromSlash(releaseBackendPath))
	require.NoError(t, os.WriteFile(candidateBackend, backendData, 0700))
	candidate.manifest.Files[0].Data = backendData
	candidate.manifest.Files[0].SHA256 = testSHA256(backendData)
	writeTestReleaseManifest(t, candidate)

	options := PathOptions{
		InstallDir:    current.installDir,
		ConfigDir:     filepath.Join(current.installDir, "config"),
		ResourceDir:   filepath.Join(current.installDir, "resource"),
		WebDir:        filepath.Join(current.installDir, "web"),
		DataDir:       filepath.Join(current.installDir, "data"),
		RecordingsDir: filepath.Join(current.installDir, "recordings"),
	}
	for _, dir := range []string{options.ConfigDir, options.ResourceDir, options.WebDir, options.DataDir, options.RecordingsDir} {
		require.NoError(t, os.MkdirAll(dir, 0700))
	}
	paths, err := ResolvePaths(options)
	require.NoError(t, err)
	lock, err := AcquireInstanceLock(current.installDir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.Close()) })
	_, err = InitializeConfig(paths)
	require.NoError(t, err)

	currentRaw, err := os.ReadFile(filepath.Join(current.installDir, "current.json"))
	require.NoError(t, err)
	journal := MaintenanceJournal{
		Schema:               1,
		OperationID:          strings.Repeat("a", 64),
		OldVersion:           current.manifest.Version,
		CandidateVersion:     candidate.manifest.Version,
		OldCurrentSHA256:     testSHA256(currentRaw),
		BackupRoot:           filepath.Join(filepath.Dir(current.installDir), "maintenance-backup"),
		BackupManifestSHA256: testSHA256([]byte("maintenance-backup")),
		Phase:                MaintenanceUpgrading,
		CreatedAt:            time.Now().UTC(),
	}
	require.NoError(t, createMaintenanceJournal(current.installDir, journal))
	return maintenanceBackendTestFixture{current: current, candidate: candidate, paths: paths, journal: journal, lock: lock}
}

func (f maintenanceBackendTestFixture) candidateBackendPath() string {
	return filepath.Join(f.candidate.releaseDir, filepath.FromSlash(releaseBackendPath))
}

func runMaintenanceBackend(t *testing.T, backend string, paths Paths, args []string, frame []byte) maintenanceBackendTestResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, backend, args...)
	cmd.Env = maintenanceBackendEnvironment(paths)
	cmd.Stdin = bytes.NewReader(frame)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatal("maintenance backend timed out")
	}
	result := maintenanceBackendTestResult{stdout: stdout.String(), stderr: stderr.String(), args: append([]string(nil), cmd.Args...), env: append([]string(nil), cmd.Env...)}
	if err == nil {
		return result
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("maintenance backend failed to start: %v", err)
	}
	result.exitCode = exitErr.ExitCode()
	return result
}

func maintenanceBackendEnvironment(paths Paths) []string {
	values := map[string]string{
		EnvInstallDir:    paths.InstallDir,
		EnvConfigDir:     paths.ConfigDir,
		EnvResourceDir:   paths.ResourceDir,
		EnvWebDir:        paths.WebDir,
		EnvDataDir:       paths.DataDir,
		EnvRecordingsDir: paths.RecordingsDir,
	}
	env := make([]string, 0, len(os.Environ())+len(values))
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, replace := values[strings.ToUpper(key)]; replace {
				continue
			}
		}
		env = append(env, entry)
	}
	for _, key := range []string{EnvInstallDir, EnvConfigDir, EnvResourceDir, EnvWebDir, EnvDataDir, EnvRecordingsDir} {
		env = append(env, key+"="+values[key])
	}
	return env
}

func assertMaintenanceCompleteOutput(t *testing.T, stdout string) {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(stdout))
	var documents []map[string]any
	for {
		var document map[string]any
		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		documents = append(documents, document)
	}
	require.Len(t, documents, 2)
	last := documents[len(documents)-1]
	require.Equal(t, "maintenance_complete", last["status"])
	require.Equal(t, "db_check", last["purpose"])
}

func assertMaintenanceBackendNoPermitLeak(t *testing.T, result maintenanceBackendTestResult, frame []byte) {
	t.Helper()
	if len(frame) == 0 {
		return
	}
	token := string(frame[len(maintenancePermitFramePrefix) : len(frame)-1])
	for _, value := range []string{result.stdout, result.stderr, strings.Join(result.args, "\x00"), strings.Join(result.env, "\x00")} {
		if strings.Contains(value, string(frame)) || strings.Contains(value, token) {
			t.Error("maintenance permit frame leaked into backend output or process metadata")
		}
	}
}

func assertMaintenanceDatabaseMissing(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func assertMaintenancePermitMissing(t *testing.T, installDir string) {
	t.Helper()
	_, err := os.Stat(filepath.Join(installDir, maintenanceDirName, maintenancePermitFileName))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func assertMaintenancePermitPresent(t *testing.T, installDir string) {
	t.Helper()
	_, err := os.Stat(filepath.Join(installDir, maintenanceDirName, maintenancePermitFileName))
	require.NoError(t, err)
}
