//go:build windows

package launcher

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

const (
	recoveryCorruptionTestRootEnv = "UVP_RECOVERY_CORRUPTION_TEST_ROOT"
	recoveryCorruptionTestCaseEnv = "UVP_RECOVERY_CORRUPTION_TEST_CASE"
)

func TestWindowsRecoveryCorruptionLauncher(t *testing.T) {
	root := strings.TrimSpace(os.Getenv(recoveryCorruptionTestRootEnv))
	if root == "" {
		t.Skip("requires an isolated recovery corruption fixture")
	}
	mode := strings.TrimSpace(os.Getenv(recoveryCorruptionTestCaseEnv))
	if mode != "sqlite" && mode != "config" {
		t.Fatalf("unknown recovery corruption case %q", mode)
	}

	release, err := standalone.LoadRelease(root)
	require.NoError(t, err)
	paths, err := standalone.ResolvePaths(standalone.PathOptions{
		InstallDir: root, ConfigDir: filepath.Join(root, "config"), DataDir: filepath.Join(root, "data"),
		ResourceDir: release.ResourceDir, WebDir: release.WebDir, RecordingsDir: filepath.Join(root, "recordings"),
	})
	require.NoError(t, err)
	config, err := standalone.LoadConfig(paths)
	require.NoError(t, err)
	beforeDB, err := os.ReadFile(paths.DatabasePath)
	require.NoError(t, err)
	beforeConfig, err := os.ReadFile(paths.ConfigFile)
	require.NoError(t, err)

	damagedDB := append([]byte(nil), beforeDB...)
	damagedConfig := append([]byte(nil), beforeConfig...)
	if mode == "sqlite" {
		const damagedHeader = "UVP_CORRUPT_DB!!"
		require.GreaterOrEqual(t, len(damagedDB), len(damagedHeader))
		copy(damagedDB[:len(damagedHeader)], damagedHeader)
		require.NotEqual(t, sha256.Sum256(beforeDB), sha256.Sum256(damagedDB))
		require.NoError(t, writeRecoveryCorruptionFile(paths.DatabasePath, damagedDB))
	} else {
		require.Greater(t, len(damagedConfig), 2)
		damagedConfig = append([]byte(nil), damagedConfig[:len(damagedConfig)/2]...)
		require.NotEmpty(t, damagedConfig)
		require.NotEqual(t, sha256.Sum256(beforeConfig), sha256.Sum256(damagedConfig))
		require.NoError(t, writeRecoveryCorruptionFile(paths.ConfigFile, damagedConfig))
		if _, err := standalone.LoadConfig(paths); err == nil {
			t.Fatal("half-written configuration unexpectedly remained valid")
		}
	}

	var (
		statusMu    sync.Mutex
		statuses    []Status
		browserOpen bool
	)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	err = LaunchWithBrowser(ctx, root, paths.RecordingsDir, func(status Status) {
		statusMu.Lock()
		statuses = append(statuses, status)
		statusMu.Unlock()
	}, func(string) {
		statusMu.Lock()
		browserOpen = true
		statusMu.Unlock()
	})
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatal("corrupted installation startup timed out")
	}
	require.Error(t, err)

	statusMu.Lock()
	observed := append([]Status(nil), statuses...)
	opened := browserOpen
	statusMu.Unlock()
	for _, status := range observed {
		if status.State == Ready {
			t.Fatalf("corrupted installation published Ready: %+v", status)
		}
	}
	require.False(t, opened)

	afterDB, err := os.ReadFile(paths.DatabasePath)
	require.NoError(t, err)
	afterConfig, err := os.ReadFile(paths.ConfigFile)
	require.NoError(t, err)
	if mode == "sqlite" {
		require.Equal(t, sha256.Sum256(damagedDB), sha256.Sum256(afterDB), "corrupted SQLite bytes were rewritten")
		require.Equal(t, sha256.Sum256(beforeConfig), sha256.Sum256(afterConfig), "unrelated configuration changed")
	} else {
		require.Equal(t, sha256.Sum256(damagedConfig), sha256.Sum256(afterConfig), "truncated configuration bytes were rewritten")
		require.Equal(t, sha256.Sum256(beforeDB), sha256.Sum256(afterDB), "corrupted configuration caused database replacement")
	}
	// The preloaded valid config is used only to check that every normal-start
	// component port was released after the launcher returned an error.
	require.NoError(t, checkPorts([]string{config.RedisAddress(), config.BackendAddress()}, config.MediaListeners()))
}

func writeRecoveryCorruptionFile(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, info.Mode().Perm())
}
