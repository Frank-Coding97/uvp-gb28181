package uac

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/internal/authoritytest"
)

// Preserve message boundaries even when ACK and BYE arrive in one TCP read.
func playbackCleanupTCPReader(t *testing.T, conn net.Conn) func() *sip.Request {
	t.Helper()
	parser := sip.NewParser().NewSIPStream()
	t.Cleanup(parser.Close)
	var pending []sip.Message
	return func() *sip.Request {
		t.Helper()
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
		buffer := make([]byte, 8192)
		for len(pending) == 0 {
			n, err := conn.Read(buffer)
			require.NoError(t, err)
			err = parser.ParseSIPStream(buffer[:n], func(m sip.Message) { pending = append(pending, m) })
			if !errors.Is(err, sip.ErrParseSipPartial) {
				require.NoError(t, err)
			}
		}
		r := pending[0].(*sip.Request)
		pending = pending[1:]
		return r
	}
}

func TestPlaybackIntentMultiBranchActualTCPUsesBranchRoute(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}

	u, db, store, id, _ := playbackIntentStoreFixture(t)
	u.client.TxRequester = nil
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN legacy_revoked_before DATETIME NULL").Error)
	barrier := newAuthorizedBarrierTest(t, db)
	peerA, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer peerA.Close()
	peerB, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer peerB.Close()
	in := validPlaybackInvite()
	in.Destination, in.Transport = peerA.Addr().String(), "TCP"
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	op, err := u.beginPlaybackIntentOperation(ctx, store, barrier, id, 2, strings.Repeat("b", 32), in)
	require.NoError(t, err)
	defer op.CloseLocal(context.Background())
	require.NoError(t, peerA.(*net.TCPListener).SetDeadline(time.Now().Add(time.Second)))
	connA, err := peerA.Accept()
	require.NoError(t, err)
	defer connA.Close()
	readA := playbackCleanupTCPReader(t, connA)
	require.NoError(t, op.Start(ctx))
	invite := readA()
	responseA := sip.NewResponseFromRequest(invite, 200, "OK", nil)
	responseA.To().Params.Add("tag", "branch-A")
	responseA.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: "device", Host: "127.0.0.1", Port: peerA.Addr().(*net.TCPAddr).Port}})
	_, err = connA.Write([]byte(responseA.String()))
	require.NoError(t, err)
	require.Eventually(t, func() bool { op.factMu.Lock(); defer op.factMu.Unlock(); return op.first != nil }, time.Second, time.Millisecond)
	responseB := responseA.Clone()
	responseB.To().Params.Add("tag", "branch-B")
	responseB.Contact().Address.Port = 9 // Not the route: no connection may be made to this target.
	params := sip.NewParams()
	params.Add("lr", "")
	responseB.AppendHeader(&sip.RecordRouteHeader{Address: sip.Uri{Scheme: "sip", Host: "127.0.0.1", Port: peerB.Addr().(*net.TCPAddr).Port, UriParams: params}})
	_, err = connA.Write([]byte(responseB.String()))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(op.owned.ObservedBranches().Responses) == 2 }, time.Second, time.Millisecond)
	finished := make(chan error, 1)
	go func() { finished <- op.CleanupKnownBranch(ctx) }()
	ackA, byeA := readA(), readA()
	require.Equal(t, sip.ACK, ackA.Method)
	require.Equal(t, sip.BYE, byeA.Method)
	require.Equal(t, "branch-A", singlePlaybackBranchTag(byeA.To().Params))
	require.Nil(t, byeA.Route())
	_, err = connA.Write([]byte(sip.NewResponseFromRequest(byeA, 200, "OK", nil).String()))
	require.NoError(t, err)
	require.NoError(t, peerB.(*net.TCPListener).SetDeadline(time.Now().Add(time.Second)))
	connB, err := peerB.Accept()
	require.NoError(t, err)
	defer connB.Close()
	readB := playbackCleanupTCPReader(t, connB)
	ackB, byeB := readB(), readB()
	require.Equal(t, sip.ACK, ackB.Method)
	require.Equal(t, sip.BYE, byeB.Method)
	for _, r := range []*sip.Request{ackB, byeB} {
		require.Equal(t, "branch-B", singlePlaybackBranchTag(r.To().Params))
		require.Equal(t, 9, r.Recipient.Port)
		require.NotNil(t, r.Route())
		require.Equal(t, peerB.Addr().(*net.TCPAddr).Port, r.Route().Address.Port)
	}
	require.Equal(t, ackA.CSeq().SeqNo, ackB.CSeq().SeqNo)
	require.Equal(t, byeA.CSeq().SeqNo, byeB.CSeq().SeqNo)
	_, err = connB.Write([]byte(sip.NewResponseFromRequest(byeB, 200, "OK", nil).String()))
	require.NoError(t, err)
	require.NoError(t, <-finished)
	require.NoError(t, barrier.WaitBefore(ctx, 1, 2))
	stored, err := store.LoadSIPInviteSteps(ctx, id)
	require.NoError(t, err)
	for _, branch := range playbackObservedBranchRecords(stored.Steps[0]) {
		require.Equal(t, playauth.SIPCleanupBYEObserved, branch.CleanupAttempts[0].State)
		require.NotNil(t, branch.CleanupAttempts[0].LocalQuiescedAt)
	}
	require.Equal(t, playauth.IntentDispatched, stored.Intent.State)
}
