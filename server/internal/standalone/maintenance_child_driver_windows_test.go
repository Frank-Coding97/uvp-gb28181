//go:build windows

package standalone

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestWindowsMaintenanceChildOwnerCrashDriver(t *testing.T) {
	launcherTest := os.Getenv("UVP_MAINTENANCE_LAUNCHER_TEST_PATH")
	if launcherTest == "" {
		t.Skip("requires built Windows launcher test executable")
	}
	fixture := newMaintenanceBackendTestFixture(t, launcherTest)
	require.NoError(t, fixture.lock.Close())
	ready := filepath.Join(t.TempDir(), "child-ready")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, launcherTest, "-test.run=^TestWindowsMaintenanceChildCrashOwner$", "-test.timeout=25s")
	cmd.Env = append(os.Environ(), "UVP_MAINTENANCE_TEST_ROOT="+fixture.paths.InstallDir, "UVP_MAINTENANCE_TEST_CRASH_OWNER=1", "UVP_MAINTENANCE_TEST_CHILD_READY="+ready)
	require.NoError(t, cmd.Start())
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
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
	require.NoError(t, cmd.Process.Kill())
	require.Error(t, cmd.Wait())
	state, err = windows.WaitForSingleObject(handle, 5000)
	require.NoError(t, err)
	require.Equal(t, uint32(windows.WAIT_OBJECT_0), state)
	lock, err := AcquireInstanceLock(fixture.paths.InstallDir)
	require.NoError(t, err)
	require.NoError(t, lock.Close())
	require.ErrorIs(t, CheckMaintenanceGate(fixture.paths.InstallDir), ErrMaintenanceRequired)
}

func TestWindowsMaintenanceChildLauncherDriver(t *testing.T) {
	launcherTest := os.Getenv("UVP_MAINTENANCE_LAUNCHER_TEST_PATH")
	if launcherTest == "" {
		t.Skip("requires built Windows launcher test executable")
	}
	for _, testCase := range []struct{ name, backend, pattern string }{
		{"real-backend", maintenanceBackendTestPath(t), "^(TestWindowsRunMaintenanceChild|TestMaintenanceChild)"},
		{"cancel-live-child", launcherTest, "^TestWindowsMaintenanceChildCancelsLiveProcess$"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newMaintenanceBackendTestFixture(t, testCase.backend)
			// The child launcher owns the lock for its own test calls; this driver only
			// prepares and later removes the isolated fixture.
			require.NoError(t, fixture.lock.Close())
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, launcherTest, "-test.run="+testCase.pattern, "-test.v", "-test.timeout=90s")
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(strings.ToUpper(entry), "UVP_MAINTENANCE_TEST_ROOT=") {
					cmd.Env = append(cmd.Env, entry)
				}
			}
			cmd.Env = append(cmd.Env, "UVP_MAINTENANCE_TEST_ROOT="+fixture.paths.InstallDir)
			output, err := cmd.CombinedOutput()
			t.Log(string(output))
			require.NoError(t, err)
			require.NotContains(t, string(output), "--- SKIP")
			require.ErrorIs(t, CheckMaintenanceGate(fixture.paths.InstallDir), ErrMaintenanceRequired)
			if testCase.name == "real-backend" {
				assertMaintenancePermitMissing(t, fixture.paths.InstallDir)
			}
		})
	}
}
