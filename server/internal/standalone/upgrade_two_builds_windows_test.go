//go:build windows

package standalone

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestWindowsUpgradeStoppedTwoBuildsDriver exercises the qualified launcher
// with two distinct backend builds. The fixture only establishes the version
// path and supported-schema boundary; it is not historical-token or physical
// device compatibility evidence.
func TestWindowsUpgradeStoppedTwoBuildsDriver(t *testing.T) {
	oldBackend := upgradeTwoBuildBackendPath(t, "UVP_UPGRADE_OLD_BACKEND_PATH")
	newBackend := upgradeTwoBuildBackendPath(t, "UVP_UPGRADE_NEW_BACKEND_PATH")
	oldSHA, err := releaseFileSHA256(oldBackend)
	require.NoError(t, err)
	newSHA, err := releaseFileSHA256(newBackend)
	require.NoError(t, err)
	require.NotEqual(t, oldSHA, newSHA, "old and new backend builds must have different SHA256 values")
	launcherPath := upgradeTwoBuildLauncherPath(t)
	sourceRoot := maintenanceComponentReleaseRoot(t)

	for _, mode := range []string{"complete", "media-failure"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newTwoBuildUpgradeFixture(t, oldBackend, oldSHA, sourceRoot)
			seedTwoBuildUpgradeDatabase(t, fixture, oldSHA)
			seedUpgradeRedis(t, fixture)
			installTwoBuildCandidateBackend(t, &fixture, newBackend, newSHA)
			beforeConfig, err := os.ReadFile(fixture.paths.ConfigFile)
			require.NoError(t, err)
			beforeCredentials, err := LoadConfig(fixture.paths)
			require.NoError(t, err)
			removeUpgradeDriverGate(t, fixture.paths.InstallDir)
			if mode == "media-failure" {
				breakTwoBuildMediaConfig(t, fixture.paths)
			}
			require.NoError(t, fixture.lock.Close())

			destination := filepath.Join(filepath.Dir(fixture.paths.InstallDir), "upgrade-two-builds-backup")
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
			defer cancel()
			upgradeOutput, upgradeErr := runTwoBuildUpgradeCommand(ctx, launcherPath, fixture, destination)
			t.Logf("two-build upgrade case %s launcher output:\n%s", mode, redactMaintenanceComponentOutput(t, fixture.paths, string(upgradeOutput)))
			if ctxErr := ctx.Err(); ctxErr != nil {
				t.Fatalf("two-build upgrade case %s timed out: %v", mode, ctxErr)
			}

			switch mode {
			case "complete":
				require.NoError(t, upgradeErr)
				require.Contains(t, string(upgradeOutput), "升级已完成："+fixture.journal.CandidateVersion)
				require.NoError(t, CheckMaintenanceGate(fixture.paths.InstallDir))
				require.Len(t, completedUpgradeJournals(t, fixture.paths.InstallDir), 1)
			case "media-failure":
				require.Error(t, upgradeErr)
				require.NotContains(t, string(upgradeOutput), "升级已完成：")
				require.Equal(t, MaintenanceRestoreRequired, readTwoBuildMaintenancePhase(t, fixture.paths.InstallDir))
				require.ErrorIs(t, CheckMaintenanceGate(fixture.paths.InstallDir), ErrMaintenanceRequired)
				restoreOutput, restoreErr := runTwoBuildRestoreCommand(ctx, launcherPath, fixture)
				t.Logf("two-build restore output:\n%s", redactMaintenanceComponentOutput(t, fixture.paths, string(restoreOutput)))
				require.NoError(t, restoreErr)
				require.Contains(t, string(restoreOutput), "恢复已完成，等待本机确认：")
				require.Equal(t, MaintenanceAwaitingConfirmation, readTwoBuildMaintenancePhase(t, fixture.paths.InstallDir))
				require.ErrorIs(t, CheckMaintenanceGate(fixture.paths.InstallDir), ErrMaintenanceRequired)
			}

			require.NoError(t, assertTwoBuildComponentsStopped(t, fixture))
			afterConfig, err := os.ReadFile(fixture.paths.ConfigFile)
			require.NoError(t, err)
			if mode == "complete" {
				require.True(t, bytes.Equal(beforeConfig, afterConfig), "authoritative YAML configuration changed")
			} else {
				afterCredentials, err := LoadConfig(fixture.paths)
				require.NoError(t, err)
				require.NotEqual(t, beforeCredentials.JWTSecret(), afterCredentials.JWTSecret())
				require.NotEqual(t, beforeCredentials.InstanceGeneration(), afterCredentials.InstanceGeneration())
				require.Equal(t, beforeCredentials.RedisPassword(), afterCredentials.RedisPassword())
			}
			backup, err := VerifyBackup(ctx, destination)
			require.NoError(t, err)
			require.Equal(t, fixture.journal.OldVersion, backup.Version)
			selected, err := LoadRelease(fixture.paths.InstallDir)
			require.NoError(t, err)
			candidate, err := LoadReleaseVersion(fixture.paths.InstallDir, fixture.journal.CandidateVersion)
			require.NoError(t, err)
			if mode == "complete" {
				require.Equal(t, fixture.journal.CandidateVersion, selected.Version)
				require.Equal(t, newSHA, releaseFileSHA256Must(t, selected.BackendExe))
			} else {
				require.Equal(t, fixture.journal.OldVersion, selected.Version)
				require.Equal(t, oldSHA, releaseFileSHA256Must(t, selected.BackendExe))
			}
			require.Equal(t, newSHA, releaseFileSHA256Must(t, candidate.BackendExe))
		})
	}
}

