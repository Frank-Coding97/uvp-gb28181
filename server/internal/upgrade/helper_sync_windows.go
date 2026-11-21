//go:build windows

package upgrade

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SyncHelper reports the staged helper on Windows instead of restarting it.
//
// Windows locks a running image, so the swap has to happen while the helper is
// down. The control script already does exactly that at the top of its
// Start-Updater (Install-UpdaterImage), which runs on every service start — the
// next restart therefore publishes the new helper with no extra risk here.
func (a *WindowsStandaloneAdapter) SyncHelper(ctx context.Context, record TaskRecord) (HelperSyncResult, error) {
	if err := a.Layout.validate(); err != nil {
		return HelperSyncResult{}, err
	}
	staged := filepath.Join(a.Layout.Root, HelperEntryDir, HelperWindowsImage+HelperStagedSuffix)
	if _, err := os.Lstat(staged); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return HelperSyncResult{Reason: "release did not ship a newer helper"}, nil
		}
		return HelperSyncResult{}, fmt.Errorf("inspect staged helper: %w", err)
	}
	if strings.TrimSpace(a.Layout.Control) == "" {
		return HelperSyncResult{}, errors.New("standalone control path is not registered")
	}
	return HelperSyncResult{Staged: true, Reason: "control script swaps the helper on the next start"}, nil
}
