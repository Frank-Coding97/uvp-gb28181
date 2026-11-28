package sipgo

import (
	"context"
	"log/slog"
	"testing"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
)

func TestOwnedFixedCleanupActualUDP(t *testing.T) { testOwnedBranchCleanupActualUDP(t, true) }
func TestOwnedFixedCleanupActualTCP(t *testing.T) { testOwnedBranchCleanupActualTCP(t, true) }

func TestOwnedFixedCleanupPreservesRequestsWithoutDispatch(t *testing.T) {
	dua, invite := ownedInviteRequest(t, "127.0.0.1:5060")
	built, err := dua.NewBranchCleanup(invite, cleanupBranchResponse(invite, 5060), invite.CSeq().SeqNo+1)
	require.NoError(t, err)
	defer built.Terminate()
	ack, bye := built.ACKRequest(), built.BYERequest()
	wantACK, wantBYE := ack.String(), bye.String()
	owner, err := dua.NewFixedBranchCleanup(ack, bye)
	require.NoError(t, err)
	defer owner.Terminate()
	require.Nil(t, owner.Transaction())
	require.Equal(t, wantACK, owner.ACKRequest().String())
	require.Equal(t, wantBYE, owner.BYERequest().String())
	require.Error(t, owner.WriteACK())
	require.Error(t, owner.StartBYE())
	*ack.CallID() = "caller-mutated"
	bye.Via().Params.Add("branch", "caller-mutated")
	require.Equal(t, wantACK, owner.ACKRequest().String())
	require.Equal(t, wantBYE, owner.BYERequest().String())
	conn := &ownedInviteConnection{}
	_, err = owner.prepareBYE(context.Background(), func(_ context.Context, request *sip.Request) (*sip.ClientTx, error) {
		require.Equal(t, wantBYE, request.String())
		return sip.NewClientTx("fixed-cleanup", request, conn, slog.Default()), nil
	})
	require.NoError(t, err)
	require.Zero(t, conn.writes.Load(), "fixed construction and transaction preparation never write SIP")
	require.NoError(t, owner.WriteACK())
	require.NoError(t, owner.StartBYE())
	owner.Terminate()
	waitOwnedInvite(t, owner.Quiesced())
	require.EqualValues(t, 2, conn.writes.Load())
}

func TestOwnedFixedCleanupRejectsChangedPair(t *testing.T) {
	dua, invite := ownedInviteRequest(t, "127.0.0.1:5060")
	built, err := dua.NewBranchCleanup(invite, cleanupBranchResponse(invite, 5060), invite.CSeq().SeqNo+1)
	require.NoError(t, err)
	defer built.Terminate()
	for name, mutate := range map[string]func(*sip.Request, *sip.Request){
		"method":            func(a, b *sip.Request) { a.Method = sip.INVITE },
		"sequence":          func(a, b *sip.Request) { b.CSeq().SeqNo = a.CSeq().SeqNo },
		"call-id":           func(a, b *sip.Request) { *b.CallID() = "different" },
		"remote-tag":        func(a, b *sip.Request) { b.To().Params.Add("tag", "different") },
		"recipient":         func(a, b *sip.Request) { b.Recipient.Host = "192.0.2.1" },
		"destination":       func(a, b *sip.Request) { b.SetDestination("192.0.2.1:5060") },
		"branch-reuse":      func(a, b *sip.Request) { b.Via().Params.Add("branch", a.Via().Params.GetOr("branch", "")) },
		"via-host":          func(a, b *sip.Request) { b.Via().Host = "192.0.2.1" },
		"duplicate-contact": func(a, b *sip.Request) { b.AppendHeader(b.Contact().Clone()) },
		"missing-length":    func(a, b *sip.Request) { b.RemoveHeader("Content-Length") },
		"secret":            func(a, b *sip.Request) { b.AppendHeader(sip.NewHeader("Authorization", "fixture-secret")) },
		"body":              func(a, b *sip.Request) { b.SetBody([]byte("not-empty")) },
		"route":             func(a, b *sip.Request) { b.AppendHeader(sip.NewHeader("Route", "<sip:proxy.example;lr>")) },
		"transport":         func(a, b *sip.Request) { b.SetTransport("TCP") },
	} {
		t.Run(name, func(t *testing.T) {
			a, b := built.ACKRequest(), built.BYERequest()
			mutate(a, b)
			owner, err := dua.NewFixedBranchCleanup(a, b)
			require.ErrorIs(t, err, ErrOwnedCleanupState)
			require.Nil(t, owner)
		})
	}
}
