package sipgo

import (
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
)

func TestOwnedClientInviteCloseObservationJoinsAndPreservesFacts(t *testing.T) {
	unhandled := make(chan *sip.Response, 1)
	owned, peer, address, first := ownedBranchUDPFixture(t, WithUserAgentTransactionLayerOptions(
		sip.WithTransactionLayerUnhandledResponseHandler(func(r *sip.Response) { unhandled <- r }),
	))
	connection := owned.Transaction().Connection()
	connection.Ref(1)
	defer connection.TryClose()
	owned.Terminate()
	waitOwnedInvite(t, owned.Quiesced())
	before := owned.ObservedBranches()
	var wg sync.WaitGroup
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func() { defer wg.Done(); owned.CloseObservation() }()
	}
	wg.Wait()
	waitOwnedInvite(t, owned.ObservationDone())
	require.Equal(t, before, owned.ObservedBranches())
	require.True(t, owned.QuarantinedBranches().Incomplete)
	late := first.Clone()
	late.To().Params.Add("tag", "after-observer-close")
	_, err := peer.WriteTo([]byte(late.String()), address)
	require.NoError(t, err)
	select {
	case <-unhandled: // Shared transport remains alive; the observer is gone.
	case <-time.After(time.Second):
		t.Fatal("closing one observation must not close the shared UA")
	}
	require.Empty(t, owned.QuarantinedBranches().Responses)
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(30*time.Millisecond)))
	_, _, err = peer.ReadFrom(make([]byte, 8192))
	require.Error(t, err, "observation close never sends SIP")
}
