package gormhelper

import (
	"context"
	"database/sql"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
	"time"
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

func TestSQLiteWriteTransactionReservesWriterBeforeCallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "competing.db")
	first, err := NewSQLiteClient(path)
	require.NoError(t, err)
	second, err := NewSQLiteClient(path)
	require.NoError(t, err)
	a, err := first.DB()
	require.NoError(t, err)
	defer a.Close()
	b, err := second.DB()
	require.NoError(t, err)
	defer b.Close()
	require.NoError(t, first.Exec("CREATE TABLE contention(id INTEGER PRIMARY KEY)").Error)
	tx, err := a.Begin()
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.Exec("INSERT INTO contention VALUES(1)")
	require.NoError(t, err)
	type begun struct {
		tx  *sql.Tx
		err error
	}
	next := make(chan begun, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() { other, e := b.BeginTx(ctx, nil); next <- begun{other, e} }()
	select {
	case result := <-next:
		if result.tx != nil {
			_ = result.tx.Rollback()
		}
		t.Fatalf("second writer began before the first released its reservation: %v", result.err)
	case <-time.After(100 * time.Millisecond):
	}
	require.NoError(t, tx.Commit())
	result := <-next
	require.NoError(t, result.err)
	_, err = result.tx.Exec("INSERT INTO contention VALUES(2)")
	require.NoError(t, err)
	require.NoError(t, result.tx.Commit())
	// ReadOnly intentionally bypasses the writer reservation in WAL mode.
	reader, err := a.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	require.NoError(t, err)
	defer reader.Rollback()
	var count int
	require.NoError(t, reader.QueryRow("SELECT COUNT(*) FROM contention").Scan(&count))
	require.Equal(t, 2, count)
	writer, err := b.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = writer.Exec("INSERT INTO contention VALUES(3)")
	require.NoError(t, err)
	require.NoError(t, writer.Commit())
	require.NoError(t, reader.Commit())
}
