//go:build windows

package launcher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

func init() {
	ready := os.Getenv("UVP_MAINTENANCE_TEST_CHILD_READY")
	if ready != "" && len(os.Args) == 2 && os.Args[1] == "-db-check" {
		if os.WriteFile(ready, []byte(strconv.Itoa(os.Getpid())), 0600) != nil {
			os.Exit(92)
		}
		time.Sleep(time.Hour)
		os.Exit(93)
	}
}

func TestWindowsMaintenanceChildCancelsLiveProcess(t *testing.T) {
	fixture := newMaintenanceChildFixture(t)
	ready := filepath.Join(t.TempDir(), "child-ready")
	t.Setenv("UVP_MAINTENANCE_TEST_CHILD_READY", ready)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runMaintenanceChild(ctx, fixture.paths, fixture.journal.OperationID, "db_check", fixture.journal.CandidateVersion)
	}()
	var pid int
	require.Eventually(t, func() bool {
		raw, err := os.ReadFile(ready)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(string(raw))
		return err == nil && pid > 0
	}, 10*time.Second, 5*time.Millisecond)
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	require.NoError(t, err)
	defer windows.CloseHandle(handle)
	state, err := windows.WaitForSingleObject(handle, 0)
	require.NoError(t, err)
	require.Equal(t, uint32(windows.WAIT_TIMEOUT), state)
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(10 * time.Second):
		t.Fatal("cancel did not finish maintenance child cleanup")
	}
	state, err = windows.WaitForSingleObject(handle, 0)
	require.NoError(t, err)
	require.Equal(t, uint32(windows.WAIT_OBJECT_0), state)
	require.ErrorIs(t, standalone.CheckMaintenanceGate(fixture.paths.InstallDir), standalone.ErrMaintenanceRequired)
}

func TestWindowsMaintenanceChildCrashOwner(t *testing.T) {
	if os.Getenv("UVP_MAINTENANCE_TEST_CRASH_OWNER") != "1" {
		t.Skip("requires isolated parent-crash driver")
	}
	fixture := newMaintenanceChildFixture(t)
	_ = runMaintenanceChild(context.Background(), fixture.paths, fixture.journal.OperationID, "db_check", fixture.journal.CandidateVersion)
	t.Fatal("crash owner returned before the driver terminated it")
}

const (
	maintenanceChildTestRootEnv    = "UVP_MAINTENANCE_TEST_ROOT"
	maintenanceChildTestBackendEnv = "UVP_MAINTENANCE_BACKEND_PATH"
	maintenanceChildPermitPath     = ".uvp-maintenance/permit.json"
)

type maintenanceChildFixture struct {
	paths   standalone.Paths
	journal standalone.MaintenanceJournal
	lock    *standalone.InstanceLock
}

func newMaintenanceChildFixture(t *testing.T) maintenanceChildFixture {
	t.Helper()
	root := strings.TrimSpace(os.Getenv(maintenanceChildTestRootEnv))
	if root == "" {
		t.Skip("requires an isolated Windows maintenance fixture")
	}
	backend := strings.TrimSpace(os.Getenv(maintenanceChildTestBackendEnv))
	if backend == "" {
		t.Skip("requires UVP_MAINTENANCE_BACKEND_PATH")
	}
	if _, err := os.Stat(backend); err != nil {
		t.Fatalf("maintenance backend fixture: %v", err)
	}

	release, err := standalone.LoadRelease(root)
	require.NoError(t, err)
	journal, err := standalone.ReadMaintenanceJournal(root)
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("requires a prepared maintenance journal in the Windows fixture")
	}
	require.NoError(t, err)
	require.NoError(t, func() error {
		_, err := standalone.LoadReleaseVersion(root, journal.CandidateVersion)
		return err
	}())
	paths, err := standalone.ResolvePaths(standalone.PathOptions{
		InstallDir: root, ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"),
		ResourceDir: release.ResourceDir, WebDir: release.WebDir, RecordingsDir: filepath.Join(root, "recordings"),
	})
	require.NoError(t, err)
	lock, err := standalone.AcquireInstanceLock(root)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.Close()) })
	requireMaintenanceChildPermitMissing(t, root)
	require.ErrorIs(t, standalone.CheckMaintenanceGate(root), standalone.ErrMaintenanceRequired)
	return maintenanceChildFixture{paths: paths, journal: journal, lock: lock}
}

func requireMaintenanceChildPermitMissing(t *testing.T, root string) {
	t.Helper()
	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(maintenanceChildPermitPath)))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestMaintenanceChildPurposeArgs(t *testing.T) {
	for purpose, expected := range map[string][]string{
		"bootstrap_db":     {"-bootstrap-db"},
		"migrate_up":       {"-migrate-up"},
		"db_check":         {"-db-check"},
		"candidate_health": nil,
		"revoke_sessions":  nil,
	} {
		t.Run(purpose, func(t *testing.T) {
			args, err := maintenanceChildArgs(purpose)
			require.NoError(t, err)
			require.Equal(t, expected, args)
		})
	}
	_, err := maintenanceChildArgs("unknown")
	require.Error(t, err)
}

func TestWindowsRunMaintenanceChildChecksCompletionAndConsumesPermit(t *testing.T) {
	fixture := newMaintenanceChildFixture(t)
	err := runMaintenanceChild(context.Background(), fixture.paths, fixture.journal.OperationID, "db_check", fixture.journal.CandidateVersion)
	require.NoError(t, err)
	requireMaintenanceChildPermitMissing(t, fixture.paths.InstallDir)
	require.ErrorIs(t, standalone.CheckMaintenanceGate(fixture.paths.InstallDir), standalone.ErrMaintenanceRequired)
	_, err = os.Stat(fixture.paths.DatabasePath)
	require.NoError(t, err)
	_ = fixture.lock
}

func TestWindowsRunMaintenanceChildRejectsInvalidInputWithoutPermit(t *testing.T) {
	fixture := newMaintenanceChildFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := runMaintenanceChild(ctx, fixture.paths, fixture.journal.OperationID, "db_check", fixture.journal.CandidateVersion)
	require.ErrorIs(t, err, context.Canceled)
	requireMaintenanceChildPermitMissing(t, fixture.paths.InstallDir)

	err = runMaintenanceChild(context.Background(), fixture.paths, fixture.journal.OperationID, "unknown", fixture.journal.CandidateVersion)
	require.Error(t, err)
	requireMaintenanceChildPermitMissing(t, fixture.paths.InstallDir)

	err = runMaintenanceChild(context.Background(), fixture.paths, fixture.journal.OperationID, "db_check", "invalid-version")
	require.Error(t, err)
	requireMaintenanceChildPermitMissing(t, fixture.paths.InstallDir)
	require.ErrorIs(t, standalone.CheckMaintenanceGate(fixture.paths.InstallDir), standalone.ErrMaintenanceRequired)
	_ = fixture.lock
}
