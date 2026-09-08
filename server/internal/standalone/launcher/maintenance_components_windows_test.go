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

func TestWindowsMaintenanceCandidateComponents(t *testing.T) {
	fixture := newMaintenanceChildFixture(t)
	mode := os.Getenv("UVP_MAINTENANCE_COMPONENT_CASE")
	require.Contains(t, []string{"complete", "media-failure", "cancel"}, mode)
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	require.Error(t, runMaintenanceCandidateHealth(ctx, fixture.paths, strings.Repeat("b", 64), fixture.journal.CandidateVersion))
	for _, purpose := range []string{"bootstrap_db", "migrate_up"} {
		require.NoError(t, runMaintenanceChild(ctx, fixture.paths, fixture.journal.OperationID, purpose, fixture.journal.CandidateVersion))
	}
	config, err := standalone.LoadConfig(fixture.paths)
	require.NoError(t, err)
	before, err := os.ReadFile(fixture.paths.ConfigFile)
	require.NoError(t, err)
	if mode == "media-failure" {
		raw, err := os.ReadFile(config.ZLMConfigPath)
		require.NoError(t, err)
		lines := strings.Split(string(raw), "\n")
		found := false
		for i, line := range lines {
			if strings.HasPrefix(line, "listen_ip=") {
				lines[i], found = "listen_ip=203.0.113.200", true
			}
		}
		require.True(t, found)
		require.NoError(t, os.WriteFile(config.ZLMConfigPath, []byte(strings.Join(lines, "\n")), 0600))
	}
	if mode == "cancel" {
		done := make(chan error, 1)
		go func() {
			done <- runMaintenanceCandidateHealth(ctx, fixture.paths, fixture.journal.OperationID, fixture.journal.CandidateVersion)
		}()
		require.Eventually(t, func() bool {
			probe, stop := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer stop()
			return checkRedis(probe, config.RedisAddress(), config.RedisPassword()) == nil
		}, 10*time.Second, time.Millisecond)
		cancel()
		select {
		case err := <-done:
			require.Error(t, err)
		case <-time.After(10 * time.Second):
			t.Fatal("candidate cancellation did not clean up")
		}
	} else {
		err = runMaintenanceCandidateHealth(ctx, fixture.paths, fixture.journal.OperationID, fixture.journal.CandidateVersion)
		if mode == "complete" {
			require.NoError(t, err)
			requireMaintenanceChildPermitMissing(t, fixture.paths.InstallDir)
		} else {
			require.Error(t, err)
		}
	}
	require.NoError(t, checkPorts([]string{config.RedisAddress()}, config.MediaListeners()))
	after, err := os.ReadFile(fixture.paths.ConfigFile)
	require.NoError(t, err)
	require.Equal(t, sha256.Sum256(before), sha256.Sum256(after))
	require.ErrorIs(t, standalone.CheckMaintenanceGate(fixture.paths.InstallDir), standalone.ErrMaintenanceRequired)
	release, err := standalone.LoadRelease(fixture.paths.InstallDir)
	require.NoError(t, err)
	require.Equal(t, fixture.journal.OldVersion, release.Version)
	if mode == "complete" {
		candidate, err := standalone.LoadReleaseVersion(fixture.paths.InstallDir, fixture.journal.CandidateVersion)
		require.NoError(t, err)
		_, err = os.Stat(filepath.Join(candidate.ReleaseDir, "media", "log"))
		require.ErrorIs(t, err, os.ErrNotExist, "media logs must not modify the immutable release directory")
	}
}
