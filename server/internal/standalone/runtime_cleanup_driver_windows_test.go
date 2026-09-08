//go:build windows

package standalone

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestWindowsRuntimeCleanupDriver runs the launcher runtime fault test against
// a disposable installation assembled from the real component images. The
// child owns the runtime process and deliberately leaves its run marker after
// the bounded Redis cleanup; this outer process only verifies the child was
// fully released and that the marker remains.
func TestWindowsRuntimeCleanupDriver(t *testing.T) {
	launcherTest := strings.TrimSpace(os.Getenv("UVP_MAINTENANCE_LAUNCHER_TEST_PATH"))
	if launcherTest == "" {
		t.Skip("requires built Windows launcher test executable")
	}
	backend := maintenanceBackendTestPath(t)
	componentRoot := maintenanceComponentReleaseRoot(t)

	fixture := newMaintenanceComponentFixture(t, backend, componentRoot)
	currentBackend := filepath.Join(fixture.current.releaseDir, filepath.FromSlash(releaseBackendPath))
	copyMaintenanceComponentFile(t, backend, currentBackend)
	fixture.current.manifest = rewriteMaintenanceComponentManifest(t, fixture.current.releaseDir, fixture.current.manifest.Version, fixture.current.manifest.SourceCommit)
	seedUpgradeDatabase(t, fixture)
	seedUpgradeRedis(t, fixture)
	prepareT16StopCurrentRelease(t, &fixture)
	removeUpgradeDriverGate(t, fixture.paths.InstallDir)
	require.NoError(t, fixture.lock.Close())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, launcherTest,
		"-test.run=^TestWindowsUnresponsiveRedisUsesBoundedOwnedCleanup$",
		"-test.v", "-test.timeout=2m45s")
	cmd.Env = runtimeCleanupDriverEnvironment(fixture.paths.InstallDir)
	output, err := cmd.CombinedOutput()
	if ctxErr := ctx.Err(); ctxErr != nil {
		t.Fatalf("unresponsive Redis cleanup launcher timed out: %v", ctxErr)
	}
	if err != nil {
		t.Fatalf("unresponsive Redis cleanup launcher failed (output bytes=%d): %v", len(output), err)
	}
	require.NotContains(t, string(output), "--- SKIP")

	current, err := LoadRelease(fixture.paths.InstallDir)
	require.NoError(t, err)
	require.Equal(t, "t16-stop", current.Version)
	assertRuntimeCleanupDriverStopped(t, fixture.paths.InstallDir, current)
	marker, err := os.ReadFile(filepath.Join(fixture.paths.DataDir, runMarkerName))
	require.NoError(t, err)
	require.NotEmpty(t, marker, "failed runtime must retain the abnormal run marker")
}

func prepareT16StopCurrentRelease(t *testing.T, fixture *maintenanceBackendTestFixture) {
	t.Helper()
	oldReleaseDir := fixture.current.releaseDir
	newReleaseDir := filepath.Join(fixture.paths.InstallDir, "releases", "t16-stop")
	require.NoError(t, os.Rename(oldReleaseDir, newReleaseDir))
	fixture.current.releaseDir = newReleaseDir
	fixture.current.manifest = rewriteMaintenanceComponentManifest(t, newReleaseDir, "t16-stop", fixture.current.manifest.SourceCommit)
	fixture.paths.ResourceDir = filepath.Join(newReleaseDir, "resource")
	fixture.paths.WebDir = filepath.Join(newReleaseDir, "web")
	require.NoError(t, writeCurrentPointerAtomically(
		filepath.Join(fixture.paths.InstallDir, "current.json"),
		[]byte(`{"version":"t16-stop"}`+"\n"),
	))
	selected, err := LoadRelease(fixture.paths.InstallDir)
	require.NoError(t, err)
	require.Equal(t, "t16-stop", selected.Version)
	require.Equal(t, newReleaseDir, selected.ReleaseDir)
}

func runtimeCleanupDriverEnvironment(root string) []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || strings.HasPrefix(strings.ToUpper(key), "UVP_") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "UVP_T16_FAULT_INSTALL_DIR="+root)
}

func assertRuntimeCleanupDriverStopped(t *testing.T, installDir string, current Release) {
	t.Helper()
	require.Eventually(t, func() bool {
		lock, err := AcquireInstanceLock(installDir)
		if err != nil {
			return false
		}
		defer lock.Close()
		return backupComponentsStopped(current) == nil
	}, 15*time.Second, 100*time.Millisecond)
}
