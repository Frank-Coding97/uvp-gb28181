package authoritytest

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"path/filepath"
	"testing"

	gosqlite "github.com/glebarez/go-sqlite"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type unwrappedSQLiteConnector string

func (c unwrappedSQLiteConnector) Driver() driver.Driver { return &gosqlite.Driver{} }
func (c unwrappedSQLiteConnector) Connect(context.Context) (driver.Conn, error) {
	return c.Driver().Open(string(c))
}

func TestNativeConnectorKeepsActualTransactionAndPool(t *testing.T) {
	for _, committed := range []bool{false, true} {
		name := "rollback"
		if committed {
			name = "committed"
		}
		t.Run(name, func(t *testing.T) {
			if !InProcess(t) {
				return
			}
			raw := sql.OpenDB(CommitFaultConnector(unwrappedSQLiteConnector(filepath.Join(t.TempDir(), "connector.sqlite"))))
			raw.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, raw.Close()) })
			db, err := gorm.Open(sqlite.Dialector{Conn: raw}, &gorm.Config{})
			require.NoError(t, err)
			authority := Register(t, db, "")
			require.NoError(t, db.Exec("CREATE TABLE connector_effect (id INTEGER PRIMARY KEY)").Error)
			fault := CommitFaultDB(t, db, 1, committed)
			faultRaw, err := fault.DB()
			require.NoError(t, err)
			require.Same(t, raw, faultRaw)
			err = fault.Transaction(func(tx *gorm.DB) error {
				_, real := tx.Statement.ConnPool.(*sql.Tx)
				require.True(t, real)
				if err := authority.CheckTx(tx); err != nil {
					return err
				}
				return tx.Exec("INSERT INTO connector_effect VALUES(?)", 1).Error
			})
			require.ErrorIs(t, err, ErrCommitReply)
			var count int64
			require.NoError(t, db.Table("connector_effect").Count(&count).Error)
			if committed {
				require.EqualValues(t, 1, count)
			} else {
				require.Zero(t, count)
			}
			require.NoError(t, fault.Transaction(func(tx *gorm.DB) error { return authority.CheckTx(tx) }))
		})
	}
}
