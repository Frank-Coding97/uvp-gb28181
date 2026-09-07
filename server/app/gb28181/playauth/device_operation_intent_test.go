package playauth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newIntentFixture(t *testing.T) (*deviceCleanupFixture, *DeviceOperationIntentStore) {
	t.Helper()
	f := newDeviceCleanupFixture(t)
	f.add(t, 1, cleanupDeviceA, 1, 1)
	f.add(t, 2, cleanupDeviceB, 1, 1)
	require.NoError(t, f.db.Exec(`CREATE TABLE gb_device_operation_intent (
		operation_id TEXT PRIMARY KEY, device_pk BIGINT NOT NULL, device_code TEXT NOT NULL,
		target_scope TEXT NOT NULL, target_pk BIGINT NOT NULL, target_code TEXT NOT NULL,
		contract_version BIGINT NOT NULL, device_epoch BIGINT NOT NULL, kind TEXT NOT NULL,
		state TEXT NOT NULL, row_version BIGINT NOT NULL, created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL, dispatch_started_at DATETIME NULL, cancelled_at DATETIME NULL
	)`).Error)
	require.NoError(t, f.db.Exec(`CREATE TABLE gb_channel (id BIGINT PRIMARY KEY, device_id TEXT, channel_id TEXT, deleted_at DATETIME)`).Error)
	require.NoError(t, f.db.Exec(`INSERT INTO gb_channel(id,device_id,channel_id) VALUES (11,?,?), (12,?,?)`, cleanupDeviceA, cleanupDeviceC, cleanupDeviceB, cleanupDeviceC).Error)
	return f, NewDeviceOperationIntentStore(f.db)
}

func intentIdentity(n int) DeviceOperationIntentIdentity {
	return DeviceOperationIntentIdentity{OperationID: fmt.Sprintf("%032x", n), DevicePK: 1,
		DeviceCode: cleanupDeviceA, TargetScope: "channel", TargetPK: 11, TargetCode: cleanupDeviceC, DeviceEpoch: 1, Kind: "live"}
}

func TestDeviceOperationIntentReservationIsDurableAndImmutable(t *testing.T) {
	f, s := newIntentFixture(t)
	ctx := context.Background()
	id := intentIdentity(1)
	first, err := s.Reserve(ctx, id)
	require.NoError(t, err)
	require.Equal(t, IntentReserved, first.State)
	require.Equal(t, int64(1), first.RowVersion)
	require.False(t, first.CreatedAt.IsZero())

	// Recreate the service, not the database: recovery sees the original intent.
	s = NewDeviceOperationIntentStore(f.db)
	second, err := s.Reserve(ctx, id)
	require.NoError(t, err)
	require.Equal(t, first, second)
	for _, change := range []func(*DeviceOperationIntentIdentity){
		func(i *DeviceOperationIntentIdentity) {
			i.TargetScope = "device"
			i.TargetPK = 1
			i.TargetCode = cleanupDeviceA
		},
		func(i *DeviceOperationIntentIdentity) { i.Kind = "playback" },
		func(i *DeviceOperationIntentIdentity) { i.DevicePK = 2; i.DeviceCode = cleanupDeviceB; i.TargetPK = 12 },
	} {
		changed := id
		change(&changed)
		_, err = s.Reserve(ctx, changed)
		require.ErrorIs(t, err, ErrDeviceIntentConflict)
	}
	_, err = s.Dispatch(ctx, id, 1)
	require.NoError(t, err)
	rows, err := NewDeviceOperationIntentStore(f.db).ListUnsettled(ctx, 1, cleanupDeviceA, 2, "", 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, IntentDispatched, rows[0].State)
	require.Equal(t, int64(2), rows[0].RowVersion)
	require.NotNil(t, rows[0].DispatchStartedAt)
}

