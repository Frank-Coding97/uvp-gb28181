package sipgo

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
)

func cleanupBranchResponse(req *sip.Request, port int) *sip.Response {
	r := sip.NewResponseFromRequest(req, 200, "OK", nil)
	r.To().Params.Add("tag", "cleanup-remote")
	r.AppendHeader(&sip.ContactHeader{Address: sip.Uri{User: "device", Host: "127.0.0.1", Port: port}})
	return r
}

func TestOwnedBranchCleanupActualUDP(t *testing.T) {
	testOwnedBranchCleanupActualUDP(t, false)
}

func testOwnedBranchCleanupActualUDP(t *testing.T, fixed bool) {
	t.Helper()
	peer, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer peer.Close()
	dua, req := ownedInviteRequest(t, peer.LocalAddr().String())
	owner, err := dua.NewBranchCleanup(req, cleanupBranchResponse(req, peer.LocalAddr().(*net.UDPAddr).Port), req.CSeq().SeqNo+1)
	require.NoError(t, err)
	if fixed {
		built := owner
		owner, err = dua.NewFixedBranchCleanup(built.ACKRequest(), built.BYERequest())
		built.Terminate()
		require.NoError(t, err)
	}
	defer owner.Terminate()
	ack, bye := owner.ACKRequest(), owner.BYERequest()
	require.Equal(t, sip.ACK, ack.Method)
	require.Equal(t, sip.BYE, bye.Method)
	require.Equal(t, req.CSeq().SeqNo, ack.CSeq().SeqNo)
	require.Equal(t, req.CSeq().SeqNo+1, bye.CSeq().SeqNo)
	require.NotEqual(t, ack.Via().Params.GetOr("branch", ""), bye.Via().Params.GetOr("branch", ""))
	require.Equal(t, req.Contact().Value(), bye.Contact().Value())
	prepared, err := owner.PrepareBYE(context.Background())
	require.NoError(t, err)
	require.Equal(t, bye.String(), prepared.String())
	require.NotNil(t, owner.Transaction())
	require.ErrorIs(t, owner.StartBYE(), ErrOwnedCleanupState)
	buf := make([]byte, 8192)
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(20*time.Millisecond)))
	_, _, err = peer.ReadFrom(buf)
	require.Error(t, err, "build/prepare/premature start send no SIP")
	// All public request observations are private clones.
	owner.ACKRequest().Recipient.Host = "192.0.2.99"
	prepared.Recipient.Host = "192.0.2.99"
	require.NoError(t, owner.WriteACK())
	require.ErrorIs(t, owner.WriteACK(), ErrOwnedCleanupState)
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
	n, _, err := peer.ReadFrom(buf)
	require.NoError(t, err)
	require.Equal(t, ack.String(), string(buf[:n]))
	require.NoError(t, owner.StartBYE())
	require.ErrorIs(t, owner.StartBYE(), ErrOwnedCleanupState)
	n, addr, err := peer.ReadFrom(buf)
	require.NoError(t, err)
	require.Equal(t, bye.String(), string(buf[:n]))
	_, err = peer.WriteTo([]byte(sip.NewResponseFromRequest(bye, 200, "OK", nil).String()), addr)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, err := owner.NextResponse(ctx)
	require.NoError(t, err)
	require.Equal(t, 200, r.StatusCode)
	require.Equal(t, bye.CSeq().SeqNo, r.CSeq().SeqNo)
	owner.Terminate()
	waitOwnedInvite(t, owner.Quiesced())
}

