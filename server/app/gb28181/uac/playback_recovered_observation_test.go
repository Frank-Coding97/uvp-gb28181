package uac

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

func TestPlaybackRecoveredObservationRealUDPAndNoDispatch(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	f, stepID := recoveredPlaybackUDPFixture(t)
	ctx := context.Background()
	o, err := f.u.beginRecoveredPlaybackObservation(ctx, f.store, f.barrier, f.id, stepID)
	require.NoError(t, err)
	defer o.CloseLocal(ctx)
	stored, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.Equal(t, playauth.SIPBranchObserverIncomplete, stored.Steps[0].BranchInventoryFault, "gap is durable before observation starts")
	listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- f.u.client.TransportLayer().ServeUDP(listener) }()
	defer func() { _ = listener.Close(); <-done }()
	// Reuse the exact persisted branch identity only to form peer-side test
	// traffic; production observation never builds a request or fake response.
	response := recoveredObservationResponse(t, stored.Steps[0].Identity, "post-restart")
	_, err = f.peer.WriteTo([]byte(response.String()), listener.LocalAddr())
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		out, err := f.store.LoadSIPInviteSteps(ctx, f.id)
		return err == nil && len(out.Steps[0].AdditionalBranches) == 1
	}, 2*time.Second, time.Millisecond)
	stored, err = f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.Equal(t, "recovery-remote", stored.Steps[0].KnownBranch.Identity.RemoteTag)
	require.Equal(t, "post-restart", stored.Steps[0].AdditionalBranches[0].Identity.RemoteTag)
	require.Empty(t, stored.Steps[0].AdditionalBranches[0].CleanupAttempts)
	require.Equal(t, playauth.SIPStepPrepared, stored.Steps[0].AdditionalBranches[0].ACKState)
	_, err = f.u.beginPlaybackIntentOperation(ctx, f.store, f.barrier, f.id, stored.Intent.RowVersion, strings.Repeat("c", 32), validPlaybackInvite())
	require.Error(t, err, "detached observation still retains original identity against reactivation")
	f.noACK(t)
}

// A peer-side response assembled from test data, never used by product code.
func recoveredObservationResponse(t *testing.T, i playauth.DeviceSIPInviteIdentity, tag string) *sip.Response {
	t.Helper()
	raw := "SIP/2.0 200 OK\r\nVia: SIP/2.0/" + i.ViaTransport + " " + net.JoinHostPort(i.ViaHost, "5061") + ";branch=" + i.Branch + "\r\nFrom: <" + i.FromURI + ">;tag=" + i.LocalTag + "\r\nTo: <" + i.ToURI + ">;tag=" + tag + "\r\nCall-ID: " + i.CallID + "\r\nCSeq: 1 INVITE\r\nContact: <sip:device@127.0.0.1:5062>\r\nContent-Length: 0\r\n\r\n"
	m, err := sip.ParseMessage([]byte(raw))
	require.NoError(t, err)
	r := m.(*sip.Response)
	r.CSeq().SeqNo = i.CSeq
	r.Via().Port = i.ViaPort
	return r
}

func TestPlaybackRecoveredObservationRetriesFactsAndBoundsInventory(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	f, stepID := recoveredPlaybackUDPFixture(t)
	ctx := context.Background()
	o, err := f.u.beginRecoveredPlaybackObservation(ctx, f.store, f.barrier, f.id, stepID)
	require.NoError(t, err)
	defer o.CloseLocal(ctx)
	var blocked atomic.Bool
	blocked.Store(true)
	entered := make(chan struct{}, 1)
	require.NoError(t, f.db.Callback().Update().Before("gorm:update").Register("fixture:recovered-facts", func(db *gorm.DB) {
		if blocked.Load() {
			select {
			case entered <- struct{}{}:
			default:
			}
			db.AddError(errors.New("fixture database unavailable"))
		}
	}))
	defer func() {
		blocked.Store(false)
		_ = o.CloseLocal(ctx)
		_ = f.db.Callback().Update().Remove("fixture:recovered-facts")
	}()
	loaded, err := f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	i := loaded.Steps[0].Identity
	for n := 0; n < 10; n++ {
		response := recoveredObservationResponse(t, i, fmt.Sprintf("late-%d", n))
		o.CaptureResponse(response)
		response.To().Params.Add("tag", "mutated-caller")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("persistence did not attempt the unavailable database")
	}
	o.mu.Lock()
	require.Len(t, o.branches, 7, "known+new inventory has a total bound of eight")
	require.Equal(t, "late-0", o.branches[0].RemoteTag)
	require.Equal(t, "late-6", o.branches[6].RemoteTag)
	o.mu.Unlock()
	loaded, err = f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.Empty(t, loaded.Steps[0].AdditionalBranches)
	blocked.Store(false)
	require.Eventually(t, func() bool {
		out, err := f.store.LoadSIPInviteSteps(ctx, f.id)
		return err == nil && len(out.Steps[0].AdditionalBranches) == 7
	}, 3*time.Second, time.Millisecond, "retry needs no new response or Close call")
	conflict := recoveredObservationResponse(t, i, "late-0")
	conflict.Contact().Address.Port = 9999
	o.CaptureResponse(conflict)
	require.NoError(t, o.flush(ctx))
	loaded, err = f.store.LoadSIPInviteSteps(ctx, f.id)
	require.NoError(t, err)
	require.Equal(t, "sip:device@127.0.0.1:5062", loaded.Steps[0].AdditionalBranches[0].Identity.RemoteTarget)
	require.Equal(t, playauth.SIPBranchObserverIncomplete, loaded.Steps[0].BranchInventoryFault)
	for _, branch := range loaded.Steps[0].AdditionalBranches {
		require.Empty(t, branch.CleanupAttempts)
	}
	f.noACK(t)
}