func TestDeviceOperationIntentDispatchHasOnlyOneWinner(t *testing.T) {
	_, s := newIntentFixture(t)
	id := intentIdentity(1)
	_, err := s.Reserve(context.Background(), id)
	require.NoError(t, err)
	var winners atomic.Int64
	var wg sync.WaitGroup
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Dispatch(context.Background(), id, 1)
			if err == nil {
				winners.Add(1)
			} else {
				require.ErrorIs(t, err, ErrDeviceIntentConflict)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int64(1), winners.Load())
	require.ErrorIs(t, s.CancelReserved(context.Background(), id.OperationID, 2), ErrDeviceIntentConflict)
}

func TestDeviceOperationIntentTransferRevokesDispatchNotOtherDevice(t *testing.T) {
	f, s := newIntentFixture(t)
	ctx := context.Background()
	id := intentIdentity(1)
	_, err := s.Reserve(ctx, id)
	require.NoError(t, err)
	other := intentIdentity(2)
	other.DevicePK, other.DeviceCode = 2, cleanupDeviceB
	other.TargetPK = 12
	_, err = s.Reserve(ctx, other)
	require.NoError(t, err)
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	_, err = s.Dispatch(ctx, id, 1)
	require.ErrorIs(t, err, ErrDeviceIntentRevoked)
	newEpoch := intentIdentity(3)
	newEpoch.DeviceEpoch = 2
	_, err = s.Reserve(ctx, newEpoch)
	require.ErrorIs(t, err, ErrDeviceCleanupPending)
	require.NoError(t, s.CancelReserved(ctx, id.OperationID, 1))
	require.ErrorIs(t, s.CancelReserved(ctx, id.OperationID, 1), ErrDeviceIntentConflict)
	_, err = s.Dispatch(ctx, other, 1)
	require.NoError(t, err)
	state, err := NewDeviceCleanupStore(f.db).Load(ctx, cleanupDeviceA)
	require.NoError(t, err)
	require.Equal(t, int64(1), state.CleanupCompletedEpoch, "empty/cancelled ledger must not advance device cleanup")
}

func TestDeviceOperationIntentRejectsInvalidIdentityAndUnavailableStorage(t *testing.T) {
	f, s := newIntentFixture(t)
	ctx := context.Background()
	for _, change := range []func(*DeviceOperationIntentIdentity){
		func(i *DeviceOperationIntentIdentity) { i.OperationID = "INVALID" },
		func(i *DeviceOperationIntentIdentity) { i.DeviceEpoch = 0 },
		func(i *DeviceOperationIntentIdentity) { i.DevicePK = 0 },
		func(i *DeviceOperationIntentIdentity) { i.DeviceCode = "bad" },
		func(i *DeviceOperationIntentIdentity) { i.TargetCode = "bad" },
		func(i *DeviceOperationIntentIdentity) { i.TargetScope = "any" },
		func(i *DeviceOperationIntentIdentity) { i.TargetPK = 0 },
		func(i *DeviceOperationIntentIdentity) { i.Kind = "arbitrary-command" },
	} {
		id := intentIdentity(1)
		change(&id)
		_, err := s.Reserve(ctx, id)
		require.ErrorIs(t, err, ErrDeviceIntentInvalid)
	}
	id := intentIdentity(1)
	id.DevicePK = 2
	_, err := s.Reserve(ctx, id)
	require.Error(t, err, "PK/code mismatch cannot be rebound")
	require.NoError(t, f.db.Exec("DROP TABLE gb_device_operation_intent").Error)
	_, err = s.Reserve(ctx, intentIdentity(1))
	require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
	_, err = s.Dispatch(ctx, intentIdentity(1), 1)
	require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
	_, err = NewDeviceOperationIntentStore(nil).Reserve(ctx, intentIdentity(1))
	require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
	_, err = s.Reserve(nil, intentIdentity(1))
	require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
}