func TestOwnedBranchCleanupRetainsPreparingTransaction(t *testing.T) {
	dua, req := ownedInviteRequest(t, "127.0.0.1:5060")
	owner, err := dua.NewBranchCleanup(req, cleanupBranchResponse(req, 5060), req.CSeq().SeqNo+1)
	require.NoError(t, err)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	conn := &ownedInviteConnection{}
	result := make(chan error, 1)
	go func() {
		_, err := owner.prepareBYE(context.Background(), func(_ context.Context, r *sip.Request) (*sip.ClientTx, error) {
			close(entered)
			<-release
			return sip.NewClientTx("cleanup-preparing", r, conn, slog.Default()), errors.New("late preparation failure")
		})
		result <- err
	}()
	waitOwnedInvite(t, entered)
	owner.Terminate()
	select {
	case <-owner.Quiesced():
		t.Fatal("connection preparation still entered")
	default:
	}
	once.Do(func() { close(release) })
	require.Error(t, <-result)
	waitOwnedInvite(t, owner.Quiesced())
	require.NotNil(t, owner.Transaction())
	require.Zero(t, conn.writes.Load())
	require.Error(t, owner.WriteACK())
	require.Error(t, owner.StartBYE())
}

func TestOwnedBranchCleanupACKFailureCannotStartBYE(t *testing.T) {
	dua, req := ownedInviteRequest(t, "127.0.0.1:5060")
	owner, err := dua.NewBranchCleanup(req, cleanupBranchResponse(req, 5060), req.CSeq().SeqNo+1)
	require.NoError(t, err)
	conn := &ownedInviteConnection{writeErr: errors.New("partial ACK write")}
	_, err = owner.prepareBYE(context.Background(), func(_ context.Context, r *sip.Request) (*sip.ClientTx, error) {
		return sip.NewClientTx("cleanup-ack-failure", r, conn, slog.Default()), nil
	})
	require.NoError(t, err)
	require.Error(t, owner.WriteACK())
	require.Error(t, owner.WriteACK())
	require.Error(t, owner.StartBYE())
	require.EqualValues(t, 1, conn.writes.Load())
	owner.Terminate()
	waitOwnedInvite(t, owner.Quiesced())
}

func TestOwnedBranchCleanupActualTCP(t *testing.T) {
	testOwnedBranchCleanupActualTCP(t, false)
}

func testOwnedBranchCleanupActualTCP(t *testing.T, fixed bool) {
	t.Helper()
	peer, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer peer.Close()
	dua, req := ownedInviteRequest(t, peer.Addr().String())
	req.SetTransport("TCP")
	req.Via().Transport = "TCP"
	owner, err := dua.NewBranchCleanup(req, cleanupBranchResponse(req, peer.Addr().(*net.TCPAddr).Port), req.CSeq().SeqNo+1)
	require.NoError(t, err)
	if fixed {
		built := owner
		owner, err = dua.NewFixedBranchCleanup(built.ACKRequest(), built.BYERequest())
		built.Terminate()
		require.NoError(t, err)
	}
	defer owner.Terminate()
	_, err = owner.PrepareBYE(context.Background())
	require.NoError(t, err)
	require.NoError(t, peer.(*net.TCPListener).SetDeadline(time.Now().Add(time.Second)))
	conn, err := peer.Accept()
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(20*time.Millisecond)))
	_, err = conn.Read(make([]byte, 1))
	require.Error(t, err, "TCP connection exists without SIP dispatch")
	for _, send := range []struct {
		write   func() error
		request *sip.Request
	}{{owner.WriteACK, owner.ACKRequest()}, {owner.StartBYE, owner.BYERequest()}} {
		require.NoError(t, send.write())
		buf := make([]byte, len(send.request.String()))
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
		_, err = io.ReadFull(conn, buf)
		require.NoError(t, err)
		require.Equal(t, send.request.String(), string(buf))
	}
	_, err = conn.Write([]byte(sip.NewResponseFromRequest(owner.BYERequest(), 200, "OK", nil).String()))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := owner.NextResponse(ctx)
	require.NoError(t, err)
	require.Equal(t, 200, response.StatusCode)
	owner.Terminate()
	waitOwnedInvite(t, owner.Quiesced())
}

