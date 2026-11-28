//go:build !windows

package upgrade

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSyncStandaloneControlUpdatesStableEntryAfterLegacyUpgrade(t *testing.T) {
	root := t.TempDir()
	release := filepath.Join(root, "releases", "task-1")
	require.NoError(t, os.MkdirAll(release, 0755))
	destination := filepath.Join(root, "uvp-gb28181-ctl.sh")
	candidate := filepath.Join(release, "uvp-gb28181-ctl.sh")
	require.NoError(t, os.WriteFile(destination, []byte("legacy control"), 0755))
	require.NoError(t, os.WriteFile(candidate, []byte("#!/bin/bash\n# stop-for-upgrade\n"), 0755))
	require.NoError(t, os.Symlink(release, filepath.Join(root, "current")))
	layout := RegisteredLayout{Root: root, Control: destination}
	changed, err := SyncStandaloneControl(layout)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "stop-for-upgrade", standaloneUpgradeStopAction(layout))
	changed, err = SyncStandaloneControl(layout)
	require.NoError(t, err)
	require.False(t, changed)
	// Administrator-provided custom control commands are never overwritten.
	custom := filepath.Join(root, "custom-control.sh")
	require.NoError(t, os.WriteFile(custom, []byte("custom"), 0755))
	layout.Control = custom
	changed, err = SyncStandaloneControl(layout)
	require.NoError(t, err)
	require.False(t, changed)
	data, err := os.ReadFile(custom)
	require.NoError(t, err)
	require.Equal(t, "custom", string(data))
}

func TestSyncStandaloneControlRejectsEscapingOrSymlinkedPayload(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "uvp-gb28181-ctl.sh")
	layout := RegisteredLayout{Root: root, Control: destination}
	external := t.TempDir()
	require.NoError(t, os.Symlink(external, filepath.Join(root, "current")))
	_, err := SyncStandaloneControl(layout)
	require.Error(t, err)
	require.NoFileExists(t, destination)
	require.NoError(t, os.Remove(filepath.Join(root, "current")))
	release := filepath.Join(root, "releases", "task-1")
	require.NoError(t, os.MkdirAll(release, 0755))
	require.NoError(t, os.Symlink(release, filepath.Join(root, "current")))
	outside := filepath.Join(external, "control.sh")
	require.NoError(t, os.WriteFile(outside, []byte("# stop-for-upgrade"), 0755))
	require.NoError(t, os.Symlink(outside, filepath.Join(release, "uvp-gb28181-ctl.sh")))
	_, err = SyncStandaloneControl(layout)
	require.Error(t, err)
	require.NoFileExists(t, destination)
}