func upgradeTwoBuildBackendPath(t *testing.T, environmentKey string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(environmentKey))
	if value == "" {
		t.Skip("requires " + environmentKey + " pointing to a real backend build")
	}
	abs, err := filepath.Abs(value)
	require.NoError(t, err)
	info, err := os.Stat(abs)
	require.NoError(t, err)
	require.False(t, info.IsDir())
	return abs
}

func upgradeTwoBuildLauncherPath(t *testing.T) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv("UVP_UPGRADE_LAUNCHER_PATH"))
	if value == "" {
		t.Skip("requires UVP_UPGRADE_LAUNCHER_PATH pointing to a qualified Windows launcher")
	}
	abs, err := filepath.Abs(value)
	require.NoError(t, err)
	info, err := os.Stat(abs)
	require.NoError(t, err)
	require.False(t, info.IsDir())
	return abs
}

func newTwoBuildUpgradeFixture(t *testing.T, oldBackend, oldSHA, sourceRoot string) maintenanceBackendTestFixture {
	t.Helper()
	fixture := newMaintenanceComponentFixture(t, oldBackend, sourceRoot)
	currentBackend := filepath.Join(fixture.current.releaseDir, filepath.FromSlash(releaseBackendPath))
	copyMaintenanceComponentFile(t, oldBackend, currentBackend)
	fixture.current.manifest = rewriteMaintenanceComponentManifest(t, fixture.current.releaseDir, fixture.current.manifest.Version, fixture.current.manifest.SourceCommit)
	// newMaintenanceComponentFixture temporarily places the old build in the
	// candidate so its permit can authorize bootstrap/migrate before the swap.
	// The candidate is replaced by the new build only after the old build has
	// initialized the active database.
	require.Equal(t, oldSHA, releaseFileSHA256Must(t, fixture.candidateBackendPath()))
	return fixture
}

func seedTwoBuildUpgradeDatabase(t *testing.T, fixture maintenanceBackendTestFixture, oldSHA string) {
	t.Helper()
	for _, action := range []struct {
		purpose string
		args    []string
	}{
		{purpose: "bootstrap_db", args: []string{"-bootstrap-db"}},
		{purpose: "migrate_up", args: []string{"-migrate-up"}},
	} {
		require.Equal(t, oldSHA, releaseFileSHA256Must(t, fixture.candidateBackendPath()))
		frame, err := IssueMaintenancePermit(fixture.paths.InstallDir, fixture.journal.OperationID, action.purpose, fixture.journal.CandidateVersion)
		require.NoError(t, err)
		result := runMaintenanceBackend(t, fixture.candidateBackendPath(), fixture.paths, action.args, frame)
		assertUpgradeDriverMaintenanceSuccess(t, result, frame, action.purpose, fixture.journal.CandidateVersion, fixture.paths.InstallDir)
		clear(frame)
	}
	require.FileExists(t, fixture.paths.DatabasePath)
	require.ErrorIs(t, CheckMaintenanceGate(fixture.paths.InstallDir), ErrMaintenanceRequired)
}

