package uac

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

func acceptedPlaybackINFOFixture(t *testing.T) *playbackOperationUDPFixture {
	t.Helper()
	f := newPlaybackOperationUDPFixture(t)
	f.respond(t, 200)
	_, err := f.op.ReadAndAccept(context.Background())
	require.NoError(t, err)
	ack, _ := readCleanupRequest(t, f.peer)
	require.Equal(t, sip.ACK, ack.Method)
	return f
}

func TestPlaybackIntentINFOActualUDPAndCleanupShareDurableSequence(t *testing.T) {
	f := acceptedPlaybackINFOFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	last := f.op.invite.CSeq
	commands := []playauth.DeviceSIPINFOCommand{{Action: "play"}, {Action: "pause"}, {Action: "resume"},
		{Action: "seek", PositionNanos: int64(time.Second), SegmentDurationNanos: int64(time.Minute)}, {Action: "scale", Scale: 2}, {Action: "teardown"}}
	for _, command := range commands {
		finished := make(chan error, 1)
		go func() { finished <- f.op.SendINFO(ctx, command) }()
		info, address := readCleanupRequest(t, f.peer)
		require.Equal(t, sip.INFO, info.Method)
		require.Equal(t, last+1, info.CSeq().SeqNo)
		last++
		require.Contains(t, string(info.Body()), fmt.Sprintf("CSeq: %d\r\n", last))
		stored, err := f.store.LoadSIPInviteSteps(ctx, f.id)
		require.NoError(t, err)
		steps := stored.Steps[0].KnownBranch.InfoSteps
		a := steps[len(steps)-1]
		require.Equal(t, playauth.SIPStepMayHaveDispatched, a.State)
		require.Equal(t, last, a.Identity.Request.Request.CSeq)
		require.Nil(t, a.LocalQuiescedAt)
		_, err = f.peer.WriteTo([]byte(sip.NewResponseFromRequest(info, 200, "OK", nil).String()), address)
		require.NoError(t, err)
		require.NoError(t, <-finished)
	}
	finished := make(chan error, 1)
	go func() { finished <- f.op.CleanupKnownBranch(ctx) }()
	ack, _ := readCleanupRequest(t, f.peer)
	require.Equal(t, f.op.invite.CSeq, ack.CSeq().SeqNo)
	bye, address := readCleanupRequest(t, f.peer)
	require.Equal(t, sip.BYE, bye.Method)
	require.Equal(t, last+1, bye.CSeq().SeqNo)
	_, err := f.peer.WriteTo([]byte(sip.NewResponseFromRequest(bye, 200, "OK", nil).String()), address)
	require.NoError(t, err)
	require.NoError(t, <-finished)
	require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
	stored, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.Len(t, stored.Steps[0].KnownBranch.InfoSteps, len(commands))
	for _, step := range stored.Steps[0].KnownBranch.InfoSteps {
		require.NotNil(t, step.Response)
		require.NotNil(t, step.LocalQuiescedAt)
	}
	require.Equal(t, playauth.IntentDispatched, stored.Intent.State)
	require.Error(t, f.op.SendINFO(ctx, playauth.DeviceSIPINFOCommand{Action: "pause"}))
	f.noACK(t)
}

func TestPlaybackIntentINFORequiresActualOriginalACK(t *testing.T) {
	f := newPlaybackOperationUDPFixture(t)
	awaitCleanupFirstBranch(t, f)
	stored, err := observeStoredPlaybackBranch(context.Background(), f.store, f.id, f.op.version, f.op.invite, f.op.request, f.op.first)
	require.NoError(t, err)
	stored, err = f.store.DispatchSIPKnownBranchACK(context.Background(), f.id, stored.Intent.RowVersion, stored.Steps[0].KnownBranch.Identity)
	require.NoError(t, err)
	f.op.version = stored.Intent.RowVersion
	require.Error(t, f.op.SendINFO(context.Background(), playauth.DeviceSIPINFOCommand{Action: "pause"}))
	f.noACK(t)
}

func TestPlaybackIntentINFOCommitUnknownCannotSendOrResend(t *testing.T) {
	for _, stage := range []int32{1, 2, 3, 4} {
		for _, committed := range []bool{false, true} {
			t.Run(fmt.Sprintf("stage=%d/commit=%v", stage, committed), func(t *testing.T) {
				f := acceptedPlaybackINFOFixture(t)
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				faultDB := f.db.Session(&gorm.Session{NewDB: true, Context: ctx})
				faultDB.Statement.ConnPool = &playbackOperationCommitFault{ConnPool: f.db.Statement.ConnPool, failAt: stage, commitFirst: committed}
				f.op.store = playauth.NewDeviceOperationIntentStore(faultDB)
				var creates atomic.Int32
				finished := make(chan error, 1)
				go func() {
					finished <- f.op.sendINFO(ctx, playauth.DeviceSIPINFOCommand{Action: "pause"}, func(ctx context.Context, r *sip.Request) (*sip.ClientTx, error) {
						creates.Add(1)
						return f.u.client.TransactionLayer().NewClientTransaction(ctx, r)
					})
				}()
				if stage >= 3 {
					info, address := readCleanupRequest(t, f.peer)
					require.Equal(t, sip.INFO, info.Method)
					_, err := f.peer.WriteTo([]byte(sip.NewResponseFromRequest(info, 200, "OK", nil).String()), address)
					require.NoError(t, err)
				}
				require.Error(t, <-finished)
				if stage <= 2 {
					require.Zero(t, creates.Load(), "unknown prepare/dispatch cannot even connect TCP")
				} else {
					require.EqualValues(t, 1, creates.Load())
				}
				require.Error(t, f.op.SendINFO(ctx, playauth.DeviceSIPINFOCommand{Action: "pause"}))
				require.NoError(t, f.op.CloseLocal(ctx), "observation retry joins and persists without sending")
				require.NoError(t, f.barrier.WaitBefore(ctx, 1, 2))
				f.noACK(t)
			})
		}
	}
}
