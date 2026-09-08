package standalone

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type upgradeCrashFixture struct {
	Paths       Paths
	OperationID string
	Trust       string
}

func TestUpgradeCompletionProcessCrash(t *testing.T) {
	executable, err := os.Executable()
	require.NoError(t, err)
	for _, mode := range []string{"before-proof", "after-proof", "after-archive"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			ready := filepath.Join(root, "ready.json")
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestUpgradeCompletionCrashHelper$", "-test.timeout=2m")
			for _, entry := range os.Environ() {
				key, _, _ := strings.Cut(entry, "=")
				switch strings.ToUpper(key) {
				case "TMP", "TEMP", "TMPDIR", "UVP_UPGRADE_CRASH_READY", "UVP_UPGRADE_CRASH_STAGE":
					continue
				}
				cmd.Env = append(cmd.Env, entry)
			}
			cmd.Env = append(cmd.Env, "TMP="+root, "TEMP="+root, "TMPDIR="+root, "UVP_UPGRADE_CRASH_READY="+ready, "UVP_UPGRADE_CRASH_STAGE="+mode)
			require.NoError(t, cmd.Start())
			defer func() {
				if cmd.ProcessState == nil {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
			}()
			var fixture upgradeCrashFixture
			require.Eventually(t, func() bool {
				raw, err := os.ReadFile(ready)
				return err == nil && json.Unmarshal(raw, &fixture) == nil
			}, 10*time.Second, 5*time.Millisecond)
			_, err := AcquireInstanceLock(fixture.Paths.InstallDir)
			require.ErrorIs(t, err, ErrInstanceRunning)
			require.NoError(t, cmd.Process.Kill())
			require.Error(t, cmd.Wait())
			if mode == "after-archive" {
				require.NoError(t, CheckMaintenanceGate(fixture.Paths.InstallDir))
				archive, err := readMaintenanceJournalFile(filepath.Join(fixture.Paths.InstallDir, ".uvp-completed-"+fixture.OperationID, "journal.json"))
				require.NoError(t, err)
				require.Equal(t, MaintenanceCompletionReady, archive.Phase)
			} else {
				require.ErrorIs(t, CheckMaintenanceGate(fixture.Paths.InstallDir), ErrMaintenanceRequired)
				err := resumeUpgradeCompletionWithTrust(context.Background(), fixture.Paths, fixture.OperationID, fixture.Trust)
				if mode == "after-proof" {
					require.NoError(t, err)
					require.NoError(t, CheckMaintenanceGate(fixture.Paths.InstallDir))
				} else {
					require.Error(t, err)
					require.ErrorIs(t, CheckMaintenanceGate(fixture.Paths.InstallDir), ErrMaintenanceRequired)
				}
			}
			lock, err := AcquireInstanceLock(fixture.Paths.InstallDir)
			require.NoError(t, err)
			require.NoError(t, lock.Close())
		})
	}
}

func TestUpgradeCompletionCrashHelper(t *testing.T) {
	ready := os.Getenv("UVP_UPGRADE_CRASH_READY")
	if ready == "" {
		t.Skip("requires isolated crash driver")
	}
	owner := completedUpgradeFixture(t)
	switch os.Getenv("UVP_UPGRADE_CRASH_STAGE") {
	case "before-proof":
	case "after-proof":
		require.NoError(t, owner.markCompletionReady(context.Background()))
	case "after-archive":
		require.NoError(t, owner.finish(context.Background()))
	default:
		t.Fatal("unknown crash stage")
	}
	raw, err := json.Marshal(upgradeCrashFixture{owner.paths, owner.journal.OperationID, owner.trust})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(ready, raw, 0600))
	time.Sleep(time.Hour) // Parent kills this actual owner before deferred cleanup.
	t.Fatal("crash helper was not terminated")
}
