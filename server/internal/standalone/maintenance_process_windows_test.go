//go:build windows

package standalone

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMaintenanceProcessHelper(t *testing.T) {
	ready := os.Getenv("UVP_MAINTENANCE_PROCESS_READY")
	if ready == "" {
		t.Skip("subprocess helper")
	}
	require.NoError(t, os.WriteFile(ready, []byte("ready"), 0600))
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func TestWindowsMaintenanceProcessesCoverEveryRelease(t *testing.T) {
	self, err := os.Executable()
	require.NoError(t, err)
	image, err := os.ReadFile(self)
	require.NoError(t, err)
	for _, selected := range []int{0, 1, 2, 3} {
		t.Run([]string{"current", "candidate", "retained", "other-installation"}[selected], func(t *testing.T) {
			current := newTestReleaseFixture(t, "1.0.0")
			candidate := newTestReleaseFixtureAt(t, current.installDir, "2.0.0", false)
			retained := newTestReleaseFixtureAt(t, current.installDir, "0.9.0", false)
			foreign := newTestReleaseFixture(t, "3.0.0")
			fixtures := []testReleaseFixture{current, candidate, retained, foreign}
			var releases []Release
			for _, fixture := range fixtures {
				path := filepath.Join(fixture.releaseDir, filepath.FromSlash(releaseBackendPath))
				require.NoError(t, os.WriteFile(path, image, 0700))
				fixture.manifest.Files[0].SHA256 = testSHA256(image)
				writeTestReleaseManifest(t, fixture)
				release, err := LoadReleaseVersion(fixture.installDir, fixture.manifest.Version)
				require.NoError(t, err)
				releases = append(releases, release)
			}
			ready := filepath.Join(t.TempDir(), "ready")
			cmd := exec.Command(releases[selected].BackendExe, "-test.run=^TestMaintenanceProcessHelper$", "-test.timeout=20s")
			cmd.Env = append(os.Environ(), "UVP_MAINTENANCE_PROCESS_READY="+ready)
			input, err := cmd.StdinPipe()
			require.NoError(t, err)
			require.NoError(t, cmd.Start())
			defer func() { _ = input.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
			require.Eventually(t, func() bool { _, err := os.Stat(ready); return err == nil }, 10*time.Second, 5*time.Millisecond)
			err = backupComponentsStopped(releases[:3]...)
			if selected == 3 {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
