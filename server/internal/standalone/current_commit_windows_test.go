//go:build windows

package standalone

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestWindowsCommitMaintenanceCurrentRetriesAfterPointerHandleRelease(t *testing.T) {
	current, _, journal := newCurrentCommitTestFixture(t)
	currentPath := filepath.Join(current.installDir, "current.json")
	before, err := os.ReadFile(currentPath)
	require.NoError(t, err)
	reader := openWindowsSecureReadHandle(t, currentPath, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE)

	err = commitMaintenanceCurrent(current.installDir, journal.OperationID)

	require.ErrorContains(t, err, "replace current pointer")
	after, readErr := os.ReadFile(currentPath)
	require.NoError(t, readErr)
	require.Equal(t, before, after)
	entries, readErr := os.ReadDir(current.installDir)
	require.NoError(t, readErr)
	for _, entry := range entries {
		require.False(t, strings.HasPrefix(entry.Name(), ".uvp-current-"), "temporary file remains: %s", entry.Name())
	}
	_, readErr = ReadMaintenanceJournal(current.installDir)
	require.NoError(t, readErr)
	require.ErrorIs(t, CheckMaintenanceGate(current.installDir), ErrMaintenanceRequired)
	require.NoError(t, reader.Close())

	require.NoError(t, commitMaintenanceCurrent(current.installDir, journal.OperationID))
	require.Equal(t, "2.0.0-win10", mustLoadCurrentVersion(t, current.installDir))
}
