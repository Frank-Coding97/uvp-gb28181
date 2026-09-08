//go:build windows

package standalone

import (
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

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

// TestWindowsUpgradeStoppedTransactionDriver exercises the public launcher
// transaction against real component images in an isolated fixture. The
// current and candidate backend are intentionally the same supplied build:
// this proves the transaction wiring and cleanup, not two-version support.
func TestWindowsUpgradeStoppedTransactionDriver(t *testing.T) {
	launcherTest := strings.TrimSpace(os.Getenv("UVP_MAINTENANCE_LAUNCHER_TEST_PATH"))
	if launcherTest == "" {
		t.Skip("requires built Windows launcher test executable")
	}
	backend := maintenanceBackendTestPath(t)
	componentRoot := maintenanceComponentReleaseRoot(t)

	for _, mode := range []string{"complete", "media-failure", "bad-candidate"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newMaintenanceComponentFixture(t, backend, componentRoot)
			currentBackend := filepath.Join(fixture.current.releaseDir, filepath.FromSlash(releaseBackendPath))
			copyMaintenanceComponentFile(t, backend, currentBackend)
			fixture.current.manifest = rewriteMaintenanceComponentManifest(t, fixture.current.releaseDir, fixture.current.manifest.Version, fixture.current.manifest.SourceCommit)

			seedUpgradeDatabase(t, fixture)
			seedUpgradeRedis(t, fixture)
			removeUpgradeDriverGate(t, fixture.paths.InstallDir)
			require.NoError(t, fixture.lock.Close())

			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, launcherTest,
				"-test.run=^TestWindowsUpgradeStoppedTransaction$",
				"-test.v", "-test.timeout=3m")
			cmd.Env = maintenanceUpgradeDriverEnvironment(fixture.paths.InstallDir, mode)
			output, err := cmd.CombinedOutput()
			t.Logf("upgrade transaction case %s launcher output:\n%s", mode, redactMaintenanceComponentOutput(t, fixture.paths, string(output)))
			if ctxErr := ctx.Err(); ctxErr != nil {
				t.Fatalf("upgrade transaction case %s timed out: %v", mode, ctxErr)
			}
			require.NoError(t, err)
			require.NotContains(t, string(output), "--- SKIP")

			assertUpgradeDriverProcessCleanup(t, fixture)
			assertUpgradeDriverOutcome(t, fixture, mode)
		})
	}
}

func seedUpgradeRedis(t *testing.T, fixture maintenanceBackendTestFixture) {
	t.Helper()
	config, err := LoadConfig(fixture.paths)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(fixture.current.releaseDir, filepath.FromSlash(releaseRedisPath)), filepath.Base(config.RedisConfigPath))
	cmd.Dir = fixture.paths.ConfigDir
	require.NoError(t, cmd.Start())
	waited := false
	defer func() {
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	client := redis.NewClient(&redis.Options{Addr: config.RedisAddress(), Password: config.RedisPassword()})
	defer client.Close()
	for client.Ping(ctx).Err() != nil {
		require.NoError(t, ctx.Err(), "fixture Redis did not become ready")
		time.Sleep(25 * time.Millisecond)
	}
	require.NoError(t, client.Set(ctx, "upgrade-fixture", "persisted", 0).Err())
	// SHUTDOWN may close the connection before replying. The child exit is
	// authoritative; the transaction subsequently verifies the persisted AOF.
	_ = client.Shutdown(ctx).Err()
	err = cmd.Wait()
	waited = true
	require.NoError(t, err)
}

func seedUpgradeDatabase(t *testing.T, fixture maintenanceBackendTestFixture) {
	t.Helper()
	for _, action := range []struct {
		purpose string
		args    []string
	}{
		{purpose: "bootstrap_db", args: []string{"-bootstrap-db"}},
		{purpose: "migrate_up", args: []string{"-migrate-up"}},
	} {
		frame, err := IssueMaintenancePermit(fixture.paths.InstallDir, fixture.journal.OperationID, action.purpose, fixture.journal.CandidateVersion)
		require.NoError(t, err)
		result := runMaintenanceBackend(t, fixture.candidateBackendPath(), fixture.paths, action.args, frame)
		assertUpgradeDriverMaintenanceSuccess(t, result, frame, action.purpose, fixture.journal.CandidateVersion, fixture.paths.InstallDir)
		clear(frame)
	}
	require.FileExists(t, fixture.paths.DatabasePath)
	require.ErrorIs(t, CheckMaintenanceGate(fixture.paths.InstallDir), ErrMaintenanceRequired)
}