func TestPlaybackRecoveredObservationSingleRegistrationAndLiveExclusion(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	f, stepID := recoveredPlaybackUDPFixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	winners := make(chan *playbackRecoveredObservation, 20)
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o, err := f.u.beginRecoveredPlaybackObservation(ctx, f.store, f.barrier, f.id, stepID)
			if err == nil {
				winners <- o
			}
		}()
	}
	wg.Wait()
	close(winners)
	require.Len(t, winners, 1)
	o := <-winners
	require.NoError(t, o.CloseLocal(ctx))
	f.u.playbackIntentMu.Lock()
	require.Len(t, f.u.playbackObservations, 1, "local observation shutdown does not erase identity")
	f.u.playbackIntentMu.Unlock()
	f.noACK(t)
}

func TestPlaybackRecoveredObservationExcludesLiveOwner(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	ctx := context.Background()
	live := newPlaybackOperationUDPFixture(t)
	other, err := live.u.beginRecoveredPlaybackObservation(ctx, live.store, live.barrier, live.id, live.op.invite.StepID)
	require.Error(t, err)
	require.Nil(t, other)
}

func TestPlaybackRecoveredObservationGapCommitUnknownDoesNotRegister(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprint(committed), func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}

			f, stepID := recoveredPlaybackUDPFixture(t)
			ctx := context.Background()
			faultDB := authoritytest.CommitFaultDB(t, f.db, 1, committed)
			o, err := f.u.beginRecoveredPlaybackObservation(ctx, newAuthorizedIntentTestStore(t, faultDB), f.barrier, f.id, stepID)
			require.Error(t, err)
			require.Nil(t, o)
			require.Empty(t, f.u.playbackObservations)
			out, err := f.store.LoadSIPInviteSteps(ctx, f.id)
			require.NoError(t, err)
			if committed {
				require.Equal(t, playauth.SIPBranchObserverIncomplete, out.Steps[0].BranchInventoryFault)
			} else {
				require.Empty(t, out.Steps[0].BranchInventoryFault)
			}
			require.Empty(t, out.Steps[0].AdditionalBranches)
			f.noACK(t)
		})
	}
}

func TestPlaybackRecoveredObservationRepeatedLossDoesNotDriveSQL(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	f, stepID := recoveredPlaybackUDPFixture(t)
	ctx := context.Background()
	o, err := f.u.beginRecoveredPlaybackObservation(ctx, f.store, f.barrier, f.id, stepID)
	require.NoError(t, err)
	defer o.CloseLocal(ctx)
	queried := make(chan struct{}, 1)
	require.NoError(t, f.db.Callback().Query().Before("gorm:query").Register("fixture:loss-sql", func(*gorm.DB) {
		select {
		case queried <- struct{}{}:
		default:
		}
	}))
	defer func() {
		_ = o.CloseLocal(ctx)
		_ = f.db.Callback().Query().Remove("fixture:loss-sql")
	}()
	for n := 0; n < 100; n++ {
		o.ObservationLost()
	}
	select {
	case <-queried:
		t.Fatal("already durable incomplete must not turn repeated rejected packets into SQL work")
	case <-time.After(40 * time.Millisecond):
	}
}
