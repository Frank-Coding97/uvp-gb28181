package standalone

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBackupOwnedRetainsTransactionInstanceLock(t *testing.T) {
	paths := newBackupTestPaths(t)
	lock, err := AcquireInstanceLock(paths.InstallDir)
	require.NoError(t, err)
	defer lock.Close()
	destination := filepath.Join(filepath.Dir(paths.InstallDir), "owned-backup")
	_, err = backupStoppedOwned(context.Background(), paths, destination)
	require.NoError(t, err)
	_, err = VerifyBackup(context.Background(), destination)
	require.NoError(t, err)
	other, err := AcquireInstanceLock(paths.InstallDir)
	require.ErrorIs(t, err, ErrInstanceRunning)
	require.Nil(t, other)
}

func TestBackupOwnedRequiresInstanceOwnership(t *testing.T) {
	paths := newBackupTestPaths(t)
	destination := filepath.Join(filepath.Dir(paths.InstallDir), "unowned-backup")
	_, err := backupStoppedOwned(context.Background(), paths, destination)
	require.Error(t, err)
	_, err = os.Stat(destination)
	require.ErrorIs(t, err, os.ErrNotExist)
}
