package standalone

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRecoveryTreeIdentityBindsWholeDirectory(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "state")
	require.NoError(t, os.WriteFile(file, []byte("before"), 0600))
	before, err := recoveryTreeIdentity(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, before, 64)
	require.NoError(t, os.Chtimes(file, time.Unix(1, 0), time.Unix(1, 0)))
	unchanged, err := recoveryTreeIdentity(context.Background(), root)
	require.NoError(t, err)
	require.Equal(t, before, unchanged)
	require.NoError(t, os.WriteFile(file, []byte("after!"), 0600))
	changed, err := recoveryTreeIdentity(context.Background(), root)
	require.NoError(t, err)
	require.NotEqual(t, before, changed)
	require.NoError(t, os.WriteFile(file, []byte("before"), 0600))
	require.NoError(t, os.Mkdir(filepath.Join(root, "empty"), 0700))
	changed, err = recoveryTreeIdentity(context.Background(), root)
	require.NoError(t, err)
	require.NotEqual(t, before, changed)
	require.NoError(t, os.Remove(filepath.Join(root, "empty")))
	require.NoError(t, os.Rename(file, filepath.Join(root, "renamed")))
	changed, err = recoveryTreeIdentity(context.Background(), root)
	require.NoError(t, err)
	require.NotEqual(t, before, changed)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = recoveryTreeIdentity(ctx, root)
	require.ErrorIs(t, err, context.Canceled)
	_, err = recoveryTreeIdentity(context.Background(), filepath.Join(root, "missing"))
	require.Error(t, err)
	_, err = recoveryTreeIdentity(context.Background(), filepath.Join(root, "renamed"))
	require.Error(t, err)
}
