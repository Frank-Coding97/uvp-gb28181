//go:build windows

package standalone

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWindowsAbnormalRestartAdmissionDriver(t *testing.T) {
	launcherTest := os.Getenv("UVP_ABNORMAL_LAUNCHER_TEST_PATH")
	if launcherTest == "" {
		t.Skip("requires isolated abnormal restart launcher test")
	}
	backend := maintenanceBackendTestPath(t)
	fixture := newMaintenanceComponentFixture(t, backend, maintenanceComponentReleaseRoot(t))
	copyMaintenanceComponentFile(t, backend, filepath.Join(fixture.current.releaseDir, filepath.FromSlash(releaseBackendPath)))
	fixture.current.manifest = rewriteMaintenanceComponentManifest(t, fixture.current.releaseDir, fixture.current.manifest.Version, fixture.current.manifest.SourceCommit)
	seedUpgradeDatabase(t, fixture)
	seedUpgradeRedis(t, fixture)
	removeUpgradeDriverGate(t, fixture.paths.InstallDir)
	_, err := BeginRun(fixture.paths)
	require.NoError(t, err)
	// Deliberately retain the previous lifetime marker; no real component is
	// running. This tests admission, not the cause of the earlier termination.
	require.NoError(t, fixture.lock.Close())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, launcherTest, "-test.run=^TestWindowsAbnormalRestartCannotOpenBusiness$", "-test.v", "-test.timeout=90s")
	cmd.Env = maintenanceComponentDriverEnvironment(fixture.paths.InstallDir, "abnormal")
	output, err := cmd.CombinedOutput()
	t.Log(redactMaintenanceComponentOutput(t, fixture.paths, string(output)))
	assertUpgradeDriverProcessCleanup(t, fixture)
	require.NoError(t, err)
	require.NotContains(t, string(output), "--- SKIP")
}
