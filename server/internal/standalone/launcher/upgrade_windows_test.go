//go:build windows

package launcher

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

func TestWindowsUpgradeStoppedTransaction(t *testing.T) {
	root := os.Getenv(maintenanceChildTestRootEnv)
	if root == "" {
		t.Skip("requires isolated upgrade driver")
	}
	mode := os.Getenv("UVP_UPGRADE_TEST_CASE")
	require.Contains(t, []string{"complete", "media-failure", "bad-candidate"}, mode)
	current, err := standalone.LoadRelease(root)
	require.NoError(t, err)
	candidate, err := standalone.LoadReleaseVersion(root, "2.0.0-win10")
	require.NoError(t, err)
	paths, err := standalone.ResolvePaths(standalone.PathOptions{
		InstallDir: root, ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"),
		ResourceDir: current.ResourceDir, WebDir: current.WebDir, RecordingsDir: filepath.Join(root, "recordings"),
	})
	require.NoError(t, err)
	config, err := standalone.LoadConfig(paths)
	require.NoError(t, err)
	authority, err := os.ReadFile(paths.ConfigFile)
	require.NoError(t, err)
	if mode == "media-failure" {
		raw, err := os.ReadFile(config.ZLMConfigPath)
		require.NoError(t, err)
		require.Contains(t, string(raw), "listen_ip=127.0.0.1")
		require.NoError(t, os.WriteFile(config.ZLMConfigPath, []byte(strings.Replace(string(raw), "listen_ip=127.0.0.1", "listen_ip=203.0.113.200", 1)), 0600))
	}
	if mode == "bad-candidate" {
		require.NoError(t, os.WriteFile(candidate.BackendExe, []byte("corrupted candidate"), 0700))
	}
	destination := filepath.Join(filepath.Dir(root), "upgrade-transaction-backup")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	err = UpgradeStopped(ctx, root, paths.RecordingsDir, candidate.Version, destination)
	if mode == "complete" {
		require.NoError(t, err)
		require.NoError(t, standalone.CheckMaintenanceGate(root))
		archives, err := filepath.Glob(filepath.Join(root, ".uvp-completed-*", "journal.json"))
		require.NoError(t, err)
		require.Len(t, archives, 1)
	} else {
		require.Error(t, err)
		if mode == "media-failure" {
			journal, err := standalone.ReadMaintenanceJournal(root)
			require.NoError(t, err)
			require.Equal(t, standalone.MaintenanceRestoreRequired, journal.Phase)
		} else {
			require.NoError(t, standalone.CheckMaintenanceGate(root))
			_, err := os.Stat(destination)
			require.ErrorIs(t, err, os.ErrNotExist)
		}
	}
	if mode != "bad-candidate" {
		backup, err := standalone.VerifyBackup(ctx, destination)
		require.NoError(t, err)
		require.Equal(t, current.Version, backup.Version)
	}
	after, err := os.ReadFile(paths.ConfigFile)
	require.NoError(t, err)
	require.Equal(t, sha256.Sum256(authority), sha256.Sum256(after))
	require.NoError(t, checkPorts([]string{config.RedisAddress()}, config.MediaListeners()))
	selected, err := standalone.LoadRelease(root)
	require.NoError(t, err)
	if mode == "complete" {
		require.Equal(t, candidate.Version, selected.Version)
	} else {
		require.Equal(t, current.Version, selected.Version)
	}
}
