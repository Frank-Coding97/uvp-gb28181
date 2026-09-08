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

func TestBackupComponentsRejectsExactRunningImage(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err = backupComponentsStopped(Release{BackendExe: executable}); err == nil {
		t.Fatal("running component image accepted")
	}
}

func TestBackupComponentsDoesNotConfuseAnotherInstallation(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), filepath.Base(executable))
	if err = backupComponentsStopped(Release{BackendExe: other}); err != nil {
		t.Fatalf("unrelated image with same basename rejected: %v", err)
	}
}

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
	for _, check := range []struct {
		name     string
		selected int
		path     string
	}{
		{"current", 0, releaseBackendPath}, {"candidate", 1, releaseBackendPath},
		{"retained", 2, releaseBackendPath}, {"other-installation", 3, releaseBackendPath},
		{"candidate-redis", 1, releaseRedisPath}, {"retained-media", 2, releaseMediaPath},
	} {
		t.Run(check.name, func(t *testing.T) {
			current := newTestReleaseFixture(t, "1.0.0")
			candidate := newTestReleaseFixtureAt(t, current.installDir, "2.0.0", false)
			retained := newTestReleaseFixtureAt(t, current.installDir, "0.9.0", false)
			foreign := newTestReleaseFixture(t, "3.0.0")
			fixtures := []testReleaseFixture{current, candidate, retained, foreign}
			var releases []Release
			for _, fixture := range fixtures {
				path := filepath.Join(fixture.releaseDir, filepath.FromSlash(check.path))
				require.NoError(t, os.WriteFile(path, image, 0700))
				for i := range fixture.manifest.Files {
					if fixture.manifest.Files[i].Path == check.path {
						fixture.manifest.Files[i].SHA256 = testSHA256(image)
					}
				}
				writeTestReleaseManifest(t, fixture)
				release, err := LoadReleaseVersion(fixture.installDir, fixture.manifest.Version)
				require.NoError(t, err)
				releases = append(releases, release)
			}
			ready := filepath.Join(t.TempDir(), "ready")
			cmd := exec.Command(filepath.Join(fixtures[check.selected].releaseDir, filepath.FromSlash(check.path)), "-test.run=^TestMaintenanceProcessHelper$", "-test.timeout=20s")
			cmd.Env = append(os.Environ(), "UVP_MAINTENANCE_PROCESS_READY="+ready)
			input, err := cmd.StdinPipe()
			require.NoError(t, err)
			require.NoError(t, cmd.Start())
			defer func() { _ = input.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
			require.Eventually(t, func() bool { _, err := os.Stat(ready); return err == nil }, 10*time.Second, 5*time.Millisecond)
			err = backupComponentsStopped(releases[:3]...)
			if check.selected == 3 {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
