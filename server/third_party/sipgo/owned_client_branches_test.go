package sipgo

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
)

func ownedBranchUDPFixture(t *testing.T) (*OwnedClientInvite, net.PacketConn, net.Addr, *sip.Response) {
	t.Helper()
	peer, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = peer.Close() })
	dua, req := ownedInviteRequest(t, peer.LocalAddr().String())
	owned, err := dua.PrepareWriteInviteOwned(context.Background(), req)
	require.NoError(t, err)
	t.Cleanup(func() { owned.Terminate(); waitOwnedInvite(t, owned.Quiesced()) })
	require.NoError(t, owned.Start())
	buffer := make([]byte, 8192)
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
	n, addr, err := peer.ReadFrom(buffer)
	require.NoError(t, err)
	message, err := sip.ParseMessage(buffer[:n])
	require.NoError(t, err)
	r := sip.NewResponseFromRequest(message.(*sip.Request), 200, "OK", nil)
	r.To().Params.Add("tag", "branch-first")
	r.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: "device", Host: "127.0.0.1", Port: peer.LocalAddr().(*net.UDPAddr).Port}})
	_, err = peer.WriteTo([]byte(r.String()), addr)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	first, err := owned.NextResponse(ctx)
	require.NoError(t, err)
	return owned, peer, addr, first
}

func TestOwnedClientInviteForkBeforeACKIsObserved(t *testing.T) {
	owned, peer, addr, first := ownedBranchUDPFixture(t)
	fork := first.Clone()
	fork.To().Params.Add("tag", "branch-before-ack")
	_, err := peer.WriteTo([]byte(fork.String()), addr)
	require.NoError(t, err)
	select {
	case response := <-owned.UnmatchedResponses():
		require.Equal(t, "branch-before-ack", ownedSingleTag(response.To().Params))
	case <-time.After(100 * time.Millisecond):
		t.Fatal("2xx branch was lost before the first explicit ACK")
	}
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(20*time.Millisecond)))
	_, _, err = peer.ReadFrom(make([]byte, 8192))
	require.Error(t, err, "observation must not send ACK or accept a branch")
}

func TestOwnedClientInviteRetainsBranchesWhenHintsAreNotConsumed(t *testing.T) {
	owned, peer, addr, first := ownedBranchUDPFixture(t)
	for n := 1; n < maxOwnedInviteBranches; n++ {
		r := first.Clone()
		r.To().Params.Add("tag", fmt.Sprintf("branch-%d", n))
		_, err := peer.WriteTo([]byte(r.String()), addr)
		require.NoError(t, err)
	}
	require.Eventually(t, func() bool { return len(owned.ObservedBranches().Responses) == maxOwnedInviteBranches }, time.Second, time.Millisecond)
	before := owned.ObservedBranches()
	require.False(t, before.Incomplete)
	for n := 0; n < 20; n++ {
		_, err := peer.WriteTo([]byte(first.String()), addr)
		require.NoError(t, err)
	}
	// The next distinct branch must not evict an earlier branch merely because
	// the application never consumed either notification channel.
	overflow := first.Clone()
	overflow.To().Params.Add("tag", "branch-overflow")
	_, err := peer.WriteTo([]byte(overflow.String()), addr)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return owned.ObservedBranches().Incomplete }, time.Second, time.Millisecond)
	after := owned.ObservedBranches()
	require.Len(t, after.Responses, len(before.Responses))
	for n := range before.Responses {
		require.Equal(t, before.Responses[n].String(), after.Responses[n].String())
	}
	owned.Terminate()
	waitOwnedInvite(t, owned.Quiesced())
	require.True(t, owned.ObservedBranches().Incomplete, "local exit cannot erase observation loss")
}

func TestOwnedClientInviteBranchSnapshotsAreDetachedAndConflictsSticky(t *testing.T) {
	owned, peer, addr, first := ownedBranchUDPFixture(t)
	before := owned.ObservedBranches()
	require.Len(t, before.Responses, 1)
	want := before.Responses[0].String()
	*before.Responses[0].CallID() = "mutated-call"
	*before.Responses[0].ContentLength() = 100
	before.Responses[0].To().Params.Add("tag", "mutated-tag")
	before.Responses[0].Contact().Address.Host = "192.0.2.9"
	require.Equal(t, want, owned.ObservedBranches().Responses[0].String())
	conflict := first.Clone()
	conflict.Contact().Address.Port++
	_, err := peer.WriteTo([]byte(conflict.String()), addr)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return owned.ObservedBranches().Incomplete }, time.Second, time.Millisecond)
	require.Equal(t, want, owned.ObservedBranches().Responses[0].String(), "same tag cannot replace the original target")
}

func TestOwnedClientInviteInvalidBranchCannotEnterSnapshot(t *testing.T) {
	for _, fault := range []string{"body", "headers", "call-id", "tag"} {
		t.Run(fault, func(t *testing.T) {
			owned, _, _, first := ownedBranchUDPFixture(t)
			bad := cloneOwnedBranchResponse(first)
			switch fault {
			case "body":
				bad.SetBody(make([]byte, 65537))
			case "headers":
				bad.AppendHeader(sip.NewHeader("Extra", strings.Repeat("x", 65537)))
			case "call-id":
				*bad.CallID() = "different"
			case "tag":
				bad.To().Params = nil
			}
			owned.observeBranch(bad, true)
			snapshot := owned.ObservedBranches()
			require.True(t, snapshot.Incomplete)
			require.Len(t, snapshot.Responses, 1)
			require.Equal(t, first.String(), snapshot.Responses[0].String())
		})
	}
}

func TestOwnedClientInviteDuplicatePrimitiveHeadersAreDetached(t *testing.T) {
	_, _, _, response := ownedBranchUDPFixture(t)
	contentType := sip.ContentTypeHeader("application/sdp")
	duplicateType := sip.ContentTypeHeader("application/sdp")
	duplicateLength := sip.ContentLengthHeader(0)
	response.AppendHeader(&contentType)
	response.AppendHeader(&duplicateType)
	response.AppendHeader(&duplicateLength)
	want := response.String()
	copy := cloneOwnedBranchResponse(response)
	for _, h := range copy.GetHeaders("Content-Type") {
		*h.(*sip.ContentTypeHeader) = "mutated"
	}
	for _, h := range copy.GetHeaders("Content-Length") {
		*h.(*sip.ContentLengthHeader) = 99
	}
	require.Equal(t, want, response.String(), "every primitive occurrence must be detached")
}

func TestOwnedClientInviteFirstACKCannotStartAfterForkObservation(t *testing.T) {
	owned, peer, _, first := ownedBranchUDPFixture(t)
	require.NoError(t, owned.AcceptResponse(first))
	_, err := owned.PrepareACK()
	require.NoError(t, err)
	fork := cloneOwnedBranchResponse(first)
	fork.To().Params.Add("tag", "fork-before-write")
	owned.observeBranch(fork, true)
	require.Error(t, owned.WritePreparedACK())
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(20*time.Millisecond)))
	_, _, err = peer.ReadFrom(make([]byte, 8192))
	require.Error(t, err, "fork observation before ACK admission must prevent a business ACK")
}
