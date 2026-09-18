package processauthority

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

type authorityCommitFaultPool struct {
	*sql.DB
	committed bool
}

func (p authorityCommitFaultPool) GetDBConn() (*sql.DB, error) { return p.DB, nil }

func (p authorityCommitFaultPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &authorityCommitFaultTx{Tx: tx, committed: p.committed}, nil
}

type authorityCommitFaultTx struct {
	*sql.Tx
	committed bool
}

func (tx *authorityCommitFaultTx) Commit() error {
	if tx.committed {
		if err := tx.Tx.Commit(); err != nil {
			return err
		}
	} else {
		_ = tx.Tx.Rollback()
	}
	return errors.New("fixture commit acknowledgement lost")
}

func TestProcessAuthorityUnknownRegistrationNeverReturnsHandle(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprintf("committed=%v", committed), func(t *testing.T) {
			db, lock := authorityDB(t), authorityLock(t)
			base, err := db.DB()
			require.NoError(t, err)
			fault := db.Session(&gorm.Session{NewDB: true, Context: context.Background()})
			fault.Statement.ConnPool = authorityCommitFaultPool{DB: base, committed: committed}
			var root registrationLatch
			authority, err := root.register(context.Background(), fault, lock)
			require.ErrorIs(t, err, ErrProcessAuthorityUnavailable)
			require.Nil(t, authority)
			var count int64
			require.NoError(t, db.Model(&models.ProcessGeneration{}).Count(&count).Error)
			if committed {
				require.EqualValues(t, 1, count)
			} else {
				require.Zero(t, count)
			}
			authority, err = root.register(context.Background(), db, lock)
			require.ErrorIs(t, err, ErrProcessAuthorityUnavailable)
			require.Nil(t, authority, "readback cannot recreate dispatch permission after a lost commit acknowledgement")
		})
	}
}

func TestProcessAuthorityChangedGenerationPermanentlySealsOldHandle(t *testing.T) {
	db, lock := authorityDB(t), authorityLock(t)
	var root registrationLatch
	authority, err := root.register(context.Background(), db, lock)
	require.NoError(t, err)
	domain, err := lock.DomainID()
	require.NoError(t, err)
	next := models.ProcessGeneration{GenerationID: strings.Repeat("c", 32), DomainID: domain, StartedAt: time.Now().UTC()}
	require.NoError(t, db.Create(&next).Error)
	require.NoError(t, db.Model(&models.ProcessAuthority{}).Where("id=1").UpdateColumn("current_generation_id", next.GenerationID).Error)
	require.ErrorIs(t, db.Transaction(authority.CheckTx), ErrProcessAuthorityUnavailable)
	require.NoError(t, db.Model(&models.ProcessAuthority{}).Where("id=1").UpdateColumn("current_generation_id", authority.GenerationID()).Error)
	require.ErrorIs(t, db.Transaction(authority.CheckTx), ErrProcessAuthorityUnavailable)
}

func TestProcessAuthoritySealDuringSQLRollsBackFence(t *testing.T) {
	for _, stage := range []string{"before", "during-update", "during-history"} {
		t.Run(stage, func(t *testing.T) {
			db, lock := authorityDB(t), authorityLock(t)
			var root registrationLatch
			authority, err := root.register(context.Background(), db, lock)
			require.NoError(t, err)
			switch stage {
			case "before":
				authority.Seal()
			case "during-update":
				require.NoError(t, db.Callback().Update().After("gorm:update").Register("authority-test:seal", func(*gorm.DB) { authority.Seal() }))
				defer db.Callback().Update().Remove("authority-test:seal")
			case "during-history":
				require.NoError(t, db.Callback().Query().After("gorm:query").Register("authority-test:seal", func(*gorm.DB) { authority.Seal() }))
				defer db.Callback().Query().Remove("authority-test:seal")
			}
			require.ErrorIs(t, db.Transaction(authority.CheckTx), ErrProcessAuthorityUnavailable)
			var row models.ProcessAuthority
			require.NoError(t, db.Take(&row).Error)
			require.EqualValues(t, 1, row.RowVersion, "failed authorization rolls back the whole fence transaction")
			require.ErrorIs(t, db.Transaction(authority.CheckTx), ErrProcessAuthorityUnavailable)
		})
	}
}

func TestProcessAuthorityPreparedTransactionsAndForgedTransaction(t *testing.T) {
	db, lock := authorityDB(t), authorityLock(t)
	var root registrationLatch
	authority, err := root.register(context.Background(), db, lock)
	require.NoError(t, err)
	require.NoError(t, db.Session(&gorm.Session{PrepareStmt: true}).Transaction(authority.CheckTx))
	base, err := db.DB()
	require.NoError(t, err)
	fake := db.Session(&gorm.Session{NewDB: true, Context: context.Background()})
	fake.Statement.ConnPool = authorityFakeTransaction{DB: base}
	require.ErrorIs(t, authority.CheckTx(fake), ErrProcessAuthorityUnavailable, "TxCommitter interface alone cannot prove a real SQL transaction")
	require.NoError(t, db.Transaction(authority.CheckTx))
}

type authorityFakeTransaction struct{ *sql.DB }

func (p authorityFakeTransaction) GetDBConn() (*sql.DB, error) { return p.DB, nil }
func (authorityFakeTransaction) Commit() error                 { return nil }
func (authorityFakeTransaction) Rollback() error               { return nil }