func assertUpgradeDriverMaintenanceSuccess(t *testing.T, result maintenanceBackendTestResult, frame []byte, purpose, version, installDir string) {
	t.Helper()
	assertMaintenanceBackendNoPermitLeak(t, result, frame)
	require.Zero(t, result.exitCode, "maintenance %s failed", purpose)
	decoder := json.NewDecoder(strings.NewReader(result.stdout))
	var last map[string]any
	for {
		var document map[string]any
		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		last = document
	}
	require.Equal(t, "maintenance_complete", last["status"])
	require.Equal(t, purpose, last["purpose"])
	require.Equal(t, version, last["version"])
	assertMaintenancePermitMissing(t, installDir)
}

func removeUpgradeDriverGate(t *testing.T, installDir string) {
	t.Helper()
	gate := filepath.Join(installDir, maintenanceDirName)
	info, err := os.Lstat(gate)
	require.NoError(t, err)
	require.True(t, info.IsDir())
	require.Zero(t, info.Mode()&os.ModeSymlink)
	require.NoError(t, os.RemoveAll(gate))
	_, err = os.Lstat(gate)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func maintenanceUpgradeDriverEnvironment(root, mode string) []string {
	env := maintenanceComponentDriverEnvironment(root, mode)
	filtered := make([]string, 0, len(env)+1)
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(key, "UVP_UPGRADE_TEST_CASE") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return append(filtered, "UVP_UPGRADE_TEST_CASE="+mode)
}

func assertUpgradeDriverProcessCleanup(t *testing.T, fixture maintenanceBackendTestFixture) {
	t.Helper()
	old, err := LoadReleaseVersion(fixture.paths.InstallDir, fixture.journal.OldVersion)
	require.NoError(t, err)
	candidate := Release{
		Version:     fixture.journal.CandidateVersion,
		ReleaseDir:  fixture.candidate.releaseDir,
		BackendExe:  filepath.Join(fixture.candidate.releaseDir, filepath.FromSlash(releaseBackendPath)),
		RedisExe:    filepath.Join(fixture.candidate.releaseDir, filepath.FromSlash(releaseRedisPath)),
		MediaExe:    filepath.Join(fixture.candidate.releaseDir, filepath.FromSlash(releaseMediaPath)),
		WebDir:      filepath.Join(fixture.candidate.releaseDir, "web"),
		ResourceDir: filepath.Join(fixture.candidate.releaseDir, "resource"),
	}
	// Keep this check independent of candidate manifest validity: bad-candidate
	// deliberately corrupts one candidate image before the launcher call.
	require.NoError(t, backupComponentsStopped(old, candidate))
}

func assertUpgradeDriverOutcome(t *testing.T, fixture maintenanceBackendTestFixture, mode string) {
	t.Helper()
	selected, err := LoadRelease(fixture.paths.InstallDir)
	require.NoError(t, err)
	switch mode {
	case "complete":
		require.NoError(t, CheckMaintenanceGate(fixture.paths.InstallDir))
		require.Equal(t, fixture.journal.CandidateVersion, selected.Version)
	case "media-failure":
		require.ErrorIs(t, CheckMaintenanceGate(fixture.paths.InstallDir), ErrMaintenanceRequired)
		journal, err := ReadMaintenanceJournal(fixture.paths.InstallDir)
		require.NoError(t, err)
		require.Equal(t, MaintenanceRestoreRequired, journal.Phase)
		require.Equal(t, fixture.journal.OldVersion, selected.Version)
	case "bad-candidate":
		require.NoError(t, CheckMaintenanceGate(fixture.paths.InstallDir))
		require.Equal(t, fixture.journal.OldVersion, selected.Version)
	default:
		t.Fatalf("unknown upgrade driver case %q", mode)
	}
}
