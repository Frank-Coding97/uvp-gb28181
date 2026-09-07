package gormhelper

import (
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestSQLiteClientUsesLockedRuntimeAndPersistentSingleConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "数据库 # space.db")
	db, err := NewSQLiteClient(path)
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	info, err := InspectSQLite(db)
	require.NoError(t, err)
	require.Equal(t, "3.53.4", info.Version)
	require.Equal(t, "wal", info.JournalMode)
	require.Equal(t, 2, info.Synchronous)
	require.Equal(t, 1, info.ForeignKeys)
	require.Equal(t, 5000, info.BusyTimeout)
	require.Equal(t, 1, info.MaxOpenConnections)
	require.NoError(t, db.Exec("CREATE TABLE durable (value TEXT NOT NULL)").Error)
	require.NoError(t, db.Exec("INSERT INTO durable VALUES ('persisted')").Error)
	require.NoError(t, raw.Close())
	reopened, err := NewSQLiteClient(path)
	require.NoError(t, err)
	reopenedRaw, err := reopened.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopenedRaw.Close() })
	var value string
	require.NoError(t, reopened.Raw("SELECT value FROM durable").Scan(&value).Error)
	require.Equal(t, "persisted", value)
}

func TestSQLiteClientRejectsRelativeAndSharedPaths(t *testing.T) {
	for _, path := range []string{"", "local.db", `\\server\share\uvp.db`} {
		_, err := NewSQLiteClient(path)
		require.Error(t, err)
	}
}
