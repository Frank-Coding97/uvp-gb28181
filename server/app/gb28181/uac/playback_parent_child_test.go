package uac

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

func TestPlaybackParentLeaseSurvivesPreparedChildShutdown(t *testing.T) {
	for _, kind := range []string{"playback", "download"} {
		t.Run(kind, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}

			u, db, store, id, _ := playbackIntentStoreFixture(t)
			u.client.TxRequester = nil
			require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
			if kind == "download" {
				require.NoError(t, db.Table("gb_device_operation_intent").Where("operation_id = ?", id.OperationID).Update("kind", kind).Error)
				id.Kind = kind
			}
			barrier := newAuthorizedBarrierTest(t, db)
			parent, err := barrier.BeginEpoch(context.Background(), id.DeviceCode, id.DeviceEpoch)
			require.NoError(t, err)
			t.Cleanup(parent.Release)
			// If the child tries a second BeginEpoch it cannot enter this lane.
			guard, err := barrier.LockTransfer(context.Background(), uint(id.DevicePK))
			require.NoError(t, err)
			t.Cleanup(guard.Release)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			peer, err := net.Listen("tcp4", "127.0.0.1:0")
			require.NoError(t, err)
			t.Cleanup(func() { _ = peer.Close() })
			in := validPlaybackInvite()
			in.Destination, in.Transport = peer.Addr().String(), "TCP"
			op, err := u.beginPlaybackIntentChild(ctx, store, barrier, parent, id, 2, strings.Repeat("b", 32), in)
			require.NoError(t, err)
			require.NotNil(t, op)
			guard.Release()
			op.initCancel()
			op.cleanupCancel()
			require.NoError(t, op.shutdownLocal(context.Background()))
			require.NoError(t, parent.Context().Err(), "child cannot release/cancel its actual parent")
			wait, stop := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer stop()
			require.ErrorIs(t, barrier.WaitBefore(wait, uint(id.DevicePK), 2), context.DeadlineExceeded)
			loaded, err := store.LoadSIPInviteSteps(context.Background(), id)
			require.NoError(t, err)
			require.Len(t, loaded.Steps, 1)
			require.Equal(t, playauth.SIPStepPrepared, loaded.Steps[0].State)
			parent.Release()
			require.NoError(t, barrier.WaitBefore(context.Background(), uint(id.DevicePK), 2))
		})
	}
}
