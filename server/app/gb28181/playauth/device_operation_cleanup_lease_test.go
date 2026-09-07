package playauth

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDeviceSIPCleanupLeaseOverlapsOriginalAndTransfer(t *testing.T) {
	f, store, id := sipCleanupFixture(t)
	require.NoError(t, f.db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
	ctx := context.Background()
	b := NewDeviceOperationBarrier(NewDeviceSecurityStore(f.db))
	original, err := b.BeginEpoch(ctx, id.DeviceCode, id.DeviceEpoch)
	require.NoError(t, err)
	defer original.Release()
	guard, err := b.LockTransfer(ctx, uint(id.DevicePK))
	require.NoError(t, err)
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	guard.Commit(2)
	guard.Release()
	require.ErrorIs(t, context.Cause(original.Context()), ErrDeviceOperationRevoked)
	out, ticket, err := store.PrepareSIPBranchCleanupWork(ctx, id, 5, sipCleanupIdentity(1))
	require.NoError(t, err)
	require.EqualValues(t, 6, out.Intent.RowVersion)
	cleanup, err := b.BeginSIPCleanup(ctx, ticket)
	require.NoError(t, err)
	defer cleanup.Release()
	require.Equal(t, id.DeviceEpoch, cleanup.OperationEpoch())
	require.NoError(t, cleanup.Context().Err(), "prior transfer must not pre-cancel new compensation")
	original.Release()
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, b.WaitBefore(waitCtx, uint(id.DevicePK), 2), context.DeadlineExceeded)
	_, err = b.BeginEpoch(ctx, id.DeviceCode, 2)
	require.Error(t, err, "cleanup lease grants no fresh business authority")
	guard, err = b.LockTransfer(ctx, uint(id.DevicePK))
	require.NoError(t, err)
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=3 WHERE id=1").Error)
	guard.Commit(3)
	guard.Release()
	require.ErrorIs(t, context.Cause(cleanup.Context()), ErrDeviceOperationRevoked)
	waitCtx2, cancel2 := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel2()
	require.ErrorIs(t, b.WaitBefore(waitCtx2, uint(id.DevicePK), 3), context.DeadlineExceeded, "cancel is not release")
	cleanup.Release()
	require.NoError(t, b.WaitBefore(ctx, uint(id.DevicePK), 3))
	state, err := NewDeviceCleanupStore(f.db).Load(ctx, id.DeviceCode)
	require.NoError(t, err)
	require.EqualValues(t, 1, state.CleanupCompletedEpoch, "quiescence does not complete unknown remote work")
}

func TestDeviceSIPCleanupLeaseTicketSingleUseEvenWhenCopied(t *testing.T) {
	f, store, id := sipCleanupFixture(t)
	ctx := context.Background()
	b := NewDeviceOperationBarrier(NewDeviceSecurityStore(f.db))
	_, ticket, err := store.PrepareSIPBranchCleanupWork(ctx, id, 5, sipCleanupIdentity(1))
	require.NoError(t, err)
	encoded, err := json.Marshal(ticket)
	require.NoError(t, err)
	require.JSONEq(t, "{}", string(encoded))
	var wins atomic.Int32
	var wg sync.WaitGroup
	for n := 0; n < 20; n++ {
		copy := *ticket
		wg.Add(1)
		go func() {
			defer wg.Done()
			lease, err := b.BeginSIPCleanup(ctx, &copy)
			if err == nil {
				wins.Add(1)
				lease.Release()
			} else {
				require.ErrorIs(t, err, ErrDeviceIntentConflict)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, wins.Load())
	_, duplicate, err := store.PrepareSIPBranchCleanupWork(ctx, id, 6, sipCleanupIdentity(1))
	require.NoError(t, err)
	require.Nil(t, duplicate, "idempotent observation cannot mint another work ticket")
}

func TestDeviceSIPCleanupLeaseRejectsChangedEvidence(t *testing.T) {
	for _, change := range []string{"device", "run", "attempt", "consumed", "quiesced", "covered", "other-database"} {
		t.Run(change, func(t *testing.T) {
			f, store, id := sipCleanupFixture(t)
			ctx := context.Background()
			b := NewDeviceOperationBarrier(NewDeviceSecurityStore(f.db))
			_, ticket, err := store.PrepareSIPBranchCleanupWork(ctx, id, 5, sipCleanupIdentity(1))
			require.NoError(t, err)
			switch change {
			case "device":
				ticket.work.intent.DevicePK++
			case "run":
				ticket.work.runID = "other-process"
			case "attempt":
				ticket.work.identity.AttemptID = "000000000000000000000000000000ff"
			case "consumed":
				_, err = store.DispatchSIPCleanupACK(ctx, id, 6, sipCleanupIdentity(1).AttemptID)
				require.NoError(t, err)
			case "quiesced":
				_, err = store.ObserveSIPCleanupQuiesced(ctx, id, 6, sipCleanupIdentity(1).AttemptID)
				require.NoError(t, err)
			case "covered":
				require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2, cleanup_completed_epoch=2 WHERE id=1").Error)
			case "other-database":
				other, _, _ := sipCleanupFixture(t)
				b = NewDeviceOperationBarrier(NewDeviceSecurityStore(other.db))
			}
			lease, err := b.BeginSIPCleanup(ctx, ticket)
			require.Error(t, err)
			require.Nil(t, lease)
			b.mu.Lock()
			require.Empty(t, b.lanes, "failure must not retain a lane or owner")
			b.mu.Unlock()
		})
	}
}

func TestDeviceSIPCleanupLeaseUnknownPrepareHasNoTicket(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "lost-commit-reply"}[committed], func(t *testing.T) {
			f, store, id := sipCleanupFixture(t)
			ctx := context.Background()
			faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: ctx})
			faultDB.Statement.ConnPool = intentCommitFaultPool{ConnPool: f.db.Statement.ConnPool, commitFirst: committed}
			out, ticket, err := NewDeviceOperationIntentStore(faultDB).PrepareSIPBranchCleanupWork(ctx, id, 5, sipCleanupIdentity(1))
			require.ErrorIs(t, err, ErrDeviceIntentUnavailable)
			require.Empty(t, out)
			require.Nil(t, ticket)
			loaded, err := store.LoadSIPInviteSteps(ctx, id)
			require.NoError(t, err)
			if committed {
				require.EqualValues(t, 6, loaded.Intent.RowVersion)
				_, ticket, err = store.PrepareSIPBranchCleanupWork(ctx, id, 6, sipCleanupIdentity(1))
				require.NoError(t, err)
				require.Nil(t, ticket)
			} else {
				require.EqualValues(t, 5, loaded.Intent.RowVersion)
			}
		})
	}
}

func TestDeviceSIPCleanupLeaseWaitsForTransferGuardAndBurnsCancelledTicket(t *testing.T) {
	f, store, id := sipCleanupFixture(t)
	ctx := context.Background()
	b := NewDeviceOperationBarrier(NewDeviceSecurityStore(f.db))
	_, ticket, err := store.PrepareSIPBranchCleanupWork(ctx, id, 5, sipCleanupIdentity(1))
	require.NoError(t, err)
	guard, err := b.LockTransfer(ctx, uint(id.DevicePK))
	require.NoError(t, err)
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	lease, err := b.BeginSIPCleanup(waitCtx, ticket)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, lease)
	guard.Release()
	lease, err = b.BeginSIPCleanup(ctx, ticket)
	require.ErrorIs(t, err, ErrDeviceIntentConflict)
	require.Nil(t, lease)
	require.NoError(t, b.WaitBefore(ctx, uint(id.DevicePK), 2))
}