func cleanupMockOwner(t *testing.T, conn sip.Connection) *OwnedBranchCleanup {
	t.Helper()
	dua, req := ownedInviteRequest(t, "127.0.0.1:5060")
	owner, err := dua.NewBranchCleanup(req, cleanupBranchResponse(req, 5060), req.CSeq().SeqNo+1)
	require.NoError(t, err)
	_, err = owner.prepareBYE(context.Background(), func(_ context.Context, r *sip.Request) (*sip.ClientTx, error) {
		return sip.NewClientTx("cleanup-mock", r, conn, slog.Default()), nil
	})
	require.NoError(t, err)
	t.Cleanup(owner.Terminate)
	return owner
}

func TestOwnedBranchCleanupQuiescedWaitsForExplicitWrites(t *testing.T) {
	for _, stage := range []string{"ACK", "BYE"} {
		t.Run(stage, func(t *testing.T) {
			conn := &ownedInviteConnection{}
			owner := cleanupMockOwner(t, conn)
			write := owner.WriteACK
			if stage == "BYE" {
				require.NoError(t, owner.WriteACK())
				write = owner.StartBYE
			}
			conn.entered, conn.release = make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(conn.release) })
			result := make(chan error, 1)
			go func() { result <- write() }()
			waitOwnedInvite(t, conn.entered)
			owner.Terminate()
			waitOwnedInvite(t, owner.Transaction().Done())
			select {
			case <-owner.Quiesced():
				t.Fatal("explicit write has not returned")
			default:
			}
			once.Do(func() { close(conn.release) })
			<-result
			waitOwnedInvite(t, owner.Quiesced())
			require.Error(t, owner.WriteACK())
			require.Error(t, owner.StartBYE())
		})
	}
}

func TestOwnedBranchCleanupConcurrentWritesOnceOnly(t *testing.T) {
	conn := &ownedInviteConnection{}
	owner := cleanupMockOwner(t, conn)
	for _, send := range []func() error{owner.WriteACK, owner.StartBYE} {
		var wins atomic.Int32
		var wg sync.WaitGroup
		for n := 0; n < 20; n++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if send() == nil {
					wins.Add(1)
				}
			}()
		}
		wg.Wait()
		require.EqualValues(t, 1, wins.Load())
	}
	require.EqualValues(t, 2, conn.writes.Load())
	var wg sync.WaitGroup
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func() { defer wg.Done(); owner.Terminate() }()
	}
	wg.Wait()
	waitOwnedInvite(t, owner.Quiesced())
}

func TestOwnedBranchCleanupFailedBYECannotRetry(t *testing.T) {
	conn := &ownedInviteConnection{}
	owner := cleanupMockOwner(t, conn)
	require.NoError(t, owner.WriteACK())
	conn.writeErr = errors.New("partial BYE write")
	require.Error(t, owner.StartBYE())
	require.Error(t, owner.StartBYE())
	waitOwnedInvite(t, owner.Quiesced())
	require.EqualValues(t, 2, conn.writes.Load())
	require.NotNil(t, owner.Transaction())
}

func TestOwnedBranchCleanupPreparationMutationFailsClosed(t *testing.T) {
	dua, req := ownedInviteRequest(t, "127.0.0.1:5060")
	owner, err := dua.NewBranchCleanup(req, cleanupBranchResponse(req, 5060), req.CSeq().SeqNo+1)
	require.NoError(t, err)
	before := owner.BYERequest().String()
	conn := &ownedInviteConnection{}
	_, err = owner.prepareBYE(context.Background(), func(_ context.Context, r *sip.Request) (*sip.ClientTx, error) {
		r.Via().Port++
		return sip.NewClientTx("cleanup-mutated", r, conn, slog.Default()), nil
	})
	require.ErrorIs(t, err, ErrOwnedCleanupState)
	waitOwnedInvite(t, owner.Quiesced())
	require.Equal(t, before, owner.BYERequest().String())
	require.Error(t, owner.WriteACK())
	require.Zero(t, conn.writes.Load())
}

