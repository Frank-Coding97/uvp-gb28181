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

const recoveryCorruptionLauncherTestPathEnv = "UVP_RECOVERY_CORRUPTION_LAUNCHER_TEST_PATH"

// TestWindowsRecoveryCorruptionDriver runs the launcher package's normal
// startup path against real component images. Each case owns a fresh isolated
// installation so the deliberately damaged active file cannot affect another
// test or a host installation.
func TestWindowsRecoveryCorruptionDriver(t *testing.T) {
	launcherTest := strings.TrimSpace(os.Getenv(recoveryCorruptionLauncherTestPathEnv))
	if launcherTest == "" {
		t.Skip("requires built Windows launcher test executable")
	}
	backend := maintenanceBackendTestPath(t)
	componentRoot := maintenanceComponentReleaseRoot(t)

	for _, mode := range []string{"sqlite", "config"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newMaintenanceComponentFixture(t, backend, componentRoot)
			currentBackend := filepath.Join(fixture.current.releaseDir, filepath.FromSlash(releaseBackendPath))
			copyMaintenanceComponentFile(t, backend, currentBackend)
			fixture.current.manifest = rewriteMaintenanceComponentManifest(t, fixture.current.releaseDir, fixture.current.manifest.Version, fixture.current.manifest.SourceCommit)
			seedUpgradeDatabase(t, fixture)
			seedUpgradeRedis(t, fixture)
			removeUpgradeDriverGate(t, fixture.paths.InstallDir)
			require.NoError(t, fixture.lock.Close())
			config, err := LoadConfig(fixture.paths)
			require.NoError(t, err)

			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, launcherTest,
				"-test.run=^TestWindowsRecoveryCorruptionLauncher$",
				"-test.v", "-test.timeout=3m")
			cmd.Env = recoveryCorruptionDriverEnvironment(fixture.paths.InstallDir, mode)
			output, err := cmd.CombinedOutput()
			redacted := string(output)
			for _, secret := range []string{config.JWTSecret(), config.RedisPassword(), config.ZLMSecret()} {
				if secret != "" {
					redacted = strings.ReplaceAll(redacted, secret, "[REDACTED]")
				}
			}
			t.Logf("normal startup corruption case %s launcher output:\n%s", mode, redacted)
			if ctxErr := ctx.Err(); ctxErr != nil {
				t.Fatalf("normal startup corruption case %s timed out: %v", mode, ctxErr)
			}
			require.NoError(t, err)
			require.NotContains(t, string(output), "--- SKIP")

			assertRecoveryCorruptionProcessCleanup(t, fixture)
		})
	}
}

func recoveryCorruptionDriverEnvironment(root, mode string) []string {
	forbidden := map[string]struct{}{
		"UVP_RECOVERY_CORRUPTION_TEST_ROOT":   {},
		"UVP_RECOVERY_CORRUPTION_TEST_CASE":   {},
		"UVP_MAINTENANCE_TEST_ROOT":           {},
		"UVP_MAINTENANCE_COMPONENT_CASE":      {},
		"UVP_MAINTENANCE_COMPONENT_TEST_ROOT": {},
	}
	env := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, skip := forbidden[strings.ToUpper(key)]; skip {
				continue
			}
		}
		env = append(env, entry)
	}
	return append(env,
		"UVP_RECOVERY_CORRUPTION_TEST_ROOT="+root,
		"UVP_RECOVERY_CORRUPTION_TEST_CASE="+mode,
	)
}

func assertRecoveryCorruptionProcessCleanup(t *testing.T, fixture maintenanceBackendTestFixture) {
	t.Helper()
	old, err := LoadReleaseVersion(fixture.paths.InstallDir, fixture.journal.OldVersion)
	require.NoError(t, err)
	candidate, err := LoadReleaseVersion(fixture.paths.InstallDir, fixture.journal.CandidateVersion)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		lock, err := AcquireInstanceLock(fixture.paths.InstallDir)
		if err != nil {
			return false
		}
		defer lock.Close()
		return backupComponentsStopped(old, candidate) == nil
	}, 15*time.Second, 100*time.Millisecond)
}
