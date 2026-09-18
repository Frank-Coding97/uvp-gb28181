package authoritytest

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
	"uvplatform.cn/uvp-gb28181/app/openapi/processauthority"
)

func TestRealTransactionCommitFault(t *testing.T) {
	for _, committed := range []bool{false, true} {
		name := "rollback"
		if committed {
			name = "committed"
		}
		t.Run(name, func(t *testing.T) {
			if !InProcess(t) {
				return
			}
			db := OpenSQLite(t, filepath.Join(t.TempDir(), "test.sqlite"))
			a := Register(t, db, "")
			require.NoError(t, db.Exec("CREATE TABLE effect (id INTEGER PRIMARY KEY)").Error)
			fault := CommitFaultDB(t, db, 1, committed)
			raw, err := db.DB()
			require.NoError(t, err)
			faultRaw, err := fault.DB()
			require.NoError(t, err)
			require.Same(t, raw, faultRaw)
			var before models.ProcessAuthority
			require.NoError(t, db.First(&before).Error)
			err = fault.Transaction(func(tx *gorm.DB) error {
				_, concrete := tx.Statement.ConnPool.(*sql.Tx)
				require.True(t, concrete)
				if err := a.CheckTx(tx); err != nil {
					return err
				}
				return tx.Exec("INSERT INTO effect VALUES(1)").Error
			})
			require.ErrorIs(t, err, ErrCommitReply)
			var count int64
			require.NoError(t, db.Table("effect").Count(&count).Error)
			var after models.ProcessAuthority
			require.NoError(t, db.First(&after).Error)
			if committed {
				require.EqualValues(t, 1, count)
				require.Equal(t, before.RowVersion+1, after.RowVersion)
			} else {
				require.Zero(t, count)
				require.Equal(t, before.RowVersion, after.RowVersion)
			}
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return a.CheckTx(tx) }))
			require.NoError(t, fault.Transaction(func(tx *gorm.DB) error { return a.CheckTx(tx) }), "fault fires once")
			second, err := processauthority.Register(context.Background(), db, nil)
			require.ErrorIs(t, err, processauthority.ErrProcessAuthorityUnavailable)
			require.Nil(t, second)
		})
	}
}

func TestAuthorizedLeafFlatSlashNamesDoNotRespawn(t *testing.T) {
	for _, name := range []string{"direct", "direct/additional", "other"} {
		t.Run(name, func(t *testing.T) {
			if !InProcess(t) {
				return
			}
			db := OpenSQLite(t, filepath.Join(t.TempDir(), "leaf.sqlite"))
			a := Register(t, db, "")
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return a.CheckTx(tx) }))
		})
	}
}
