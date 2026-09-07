package playback

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

// Only the intent store uses this pool. The real admission barrier and
// assertions use the same dedicated SQLite DB through its healthy pool.
type parentCommitFaultPool struct {
	gorm.ConnPool
	commits   atomic.Int32
	failAt    int32
	committed bool
}

type parentCommitFaultTx struct {
	*sql.Tx
	pool *parentCommitFaultPool
}

func (p *parentCommitFaultPool) BeginTx(ctx context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.ConnPool.(*sql.DB).BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &parentCommitFaultTx{Tx: tx, pool: p}, nil
}

func (tx *parentCommitFaultTx) Commit() error {
	if tx.pool.commits.Add(1) != tx.pool.failAt {
		return tx.Tx.Commit()
	}
	if tx.pool.committed {
		if err := tx.Tx.Commit(); err != nil {
			return err
		}
	}
	return errors.New("fixture parent commit acknowledgement lost")
}

func TestPlaybackIntentServiceParentCommitUnknownNeverStartsResources(t *testing.T) {
	for _, failAt := range []int32{1, 2} { // Reserve, then Dispatch.
		for _, committed := range []bool{false, true} {
			t.Run(fmt.Sprintf("commit%d/committed=%v", failAt, committed), func(t *testing.T) {
				db, barrier, request := playbackEpochFixture(t)
				require.NoError(t, db.Exec("CREATE TABLE gb_channel (id INTEGER PRIMARY KEY, device_id TEXT, channel_id TEXT, deleted_at DATETIME)").Error)
				require.NoError(t, db.Exec("INSERT INTO gb_channel VALUES(2,?,?,NULL)", request.DeviceID, request.SIPChannelID).Error)
				faultDB := db.Session(&gorm.Session{NewDB: true, Context: context.Background()})
				faultDB.Statement.ConnPool = &parentCommitFaultPool{ConnPool: db.Statement.ConnPool, failAt: failAt, committed: committed}
				h := &heldPlaybackStage{}
				metrics := &Metrics{}
				service := NewService(NewRegistry(RegistryConfig{}), h, &parentServiceRTPFactory{RTPOpener: h},
					&parentServiceSIPFactory{PlaybackInviter: h}, h,
					ServiceConfig{DeviceOperations: barrier, Intents: playauth.NewDeviceOperationIntentStore(faultDB), Metrics: metrics})
				result, err := service.Create(context.Background(), request)
				require.ErrorIs(t, err, playauth.ErrDeviceIntentUnavailable)
				require.Nil(t, result.Session)
				require.Zero(t, h.node.Load(), "even the picker must follow confirmed parent dispatch")
				require.Zero(t, h.open.Load())
				require.Zero(t, h.invite.Load())
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				require.NoError(t, barrier.WaitBefore(ctx, 1, 2), "no child started, so no local lease remains")
				var rows []playauth.DeviceOperationIntent
				require.NoError(t, db.Find(&rows).Error)
				if failAt == 1 && !committed {
					require.Empty(t, rows)
				} else {
					require.Len(t, rows, 1)
					state := playauth.IntentReserved
					if failAt == 2 && committed {
						state = playauth.IntentDispatched
					}
					require.Equal(t, state, rows[0].State, "no false cancel/complete after an unknown commit")
				}
				var completed int64
				require.NoError(t, db.Table("gb_device").Select("cleanup_completed_epoch").Scan(&completed).Error)
				require.EqualValues(t, 1, completed)
				require.EqualValues(t, 1, metrics.Created.Load())
				require.EqualValues(t, 1, metrics.Failed.Load())
				require.EqualValues(t, 1, metrics.Cleaned.Load())
				require.NoError(t, service.Close(ctx))
			})
		}
	}
}
