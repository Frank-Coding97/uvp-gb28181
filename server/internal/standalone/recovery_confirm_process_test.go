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

func TestRecoveryConfirmationArchiveProcessCrash(t *testing.T) {
	executable, err := os.Executable()
	require.NoError(t, err)
	for _, stage := range []string{"before-rename", "after-rename"} {
		t.Run(stage, func(t *testing.T) {
			root := t.TempDir()
			ready := filepath.Join(root, "ready.json")
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestRecoveryConfirmationArchiveCrashHelper$", "-test.timeout=2m")
			for _, entry := range os.Environ() {
				key, _, _ := strings.Cut(entry, "=")
				switch strings.ToUpper(key) {
				case "TMP", "TEMP", "TMPDIR", "UVP_RECOVERY_CONFIRM_READY", "UVP_RECOVERY_CONFIRM_STAGE":
					continue
				}
				cmd.Env = append(cmd.Env, entry)
			}
			cmd.Env = append(cmd.Env, "TMP="+root, "TEMP="+root, "TMPDIR="+root, "UVP_RECOVERY_CONFIRM_READY="+ready, "UVP_RECOVERY_CONFIRM_STAGE="+stage)
			require.NoError(t, cmd.Start())
			defer func() {
				if cmd.ProcessState == nil {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
			}()
			var fixture recoveryPublicationProcessReady
			require.Eventually(t, func() bool {
				raw, err := os.ReadFile(ready)
				return err == nil && json.Unmarshal(raw, &fixture) == nil && fixture.Root != "" && fixture.Operation != ""
			}, 10*time.Second, 5*time.Millisecond)
			relative, err := filepath.Rel(root, fixture.Root)
			require.NoError(t, err)
			require.NotEqual(t, "..", relative)
			require.False(t, strings.HasPrefix(relative, ".."+string(filepath.Separator)))
			_, err = AcquireInstanceLock(fixture.Root)
			require.ErrorIs(t, err, ErrInstanceRunning)
			require.NoError(t, cmd.Process.Kill())
			require.Error(t, cmd.Wait())
			lock, err := AcquireInstanceLock(fixture.Root)
			require.NoError(t, err)
			defer lock.Close()
			if stage == "before-rename" {
				require.ErrorIs(t, CheckMaintenanceGate(fixture.Root), ErrMaintenanceRequired)
				require.NoError(t, archiveRecoveryConfirmation(context.Background(), fixture.Root, fixture.Operation, fixture.Trust, 1, strings.Repeat("a", 64), func() error { return nil }, backupPublish))
			}
			require.NoError(t, CheckMaintenanceGate(fixture.Root))
			archive := filepath.Join(fixture.Root, ".uvp-recovered-"+fixture.Operation)
			_, err = readSecureConfigFile(filepath.Join(archive, "confirmation.json"))
			require.NoError(t, err)
			for _, name := range []string{"config", "data"} {
				_, err = recoveryTreeIdentity(context.Background(), filepath.Join(archive, "failed", fixture.Operation, name))
				require.NoError(t, err)
			}
		})
	}
}

func TestRecoveryConfirmationArchiveCrashHelper(t *testing.T) {
	ready, stage := os.Getenv("UVP_RECOVERY_CONFIRM_READY"), os.Getenv("UVP_RECOVERY_CONFIRM_STAGE")
	if ready == "" || stage == "" {
		t.Skip("requires isolated recovery confirmation crash driver")
	}
	fixture := newRecoveryPublicationFixture(t)
	root, op := fixture.paths.InstallDir, fixture.journal.OperationID
	require.NoError(t, publishRecoveryDirectories(context.Background(), root, op, fixture.trust, nil))
	require.NoError(t, restoreRecoveryCurrent(context.Background(), root, op, fixture.trust, nil))
	require.NoError(t, advanceMaintenance(root, op, MaintenanceRestoring, MaintenanceAwaitingConfirmation))
	raw, err := json.Marshal(recoveryPublicationProcessReady{Root: root, Operation: op, Trust: fixture.trust})
	require.NoError(t, err)
	// This test isolates the filesystem commit after authentication; it does
	// not substitute for real administrator or old-token acceptance tests.
	err = archiveRecoveryConfirmation(context.Background(), root, op, fixture.trust, 1, strings.Repeat("a", 64), func() error { return nil }, func(source, target string) error {
		if stage == "after-rename" {
			require.NoError(t, backupPublish(source, target))
		}
		require.NoError(t, os.WriteFile(ready, raw, 0600))
		time.Sleep(time.Hour)
		return nil
	})
	t.Fatalf("helper returned before termination: %v", err)
}