func TestOwnedBranchCleanupResponseMustMatchAttempt(t *testing.T) {
	for _, kind := range []string{"ok", "481", "call-id", "tag", "cseq", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			owner := cleanupMockOwner(t, &ownedInviteConnection{})
			require.NoError(t, owner.WriteACK())
			require.NoError(t, owner.StartBYE())
			r := sip.NewResponseFromRequest(owner.BYERequest(), 200, "OK", nil)
			switch kind {
			case "481":
				r.StatusCode = 481
			case "call-id":
				*r.CallID() += "other"
			case "tag":
				r.To().Params.Add("tag", "another-branch")
			case "cseq":
				r.CSeq().SeqNo++
			case "duplicate":
				r.AppendHeader(sip.HeaderClone(r.CallID()))
			}
			received := make(chan struct{})
			go func() { owner.Transaction().Receive(r); close(received) }()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			got, err := owner.NextResponse(ctx)
			if kind == "ok" || kind == "481" {
				require.NoError(t, err)
				require.Equal(t, r.StatusCode, got.StatusCode)
			} else {
				require.ErrorIs(t, err, ErrOwnedCleanupState)
				require.Nil(t, got)
			}
			waitOwnedInvite(t, received)
			owner.Terminate()
			waitOwnedInvite(t, owner.Quiesced())
		})
	}
}

func TestOwnedBranchCleanupRequestSnapshotsAreDetached(t *testing.T) {
	dua, req := ownedInviteRequest(t, "127.0.0.1:5060")
	owner, err := dua.NewBranchCleanup(req, cleanupBranchResponse(req, 5060), req.CSeq().SeqNo+1)
	require.NoError(t, err)
	defer owner.Terminate()
	ack, bye := owner.ACKRequest().String(), owner.BYERequest().String()
	*req.CallID() = "changed-source"
	req.From().Params.Add("tag", "changed-source")
	for _, request := range []*sip.Request{owner.ACKRequest(), owner.BYERequest()} {
		*request.CallID() = "changed-observation"
		*request.MaxForwards() = 1
		*request.ContentLength() = 900
		request.CSeq().SeqNo++
		request.To().Params.Add("tag", "changed-observation")
		request.Via().Params.Add("branch", "changed-observation")
		request.Contact().Address.Host = "192.0.2.99"
	}
	require.Equal(t, ack, owner.ACKRequest().String())
	require.Equal(t, bye, owner.BYERequest().String())
}

func TestOwnedBranchCleanupInvalidPreparationHasNoHandle(t *testing.T) {
	for _, kind := range []string{"nil-request", "nil-response", "missing-contact", "wrong-call", "wrong-cseq", "old-bye-cseq", "missing-from", "duplicate-to", "rewrite-contact", "bad-route", "route-header-params", "encrypted-target"} {
		t.Run(kind, func(t *testing.T) {
			dua, req := ownedInviteRequest(t, "127.0.0.1:5060")
			response := cleanupBranchResponse(req, 5060)
			sequence := req.CSeq().SeqNo + 1
			switch kind {
			case "nil-request":
				req = nil
			case "nil-response":
				response = nil
			case "missing-contact":
				response.RemoveHeader("Contact")
			case "wrong-call":
				changed := sip.CallIDHeader("unrelated")
				response.ReplaceHeader(&changed)
			case "wrong-cseq":
				response.CSeq().SeqNo++
			case "old-bye-cseq":
				sequence--
			case "missing-from":
				req.RemoveHeader("From")
			case "duplicate-to":
				response.AppendHeader(sip.HeaderClone(response.To()))
			case "rewrite-contact":
				dua.RewriteContact = true
			case "bad-route":
				response.AppendHeader(sip.NewHeader("Record-Route", "not a SIP address"))
			case "route-header-params":
				response.AppendHeader(sip.NewHeader("Record-Route", "<sip:proxy.example;lr>;tag=wrong"))
			case "encrypted-target":
				response.Contact().Address.Scheme = "sips"
			}
			owner, err := dua.NewBranchCleanup(req, response, sequence)
			require.ErrorIs(t, err, ErrOwnedCleanupState)
			require.Nil(t, owner)
		})
	}
}
