//go:build windows

package launcher

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

func TestWindowsAbnormalRestartCannotOpenBusiness(t *testing.T) {
	root := os.Getenv("UVP_MAINTENANCE_TEST_ROOT")
	if root == "" {
		t.Skip("requires isolated abnormal restart fixture")
	}
	marker := filepath.Join(root, "data", ".uvp-running.json")
	before, err := os.ReadFile(marker)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	opened := false
	err = LaunchWithBrowser(ctx, root, "", func(status Status) {
		if status.State == Ready {
			opened = true
			cancel()
		}
	}, nil)
	require.False(t, opened, "unclean history reached Ready without credential recovery")
	require.ErrorIs(t, err, standalone.ErrUncleanRecoveryRequired)
	after, err := os.ReadFile(marker)
	require.NoError(t, err, "unclean evidence must be preserved")
	require.Equal(t, sha256.Sum256(before), sha256.Sum256(after), "admission replaced previous lifetime evidence")
}