func installTwoBuildCandidateBackend(t *testing.T, fixture *maintenanceBackendTestFixture, newBackend, newSHA string) {
	t.Helper()
	require.NotNil(t, fixture)
	copyMaintenanceComponentFile(t, newBackend, fixture.candidateBackendPath())
	fixture.candidate.manifest = rewriteMaintenanceComponentManifest(t, fixture.candidate.releaseDir, fixture.candidate.manifest.Version, fixture.candidate.manifest.SourceCommit)
	require.Equal(t, newSHA, releaseFileSHA256Must(t, fixture.candidateBackendPath()))
}

func breakTwoBuildMediaConfig(t *testing.T, paths Paths) {
	t.Helper()
	config, err := LoadConfig(paths)
	require.NoError(t, err)
	raw, err := os.ReadFile(config.ZLMConfigPath)
	require.NoError(t, err)
	require.Contains(t, string(raw), "listen_ip=127.0.0.1")
	broken := strings.Replace(string(raw), "listen_ip=127.0.0.1", "listen_ip=203.0.113.200", 1)
	require.NoError(t, os.WriteFile(config.ZLMConfigPath, []byte(broken), 0600))
}

func runTwoBuildUpgradeCommand(ctx context.Context, launcherPath string, fixture maintenanceBackendTestFixture, destination string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, launcherPath, "upgrade", "--install-dir", fixture.paths.InstallDir, "--recordings-dir", fixture.paths.RecordingsDir, "--version", fixture.journal.CandidateVersion, "--backup", destination)
	cmd.Env = upgradeTwoBuildLauncherEnvironment()
	return cmd.CombinedOutput()
}

func runTwoBuildRestoreCommand(ctx context.Context, launcherPath string, fixture maintenanceBackendTestFixture) ([]byte, error) {
	cmd := exec.CommandContext(ctx, launcherPath, "restore", "--install-dir", fixture.paths.InstallDir, "--recordings-dir", fixture.paths.RecordingsDir)
	cmd.Env = upgradeTwoBuildLauncherEnvironment()
	return cmd.CombinedOutput()
}

func upgradeTwoBuildLauncherEnvironment() []string {
	forbidden := map[string]struct{}{
		"UVP_MAINTENANCE_BACKEND_PATH":           {},
		"UVP_MAINTENANCE_COMPONENT_RELEASE_ROOT": {},
		"UVP_MAINTENANCE_LAUNCHER_TEST_PATH":     {},
		"UVP_MAINTENANCE_TEST_ROOT":              {},
		"UVP_UPGRADE_LAUNCHER_PATH":              {},
		"UVP_UPGRADE_NEW_BACKEND_PATH":           {},
		"UVP_UPGRADE_OLD_BACKEND_PATH":           {},
	}
	env := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		upperKey := strings.ToUpper(key)
		if _, skip := forbidden[upperKey]; skip || strings.Contains(upperKey, "TRUST") || strings.Contains(upperKey, "ALLOWLIST") {
			continue
		}
		env = append(env, entry)
	}
	return env
}

func assertTwoBuildComponentsStopped(t *testing.T, fixture maintenanceBackendTestFixture) error {
	t.Helper()
	current, err := LoadRelease(fixture.paths.InstallDir)
	if err != nil {
		return err
	}
	candidate, err := LoadReleaseVersion(fixture.paths.InstallDir, fixture.journal.CandidateVersion)
	if err != nil {
		return err
	}
	return backupComponentsStopped(current, candidate)
}

func readTwoBuildMaintenancePhase(t *testing.T, installDir string) MaintenancePhase {
	t.Helper()
	journal, err := ReadMaintenanceJournal(installDir)
	require.NoError(t, err)
	return journal.Phase
}

func completedUpgradeJournals(t *testing.T, installDir string) []string {
	t.Helper()
	archives, err := filepath.Glob(filepath.Join(installDir, ".uvp-completed-*", "journal.json"))
	require.NoError(t, err)
	return archives
}

func releaseFileSHA256Must(t *testing.T, path string) string {
	t.Helper()
	digest, err := releaseFileSHA256(path)
	require.NoError(t, err)
	return digest
}