func TestDeviceOperationIntentRecoveryIsBoundedAndExact(t *testing.T) {
	f, s := newIntentFixture(t)
	ctx := context.Background()
	for n := 1; n <= 3; n++ {
		_, err := s.Reserve(ctx, intentIdentity(n))
		require.NoError(t, err)
	}
	require.NoError(t, s.CancelReserved(ctx, intentIdentity(2).OperationID, 1))
	rows, err := s.ListUnsettled(ctx, 1, cleanupDeviceA, 2, "", 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, intentIdentity(1).OperationID, rows[0].OperationID)
	rows, err = s.ListUnsettled(ctx, 1, cleanupDeviceA, 2, rows[0].OperationID, 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, intentIdentity(3).OperationID, rows[0].OperationID)
	rows, err = s.ListUnsettled(ctx, 2, cleanupDeviceB, 2, "", 10)
	require.NoError(t, err)
	require.Empty(t, rows)
	rows, err = s.ListUnsettled(ctx, 1, cleanupDeviceA, 1, "", 10)
	require.NoError(t, err)
	require.Empty(t, rows, "current/newer epochs are not cleanup targets")
	require.NoError(t, f.db.Exec("UPDATE gb_device_operation_intent SET state='corrupt' WHERE operation_id=?", intentIdentity(1).OperationID).Error)
	_, err = s.ListUnsettled(ctx, 1, cleanupDeviceA, 2, "", 10)
	require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
}

func TestDeviceOperationIntentIDsAndStateEvidence(t *testing.T) {
	a, err := NewDeviceOperationIntentID()
	require.NoError(t, err)
	b, err := NewDeviceOperationIntentID()
	require.NoError(t, err)
	require.True(t, validIntentID(a))
	require.True(t, validIntentID(b))
	require.NotEqual(t, a, b)
	f, s := newIntentFixture(t)
	id := intentIdentity(1)
	_, err = s.Reserve(context.Background(), id)
	require.NoError(t, err)
	_, err = s.Dispatch(context.Background(), id, 1)
	require.NoError(t, err)
	require.NoError(t, f.db.Exec("UPDATE gb_device_operation_intent SET dispatch_started_at=NULL WHERE operation_id=?", id.OperationID).Error)
	_, err = s.ListUnsettled(context.Background(), 1, cleanupDeviceA, 2, "", 10)
	require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
}

// Simulate both a failed commit and an acknowledged-by-DB commit whose reply
// was lost. This wraps only the isolated test database's transaction boundary.
type intentCommitFaultPool struct {
	gorm.ConnPool
	commitFirst bool
}
type intentCommitFaultTx struct {
	*sql.Tx
	commitFirst bool
}

func (p intentCommitFaultPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.ConnPool.(*sql.DB).BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &intentCommitFaultTx{Tx: tx, commitFirst: p.commitFirst}, nil
}

func (tx intentCommitFaultTx) Commit() error {
	if tx.commitFirst {
		if err := tx.Tx.Commit(); err != nil {
			return err
		}
	}
	return errors.New("fixture commit acknowledgement unavailable")
}

func TestDeviceOperationIntentCommitUnknownNeverGrantsDispatch(t *testing.T) {
	for _, commitFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("committed=%v", commitFirst), func(t *testing.T) {
			f, normal := newIntentFixture(t)
			ctx := context.Background()
			faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: ctx})
			faultDB.Statement.ConnPool = intentCommitFaultPool{ConnPool: f.db.Statement.ConnPool, commitFirst: commitFirst}
			fault := NewDeviceOperationIntentStore(faultDB)
			id := intentIdentity(1)
			row, err := fault.Reserve(ctx, id)
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
			require.Empty(t, row.OperationID, "unknown commit must not return usable reservation")
			// A normal explicit retry may observe/create reservation; it does not
			// itself grant dispatch permission.
			_, err = normal.Reserve(ctx, id)
			require.NoError(t, err)
			var effects int
			row, err = fault.Dispatch(ctx, id, 1)
			if err == nil {
				effects++
			}
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
			require.Empty(t, row.OperationID)
			require.Zero(t, effects)
			rows, err := normal.ListUnsettled(ctx, 1, cleanupDeviceA, 2, "", 10)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			if commitFirst {
				require.Equal(t, IntentDispatched, rows[0].State)
				_, err = normal.Dispatch(ctx, id, 1)
				require.ErrorIs(t, err, ErrDeviceIntentConflict, "lost reply cannot grant a second execution")
			} else {
				require.Equal(t, IntentReserved, rows[0].State)
			}
		})
	}
}
