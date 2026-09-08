package authoritytest

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync/atomic"
	"testing"

	gosqlite "github.com/glebarez/go-sqlite"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var ErrCommitReply = errors.New("fixture commit reply unavailable")

type commitContextKey struct{}

type commitFault struct {
	count     atomic.Int32
	hit       atomic.Bool
	failAt    int32
	committed bool
	after     func() error
}

type sqliteConnector struct{ path string }

func (c sqliteConnector) Driver() driver.Driver { return &gosqlite.Driver{} }
func (c sqliteConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := c.Driver().Open(c.path)
	if err != nil {
		return nil, err
	}
	return &sqliteConn{Conn: conn}, nil
}

type sqliteConn struct{ driver.Conn }

func (c *sqliteConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, err := c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	if fault, ok := ctx.Value(commitContextKey{}).(*commitFault); ok {
		return &sqliteTx{Tx: tx, fault: fault}, nil
	}
	return tx, nil
}
func (c *sqliteConn) Ping(ctx context.Context) error { return c.Conn.(driver.Pinger).Ping(ctx) }
func (c *sqliteConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	return c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, query)
}
func (c *sqliteConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, query, args)
}
func (c *sqliteConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, query, args)
}

type sqliteTx struct {
	driver.Tx
	fault *commitFault
}

func (tx *sqliteTx) Commit() error {
	if tx.fault.count.Add(1) != tx.fault.failAt || !tx.fault.hit.CompareAndSwap(false, true) {
		return tx.Tx.Commit()
	}
	var err error
	if tx.fault.committed {
		err = tx.Tx.Commit()
	} else {
		err = tx.Tx.Rollback()
	}
	if err != nil {
		return err
	}
	if tx.fault.after != nil {
		return tx.fault.after()
	}
	return ErrCommitReply
}

func OpenSQLite(t *testing.T, path string) *gorm.DB {
	t.Helper()
	raw := sql.OpenDB(sqliteConnector{path: path})
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, raw.Close()) })
	db, err := gorm.Open(sqlite.Dialector{Conn: raw}, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	return db
}

// The factory selects only this session's transactions. It returns the actual
// *sql.Tx from the registered pool; only the driver beneath database/sql injects
// commit faults. Authority's concrete-transaction and same-pool checks remain.
type commitFaultPool struct {
	*sql.DB
	fault *commitFault
}

func (p *commitFaultPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return p.DB.BeginTx(context.WithValue(ctx, commitContextKey{}, p.fault), opts)
}
func (p *commitFaultPool) GetDBConn() (*sql.DB, error) { return p.DB, nil }

func CommitFaultDB(t *testing.T, db *gorm.DB, failAt int32, committed bool) *gorm.DB {
	return commitHookDB(t, db, failAt, committed, nil)
}

// AfterCommitDB is for real process-kill fixtures paused after durable commit.
func AfterCommitDB(t *testing.T, db *gorm.DB, at int32, after func() error) *gorm.DB {
	return commitHookDB(t, db, at, true, after)
}

func commitHookDB(t *testing.T, db *gorm.DB, failAt int32, committed bool, after func() error) *gorm.DB {
	t.Helper()
	require.Positive(t, failAt)
	raw, err := db.DB()
	require.NoError(t, err)
	_, sqliteOK := raw.Driver().(*gosqlite.Driver)
	_, nativeOK := raw.Driver().(faultDriver)
	require.True(t, sqliteOK || nativeOK, "pool must be opened with a commit fault connector")
	out := db.Session(&gorm.Session{NewDB: true, Context: context.Background()})
	fault := &commitFault{failAt: failAt, committed: committed, after: after}
	out.Statement.ConnPool = &commitFaultPool{DB: raw, fault: fault}
	t.Cleanup(func() { require.True(t, fault.hit.Load(), "target commit fault was never exercised") })
	return out
}
