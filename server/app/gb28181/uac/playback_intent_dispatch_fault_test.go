package uac

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

func requirePlaybackNoPacket(t *testing.T, peer net.PacketConn) {
	t.Helper()
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(50*time.Millisecond)))
	buffer := make([]byte, 8192)
	_, _, err := peer.ReadFrom(buffer)
	var timeout net.Error
	require.ErrorAs(t, err, &timeout)
	require.True(t, timeout.Timeout(), "no datagram, not just no ACK")
}

func TestPlaybackIntentOperationUnknownInviteCommitNeverSends(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprintf("committed=%v", committed), func(t *testing.T) {
			f := newPlaybackOperationPreparedUDPFixture(t)
			faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: context.Background()})
			faultDB.Statement.ConnPool = &playbackOperationCommitFault{ConnPool: f.db.Statement.ConnPool, failAt: 1, commitFirst: committed}
			f.op.store = playauth.NewDeviceOperationIntentStore(faultDB)
			require.Error(t, f.op.Start(context.Background()))
			requirePlaybackNoPacket(t, f.peer)
			f.op.store = f.store
			require.Error(t, f.op.Start(context.Background()), "readback cannot grant another INVITE start")
			require.NoError(t, f.op.CloseLocal(context.Background()))
			loaded, err := f.store.LoadSIPInviteSteps(context.Background(), f.id)
			require.NoError(t, err)
			want := playauth.SIPStepPrepared
			if committed {
				want = playauth.SIPStepMayHaveDispatched
			}
			require.Equal(t, want, loaded.Steps[0].State)
			require.Equal(t, playauth.IntentDispatched, loaded.Intent.State)
			requirePlaybackNoPacket(t, f.peer)
		})
	}
}

func TestPlaybackIntentOperationUnknownCancelCommitNeverSends(t *testing.T) {
	for _, step := range []int32{1, 2} {
		for _, committed := range []bool{false, true} {
			t.Run(fmt.Sprintf("step=%d/committed=%v", step, committed), func(t *testing.T) {
				f := newPlaybackOperationUDPFixture(t)
				f.respond(t, 180)
				require.Eventually(t, func() bool {
					f.op.factMu.Lock()
					defer f.op.factMu.Unlock()
					return f.op.provisional
				}, time.Second, time.Millisecond)
				faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: context.Background()})
				faultDB.Statement.ConnPool = &playbackOperationCommitFault{ConnPool: f.db.Statement.ConnPool, failAt: step, commitFirst: committed}
				f.op.store = playauth.NewDeviceOperationIntentStore(faultDB)
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				guard, err := f.barrier.LockTransfer(ctx, 1)
				require.NoError(t, err)
				require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
				guard.Commit(2)
				guard.Release()
				require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2), "supervisor terminates and reads back without restoring CANCEL permission")
				requirePlaybackNoPacket(t, f.peer)
				loaded, err := f.store.LoadSIPInviteSteps(ctx, f.id)
				require.NoError(t, err)
				if step == 1 && !committed {
					require.Nil(t, loaded.Steps[0].Cancel)
				} else {
					require.NotNil(t, loaded.Steps[0].Cancel)
					want := playauth.SIPStepPrepared
					if step == 2 && committed {
						want = playauth.SIPStepMayHaveDispatched
					}
					require.Equal(t, want, loaded.Steps[0].Cancel.State)
				}
				require.Error(t, f.op.Cancel(ctx))
				require.Equal(t, playauth.IntentDispatched, loaded.Intent.State)
				requirePlaybackNoPacket(t, f.peer)
			})
		}
	}
}

func TestPlaybackIntentOperationTransferBeforeStartNeverSends(t *testing.T) {
	f := newPlaybackOperationPreparedUDPFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	guard, err := f.barrier.LockTransfer(ctx, 1)
	require.NoError(t, err)
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	guard.Commit(2)
	guard.Release()
	require.Error(t, f.op.Start(ctx))
	require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
	requirePlaybackNoPacket(t, f.peer)
	loaded, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.Equal(t, playauth.SIPStepPrepared, loaded.Steps[0].State)
}
