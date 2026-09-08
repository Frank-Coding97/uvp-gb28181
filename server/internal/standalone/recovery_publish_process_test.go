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

type recoveryPublicationProcessReady struct {
	Root      string `json:"root"`
	Operation string `json:"op"`
	Trust     string `json:"trust"`
}

func TestRecoveryPublicationProcessCrash(t *testing.T) {
	executable, err := os.Executable()
	require.NoError(t, err)
	for _, stage := range []string{"config_isolated", "config_published", "data_isolated", "data_published"} {
		t.Run(stage, func(t *testing.T) {
			// The parent owns this directory; the killed child never runs testing cleanup.
			root := t.TempDir()
			ready := filepath.Join(root, "ready.json")
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestRecoveryPublicationCrashHelper$", "-test.timeout=2m")
			for _, entry := range os.Environ() {
				key, _, _ := strings.Cut(entry, "=")
				switch strings.ToUpper(key) {
				case "TMP", "TEMP", "TMPDIR", "UVP_RECOVERY_PUBLISH_READY", "UVP_RECOVERY_PUBLISH_STAGE":
					continue
				}
				cmd.Env = append(cmd.Env, entry)
			}
			cmd.Env = append(cmd.Env,
				"TMP="+root,
				"TEMP="+root,
				"TMPDIR="+root,
				"UVP_RECOVERY_PUBLISH_READY="+ready,
				"UVP_RECOVERY_PUBLISH_STAGE="+stage,
			)
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
				return err == nil && json.Unmarshal(raw, &fixture) == nil && fixture.Root != "" && fixture.Operation != "" && fixture.Trust != ""
			}, 10*time.Second, 5*time.Millisecond)
			relative, err := filepath.Rel(root, fixture.Root)
			require.NoError(t, err)
			require.NotEqual(t, "..", relative)
			require.False(t, strings.HasPrefix(relative, ".."+string(filepath.Separator)))
			_, err = AcquireInstanceLock(fixture.Root)
			require.ErrorIs(t, err, ErrInstanceRunning)
			before, err := readRecoveryJournal(fixture.Root)
			require.NoError(t, err)
			require.Equal(t, map[string]string{
				"config_isolated":  "staged",
				"config_published": "config_isolated",
				"data_isolated":    "config_published",
				"data_published":   "data_isolated",
			}[stage], before.Phase)

			require.NoError(t, cmd.Process.Kill())
			require.Error(t, cmd.Wait())
			require.ErrorIs(t, CheckMaintenanceGate(fixture.Root), ErrMaintenanceRequired)

			lock, err := AcquireInstanceLock(fixture.Root)
			require.NoError(t, err)
			defer lock.Close()
			require.NoError(t, publishRecoveryDirectories(context.Background(), fixture.Root, fixture.Operation, fixture.Trust, nil))
			require.NoError(t, lock.Close())

			final, err := readRecoveryJournal(fixture.Root)
			require.NoError(t, err)
			require.Equal(t, "data_published", final.Phase)
			require.Equal(t, fixture.Operation, final.OperationID)
			failed := filepath.Join(fixture.Root, maintenanceDirName, "failed", fixture.Operation)
			for name, digest := range map[string]string{"config": final.FailedConfigSHA256, "data": final.FailedDataSHA256} {
				actual, err := recoveryTreeIdentity(context.Background(), filepath.Join(failed, name))
				require.NoError(t, err)
				require.Equal(t, digest, actual)
			}
			for name, digest := range map[string]string{"config": final.StagedConfigSHA256, "data": final.StagedDataSHA256} {
				actual, err := recoveryTreeIdentity(context.Background(), filepath.Join(fixture.Root, name))
				require.NoError(t, err)
				require.Equal(t, digest, actual)
			}
			entries, err := os.ReadDir(failed)
			require.NoError(t, err)
			require.Len(t, entries, 2)
			require.ErrorIs(t, CheckMaintenanceGate(fixture.Root), ErrMaintenanceRequired)
			current, err := releaseFileSHA256(filepath.Join(fixture.Root, "current.json"))
			require.NoError(t, err)
			require.Equal(t, final.OldCurrentSHA256, current)
		})
	}
}

func TestRecoveryPublicationCrashHelper(t *testing.T) {
	ready := os.Getenv("UVP_RECOVERY_PUBLISH_READY")
	stage := os.Getenv("UVP_RECOVERY_PUBLISH_STAGE")
	if ready == "" || stage == "" {
		t.Skip("requires isolated recovery publication crash driver")
	}
	fixture := newRecoveryPublicationFixture(t)
	readyPayload, err := json.Marshal(recoveryPublicationProcessReady{
		Root: fixture.paths.InstallDir, Operation: fixture.journal.OperationID, Trust: fixture.trust,
	})
	require.NoError(t, err)
	err = publishRecoveryDirectories(context.Background(), fixture.paths.InstallDir, fixture.journal.OperationID, fixture.trust, func(phase string) error {
		if phase != stage {
			return nil
		}
		if err := os.WriteFile(ready, readyPayload, 0600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Hour) // Parent terminates this actual owner after the rename.
		return nil
	})
	t.Fatalf("crash helper returned before parent termination: %v", err)
}
