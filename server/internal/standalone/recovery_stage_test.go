package standalone

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecoveryStageIsolatedBuildAndRetry(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "retry"}[fail], func(t *testing.T) {
			owner := completedUpgradeFixture(t)
			root, outer := owner.paths.InstallDir, owner.journal
			require.NoError(t, advanceMaintenance(root, outer.OperationID, MaintenanceCommitting, MaintenanceRestoreRequired))
			beforeConfig, err := recoveryTreeIdentity(context.Background(), owner.paths.ConfigDir)
			require.NoError(t, err)
			beforeData, err := recoveryTreeIdentity(context.Background(), owner.paths.DataDir)
			require.NoError(t, err)
			original, err := LoadConfig(owner.paths)
			require.NoError(t, err)
			var calls []string
			run := func(ctx context.Context, p Paths, op, purpose, version string) error {
				require.NotEqual(t, owner.paths.DataDir, p.DataDir)
				require.Equal(t, outer.OldVersion, version)
				require.Equal(t, outer.OperationID, op)
				calls = append(calls, purpose)
				if fail {
					return errors.New("injected offline failure")
				}
				return nil
			}
			redis := func(ctx context.Context, exe, source, target, control string, indexDB int) error {
				require.NotEqual(t, filepath.Join(outer.BackupRoot, "data", "redis"), source)
				entries, err := os.ReadDir(target)
				require.NoError(t, err)
				require.Empty(t, entries)
				entries, err = os.ReadDir(source)
				require.NoError(t, err)
				require.NotEmpty(t, entries)
				return os.WriteFile(filepath.Join(target, "verified-fixture"), []byte("only new state"), 0600)
			}
			stage, err := prepareRecoveryStage(context.Background(), owner.paths, outer.OperationID, owner.trust, run, redis)
			if fail {
				require.Error(t, err)
				j, err := readRecoveryJournal(root)
				require.NoError(t, err)
				require.Equal(t, "staging", j.Phase)
				fail = false
				calls = nil
				stage, err = prepareRecoveryStage(context.Background(), owner.paths, outer.OperationID, owner.trust, run, redis)
				require.NoError(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, []string{"revoke_sessions", "db_check"}, calls)
			config, err := LoadConfig(stage)
			require.NoError(t, err)
			require.NotEqual(t, original.JWTSecret(), config.JWTSecret())
			require.NotEqual(t, original.InstanceGeneration(), config.InstanceGeneration())
			require.Equal(t, original.RedisPassword(), config.RedisPassword())
			after, err := recoveryTreeIdentity(context.Background(), owner.paths.ConfigDir)
			require.NoError(t, err)
			require.Equal(t, beforeConfig, after)
			after, err = recoveryTreeIdentity(context.Background(), owner.paths.DataDir)
			require.NoError(t, err)
			require.Equal(t, beforeData, after)
			j, err := readRecoveryJournal(root)
			require.NoError(t, err)
			require.Equal(t, "staged", j.Phase)
			require.NoError(t, checkRecoveryDirectoryState(context.Background(), root, j, 1))
			repeated, err := prepareRecoveryStage(context.Background(), owner.paths, outer.OperationID, owner.trust,
				func(context.Context, Paths, string, string, string) error {
					t.Fatal("sealed stage reran backend")
					return nil
				},
				func(context.Context, string, string, string, string, int) error {
					t.Fatal("sealed stage reran Redis")
					return nil
				})
			require.NoError(t, err)
			again, err := LoadConfig(repeated)
			require.NoError(t, err)
			require.Equal(t, config.ConfigSHA256, again.ConfigSHA256)
			require.ErrorIs(t, CheckMaintenanceGate(root), ErrMaintenanceRequired)
		})
	}
}
