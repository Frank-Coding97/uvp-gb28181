//go:build windows

package standalone

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWindowsBackupRejectsExternalDatabaseHandle(t *testing.T) {
	paths := newBackupTestPaths(t)
	before, err := os.ReadFile(paths.DatabasePath)
	require.NoError(t, err)
	held, err := os.Open(paths.DatabasePath)
	require.NoError(t, err)
	destination := filepath.Join(filepath.Dir(paths.InstallDir), "external-handle-backup")
	_, err = BackupStopped(context.Background(), paths, destination)
	held.Close()
	require.Error(t, err)
	require.ErrorContains(t, err, "exclusively")
	_, err = os.Stat(destination)
	require.ErrorIs(t, err, os.ErrNotExist)
	after, err := os.ReadFile(paths.DatabasePath)
	require.NoError(t, err)
	require.Equal(t, sha256.Sum256(before), sha256.Sum256(after))
}

func TestWindowsBackupPreservesCommittedWAL(t *testing.T) {
	paths := newBackupTestPaths(t)
	executable, err := os.Executable()
	require.NoError(t, err)
	child := exec.Command(executable, "-test.run=^TestWindowsBackupWALWriterHelper$")
	child.Env = append(os.Environ(), "UVP_BACKUP_WAL_WRITER="+paths.DatabasePath)
	output, err := child.CombinedOutput()
	require.NoError(t, err, string(output))
	before := make(map[string][32]byte)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		raw, err := os.ReadFile(paths.DatabasePath + suffix)
		require.NoError(t, err)
		require.NotEmpty(t, raw)
		before[suffix] = sha256.Sum256(raw)
	}
	destination := filepath.Join(filepath.Dir(paths.InstallDir), "wal-backup")
	manifest, err := BackupStopped(context.Background(), paths, destination)
	require.NoError(t, err)
	require.Contains(t, manifest.SQLiteFiles, "data/uvp.db-wal")
	for suffix, sum := range before {
		raw, err := os.ReadFile(paths.DatabasePath + suffix)
		require.NoError(t, err)
		require.Equal(t, sum, sha256.Sum256(raw), "source SQLite file changed")
	}
	db, err := sql.Open("sqlite", filepath.Join(destination, "data", "uvp.db"))
	require.NoError(t, err)
	defer db.Close()
	var value string
	require.NoError(t, db.QueryRow("SELECT value FROM backup_wal_fixture WHERE id=1").Scan(&value))
	require.Equal(t, "committed-only-in-wal", value)
}

func TestWindowsBackupWALWriterHelper(t *testing.T) {
	path := os.Getenv("UVP_BACKUP_WAL_WRITER")
	if path == "" {
		t.Skip("subprocess only")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		os.Exit(81)
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec("PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE backup_wal_fixture(id INTEGER PRIMARY KEY, value TEXT); INSERT INTO backup_wal_fixture VALUES(1,'committed-only-in-wal');")
	if err != nil {
		os.Exit(82)
	}
	// Deliberately exit without Close: the parent must preserve and replay WAL.
	os.Exit(0)
}
