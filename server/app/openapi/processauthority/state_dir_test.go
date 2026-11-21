package processauthority

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrepareStateDirCreatesOnlyTheDefault(t *testing.T) {
	t.Setenv("UVP_PROCESS_AUTHORITY_DIR", "")
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("XDG_STATE_HOME", configDir)
	t.Setenv("LOCALAPPDATA", configDir)
	t.Setenv("HOME", configDir)

	defaultPath, err := DefaultStateDir()
	require.NoError(t, err)
	path, err := PrepareStateDir("")
	require.NoError(t, err)
	require.DirExists(t, path)
	require.Equal(t, defaultPath, path)
	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(path)
		require.NoError(t, statErr)
		require.Equal(t, os.FileMode(0700), info.Mode().Perm())
	}
}

func TestPrepareStateDirDoesNotCreateExplicitPath(t *testing.T) {
	t.Setenv("UVP_PROCESS_AUTHORITY_DIR", filepath.Join(t.TempDir(), "deployment-path"))
	path := filepath.Join(t.TempDir(), "not-created")
	resolved, err := PrepareStateDir(path)
	require.NoError(t, err)
	require.Equal(t, path, resolved)
	_, err = os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestPrepareStateDirCreatesDeploymentPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deployment-path")
	t.Setenv("UVP_PROCESS_AUTHORITY_DIR", path)
	resolved, err := PrepareStateDir("")
	require.NoError(t, err)
	require.Equal(t, path, resolved)
	require.DirExists(t, path)
}
