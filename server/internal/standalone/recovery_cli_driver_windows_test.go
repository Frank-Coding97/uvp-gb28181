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

// TestWindowsRecoveryCLIDriver exercises the public restore subcommand against
// the same isolated real component fixture as the native coordinator test. The
// launcher executable must be built with the backend SHA allowlist; this test
// deliberately supplies no trust-related argument or environment override.
func TestWindowsRecoveryCLIDriver(t *testing.T) {
	launcherPath := strings.TrimSpace(os.Getenv("UVP_RECOVERY_LAUNCHER_PATH"))
	if launcherPath == "" {
		t.Skip("requires UVP_RECOVERY_LAUNCHER_PATH pointing to a qualified Windows launcher")
	}
	backend := maintenanceBackendTestPath(t)
	fixture := newMaintenanceComponentFixture(t, backend, maintenanceComponentReleaseRoot(t))
	currentBackend := filepath.Join(fixture.current.releaseDir, filepath.FromSlash(releaseBackendPath))
	copyMaintenanceComponentFile(t, backend, currentBackend)
	fixture.current.manifest = rewriteMaintenanceComponentManifest(t, fixture.current.releaseDir, fixture.current.manifest.Version, fixture.current.manifest.SourceCommit)
	seedUpgradeDatabase(t, fixture)
	seedUpgradeRedis(t, fixture)
	removeUpgradeDriverGate(t, fixture.paths.InstallDir)
	require.NoError(t, fixture.lock.Close())

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	digest, err := releaseFileSHA256(backend)
	require.NoError(t, err)
	backup := filepath.Join(filepath.Dir(fixture.paths.InstallDir), "recovery-cli-backup")
	owner, err := prepareUpgradeStoppedWithTrust(ctx, fixture.paths, fixture.candidate.manifest.Version, backup, digest)
	require.NoError(t, err)
	require.Equal(t, MaintenanceUpgrading, owner.journal.Phase)
	require.NoError(t, advanceMaintenance(fixture.paths.InstallDir, owner.journal.OperationID, MaintenanceUpgrading, MaintenanceRestoreRequired))
	require.NoError(t, owner.Close())

	cmd := exec.CommandContext(ctx, launcherPath, "restore", "--install-dir", fixture.paths.InstallDir, "--recordings-dir", fixture.paths.RecordingsDir)
	cmd.Env = recoveryCLIDriverEnvironment()
	output, err := cmd.CombinedOutput()
	if ctxErr := ctx.Err(); ctxErr != nil {
		t.Fatalf("restore CLI timed out: %v", ctxErr)
	}
	require.NoError(t, err)
	redactedOutput := redactMaintenanceComponentOutput(t, fixture.paths, string(output))
	t.Logf("restore CLI output:\n%s", redactedOutput)
	require.Contains(t, redactedOutput, "恢复已完成，等待本机确认："+owner.journal.OperationID)
	require.Contains(t, redactedOutput, "UVP.exe recovery-confirm --operation "+owner.journal.OperationID)

	journal, err := ReadMaintenanceJournal(fixture.paths.InstallDir)
	require.NoError(t, err)
	require.Equal(t, MaintenanceAwaitingConfirmation, journal.Phase)
	require.ErrorIs(t, CheckMaintenanceGate(fixture.paths.InstallDir), ErrMaintenanceRequired)
	selected, err := LoadRelease(fixture.paths.InstallDir)
	require.NoError(t, err)
	require.Equal(t, fixture.journal.OldVersion, selected.Version)
	candidate, err := LoadReleaseVersion(fixture.paths.InstallDir, fixture.journal.CandidateVersion)
	require.NoError(t, err)
	require.NoError(t, backupComponentsStopped(selected, candidate))
	// A pipe must never act as the local administrator console. No real
	// credential is supplied, and refusal must retain the complete gate.
	confirm := exec.CommandContext(ctx, launcherPath, "recovery-confirm", "--install-dir", fixture.paths.InstallDir, "--operation", journal.OperationID)
	confirm.Env = recoveryCLIDriverEnvironment()
	confirm.Stdin = strings.NewReader("fixture-user\nfixture-password\nCONFIRM " + journal.OperationID + "\n")
	confirmationOutput, confirmationErr := confirm.CombinedOutput()
	require.Error(t, confirmationErr)
	require.Contains(t, string(confirmationOutput), "管理员用户名：")
	require.NotContains(t, string(confirmationOutput), "fixture-password")
	require.NotContains(t, string(confirmationOutput), "本机确认已完成")
	require.ErrorIs(t, CheckMaintenanceGate(fixture.paths.InstallDir), ErrMaintenanceRequired)
	_, err = os.Stat(filepath.Join(fixture.paths.InstallDir, ".uvp-recovered-"+journal.OperationID))
	require.True(t, os.IsNotExist(err))
}

func recoveryCLIDriverEnvironment() []string {
	forbidden := map[string]struct{}{
		"UVP_RECOVERY_LAUNCHER_PATH":             {},
		"UVP_RECOVERY_REDIS_BINARY":              {},
		"UVP_MAINTENANCE_BACKEND_PATH":           {},
		"UVP_MAINTENANCE_COMPONENT_RELEASE_ROOT": {},
		"UVP_MAINTENANCE_LAUNCHER_TEST_PATH":     {},
	}
	env := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			upperKey := strings.ToUpper(key)
			if _, skip := forbidden[upperKey]; skip || strings.Contains(upperKey, "TRUST") || strings.Contains(upperKey, "ALLOWLIST") {
				continue
			}
		}
		env = append(env, entry)
	}
	return env
}
