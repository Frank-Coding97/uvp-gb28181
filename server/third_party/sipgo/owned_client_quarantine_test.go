package sipgo

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
)

func TestOwnedClientInviteLate2xxRetainedAfterTransactionExit(t *testing.T) {
	unhandled := make(chan *sip.Response, 1)
	owned, peer, address, first := ownedBranchUDPFixture(t, WithUserAgentTransactionLayerOptions(
		sip.WithTransactionLayerUnhandledResponseHandler(func(r *sip.Response) { unhandled <- r }),
	))
	// Keep the existing UDP receive socket alive independently of the closed
	// transaction, just as a shared listening transport remains alive.
	connection := owned.Transaction().Connection()
	connection.Ref(1)
	t.Cleanup(func() { _, _ = connection.TryClose() })
	owned.Terminate()
	waitOwnedInvite(t, owned.Quiesced())
	_, err := owned.session.UA.PrepareWriteInviteOwned(context.Background(), owned.Request())
	require.ErrorIs(t, err, sip.ErrClientResponseObservation, "a terminated transaction cannot replace its still-live observer")
	before := owned.ObservedBranches()
	require.Len(t, before.Responses, 1)
	late := first.Clone()
	late.To().Params.Add("tag", "late-after-exit")
	_, err = peer.WriteTo([]byte(late.String()), address)
	require.NoError(t, err)
	select {
	case received := <-unhandled:
		require.Equal(t, "late-after-exit", ownedSingleTag(received.To().Params))
	case <-time.After(time.Second):
		t.Fatal("fixture did not deliver late response through the real transport")
	}
	quarantined := owned.QuarantinedBranches()
	require.Len(t, quarantined.Responses, 1, "parsed late 2xx must remain owned after transaction removal")
	require.Equal(t, "late-after-exit", ownedSingleTag(quarantined.Responses[0].To().Params))
	require.Equal(t, before, owned.ObservedBranches(), "late facts cannot mutate the frozen cleanup batch")
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(30*time.Millisecond)))
	_, _, err = peer.ReadFrom(make([]byte, 8192))
	require.Error(t, err, "late observation has no ACK/BYE authority")
}

func TestOwnedClientInviteQuarantineSharesBoundAndDetachesSnapshots(t *testing.T) {
	owned, _, _, first := ownedBranchUDPFixture(t)
	owned.Terminate()
	waitOwnedInvite(t, owned.Quiesced())
	frozen := owned.ObservedBranches()
	sink := ownedInviteResponseSink{owned}
	for index := 1; index < maxOwnedInviteBranches; index++ {
		r := first.Clone()
		r.To().Params.Add("tag", fmt.Sprintf("late-%d", index))
		sink.CaptureResponse(r)
		sink.CaptureResponse(r)
	}
	snapshot := owned.QuarantinedBranches()
	require.Len(t, snapshot.Responses, 7)
	require.False(t, snapshot.Incomplete)
	want := snapshot.Responses[0].String()
	*snapshot.Responses[0].CallID() = "mutated-caller-copy"
	require.Equal(t, want, owned.QuarantinedBranches().Responses[0].String())
	ninth := first.Clone()
	ninth.To().Params.Add("tag", "ninth")
	sink.CaptureResponse(ninth)
	require.Len(t, owned.QuarantinedBranches().Responses, 7)
	require.True(t, owned.QuarantinedBranches().Incomplete)
	require.Equal(t, frozen, owned.ObservedBranches())
}

func TestOwnedClientInviteQuarantineInvalidConflictAndObservationLoss(t *testing.T) {
	for _, failure := range []string{"conflict", "invalid", "connection", "cancel", "layer-close"} {
		t.Run(failure, func(t *testing.T) {
			owned, _, _, first := ownedBranchUDPFixture(t)
			owned.Terminate()
			waitOwnedInvite(t, owned.Quiesced())
			frozen := owned.ObservedBranches()
			late := first.Clone()
			late.To().Params.Add("tag", "retained-late")
			sink := ownedInviteResponseSink{owned}
			sink.CaptureResponse(late)
			want := owned.QuarantinedBranches().Responses[0].String()
			switch failure {
			case "conflict":
				bad := first.Clone()
				bad.Contact().Address.Port++
				sink.CaptureResponse(bad)
			case "invalid":
				bad := first.Clone()
				bad.RemoveHeader("Contact")
				sink.CaptureResponse(bad)
			case "connection":
				owned.session.UA.Client.TransactionLayer().OnConnectionClose(owned.Transaction().Connection())
			case "cancel":
				owned.responseObservation.Close()
			case "layer-close":
				owned.session.UA.Client.TransactionLayer().Close()
			}
			got := owned.QuarantinedBranches()
			require.True(t, got.Incomplete)
			require.Len(t, got.Responses, 1)
			require.Equal(t, want, got.Responses[0].String())
			require.Equal(t, frozen, owned.ObservedBranches())
		})
	}
}

func TestOwnedClientInviteQuarantineFreezeRaceDoesNotDropFacts(t *testing.T) {
	owned, _, _, first := ownedBranchUDPFixture(t)
	fork := first.Clone()
	fork.To().Params.Add("tag", "racing-freeze")
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); <-start; ownedInviteResponseSink{owned}.CaptureResponse(fork) }()
	go func() { defer wg.Done(); <-start; owned.Terminate() }()
	close(start)
	wg.Wait()
	waitOwnedInvite(t, owned.Quiesced())
	active, quarantine := owned.ObservedBranches(), owned.QuarantinedBranches()
	require.Equal(t, 2, len(active.Responses)+len(quarantine.Responses))
	count := 0
	for _, r := range append(active.Responses, quarantine.Responses...) {
		if ownedSingleTag(r.To().Params) == "racing-freeze" {
			count++
		}
	}
	require.Equal(t, 1, count)
}

func TestOwnedClientInviteQuarantineLossBeforeStartDeniesWrite(t *testing.T) {
	peer, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer peer.Close()
	dua, req := ownedInviteRequest(t, peer.LocalAddr().String())
	owned, err := dua.PrepareWriteInviteOwned(context.Background(), req)
	require.NoError(t, err)
	defer owned.Terminate()
	owned.responseObservation.Close()
	require.True(t, owned.ObservedBranches().Incomplete)
	require.ErrorIs(t, owned.Start(), ErrOwnedInviteState, "lost observation must revoke first-write admission")
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(30*time.Millisecond)))
	_, _, err = peer.ReadFrom(make([]byte, 8192))
	require.Error(t, err, "closed observation cannot permit INVITE")
}
