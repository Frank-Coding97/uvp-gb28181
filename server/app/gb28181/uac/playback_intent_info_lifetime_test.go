package uac

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

func TestPlaybackIntentINFOBlockedCreateCannotOutliveOwnership(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	f := acceptedPlaybackINFOFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	finished := make(chan error, 1)
	go func() {
		finished <- f.op.sendINFO(ctx, playauth.DeviceSIPINFOCommand{Action: "pause"}, func(ctx context.Context, r *sip.Request) (*sip.ClientTx, error) {
			close(entered)
			<-release // Emulate a connection factory that returns after cancellation.
			return f.u.client.TransactionLayer().NewClientTransaction(context.Background(), r)
		})
	}()
	<-entered
	closeCtx, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	require.ErrorIs(t, f.op.CloseLocal(closeCtx), context.DeadlineExceeded)
	stop()
	waitCtx, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	require.ErrorIs(t, f.barrier.WaitBefore(waitCtx, 1, 2), context.DeadlineExceeded, "blocked factory remains owned")
	stop()
	once.Do(func() { close(release) })
	require.Error(t, <-finished)
	require.NoError(t, f.op.CloseLocal(ctx))
	require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
	f.noACK(t)
}

func TestPlaybackIntentINFOTxAndErrorStillJoinsActualQuiescence(t *testing.T) {
	for _, outcome := range []string{"tx-error", "mutated-request", "nil-tx"} {
		t.Run(outcome, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}

			f := acceptedPlaybackINFOFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var tx *sip.ClientTx
			err := f.op.sendINFO(ctx, playauth.DeviceSIPINFOCommand{Action: "pause"}, func(ctx context.Context, r *sip.Request) (*sip.ClientTx, error) {
				if outcome == "nil-tx" {
					return nil, errors.New("fixture factory error")
				}
				var err error
				tx, err = f.u.client.TransactionLayer().NewClientTransaction(ctx, r)
				if err != nil {
					return tx, err
				}
				if outcome == "mutated-request" {
					r.CSeq().SeqNo++
					return tx, nil
				}
				return tx, errors.New("fixture handle and error")
			})
			require.Error(t, err)
			if tx != nil {
				select {
				case <-tx.Quiesced():
				default:
					t.Fatal("lost concrete transaction on factory error")
				}
			}
			require.NoError(t, f.op.CloseLocal(ctx))
			require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
			f.noACK(t)
		})
	}
}

func TestPlaybackIntentINFOTransferStopsControlBeforeCleanup(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	f := acceptedPlaybackINFOFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- f.op.SendINFO(ctx, playauth.DeviceSIPINFOCommand{Action: "pause"}) }()
	info, _ := readCleanupRequest(t, f.peer)
	require.Equal(t, sip.INFO, info.Method)
	guard, err := f.barrier.LockTransfer(ctx, 1)
	require.NoError(t, err)
	require.NoError(t, f.db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=1").Error)
	guard.Commit(2)
	guard.Release()
	require.Error(t, <-finished)
	ack, _ := readCleanupRequest(t, f.peer)
	require.Equal(t, sip.ACK, ack.Method)
	bye, address := readCleanupRequest(t, f.peer)
	require.Equal(t, sip.BYE, bye.Method)
	require.Equal(t, info.CSeq().SeqNo+1, bye.CSeq().SeqNo)
	stored, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.NotNil(t, stored.Steps[0].KnownBranch.InfoSteps[0].LocalQuiescedAt)
	require.Nil(t, stored.Steps[0].KnownBranch.InfoSteps[0].Response)
	_, err = f.peer.WriteTo([]byte(sip.NewResponseFromRequest(bye, 200, "OK", nil).String()), address)
	require.NoError(t, err)
	require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
	require.Error(t, f.op.SendINFO(ctx, playauth.DeviceSIPINFOCommand{Action: "teardown"}))
	f.noACK(t)
}

func TestPlaybackIntentINFORejectsAmbiguousViaResponse(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	f := acceptedPlaybackINFOFixture(t)
	body, err := (playauth.DeviceSIPINFOCommand{Action: "pause"}).Body(f.op.invite.CSeq + 1)
	require.NoError(t, err)
	r, err := f.op.dua.BuildDialogINFO(f.op.request, f.op.first, f.op.invite.CSeq+1, "Application/MANSRTSP", body)
	require.NoError(t, err)
	snapshot, err := snapshotPlaybackINFORequest(r, f.op.invite.StepID)
	require.NoError(t, err)
	i := playauth.DeviceSIPINFOIdentity{InfoID: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", Request: snapshot}
	response := sip.NewResponseFromRequest(r, 200, "OK", nil)
	response.Via().Params = append(response.Via().Params, sip.HeaderKV{K: "branch", V: "different"})
	_, err = snapshotPlaybackINFOResponse(response, i)
	require.Error(t, err)
}
