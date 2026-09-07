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

type ownedInviteConnection struct {
	entered, release chan struct{}
	writes           atomic.Int32
	writeErr         error
}

func (c *ownedInviteConnection) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5061}
}
func (c *ownedInviteConnection) WriteMsg(sip.Message) error {
	c.writes.Add(1)
	if c.entered != nil {
		close(c.entered)
		<-c.release
	}
	return c.writeErr
}
func (c *ownedInviteConnection) Ref(n int) int          { return n }
func (c *ownedInviteConnection) Close() error           { return nil }
func (c *ownedInviteConnection) TryClose() (int, error) { return 0, nil }

func ownedInviteRequest(t *testing.T, destination string) (*DialogUA, *sip.Request) {
	t.Helper()
	ua, err := NewUA()
	require.NoError(t, err)
	t.Cleanup(func() { _ = ua.Close() })
	client, err := NewClient(ua)
	require.NoError(t, err)
	req := sip.NewRequest(sip.INVITE, sip.Uri{User: "device", Host: "127.0.0.1", Port: 5060})
	req.AppendHeader(&sip.ContactHeader{Address: sip.Uri{User: "platform", Host: "127.0.0.1", Port: 5061}})
	params := sip.NewParams()
	params.Add("branch", sip.GenerateBranchN(16))
	req.AppendHeader(&sip.ViaHeader{ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: "UDP", Host: "127.0.0.1", Port: 5061, Params: params})
	req.SetTransport("UDP")
	req.SetDestination(destination)
	req.SetBody([]byte("fixture SDP"))
	require.NoError(t, ClientRequestBuild(client, req))
	return &DialogUA{Client: client, ContactHDR: *req.Contact()}, req
}

func waitOwnedInvite(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("owned invite did not leave local work")
	}
}

func TestOwnedClientInviteRetainsBlockedInitialWrite(t *testing.T) {
	dua, req := ownedInviteRequest(t, "127.0.0.1:5060")
	conn := &ownedInviteConnection{entered: make(chan struct{}), release: make(chan struct{})}
	tx := sip.NewClientTx("owned-start", req, conn, slog.Default())
	owned := newOwnedClientInvite(dua, req, tx)
	require.Same(t, tx, owned.Transaction())
	var release sync.Once
	defer release.Do(func() { close(conn.release) })
	started := make(chan error, 1)
	go func() { started <- owned.Start() }()
	waitOwnedInvite(t, conn.entered)
	owned.Terminate()
	waitOwnedInvite(t, tx.Done())
	select {
	case <-owned.Quiesced():
		t.Fatal("initial write still entered")
	default:
	}
	release.Do(func() { close(conn.release) })
	require.Error(t, <-started)
	waitOwnedInvite(t, owned.Quiesced())
	require.Error(t, owned.Start())
	require.EqualValues(t, 1, conn.writes.Load())
}

func TestOwnedClientInviteRetainsFailedInitialWrite(t *testing.T) {
	dua, req := ownedInviteRequest(t, "127.0.0.1:5060")
	conn := &ownedInviteConnection{writeErr: errors.New("fixture partial write")}
	tx := sip.NewClientTx("owned-failure", req, conn, slog.Default())
	owned := newOwnedClientInvite(dua, req, tx)
	require.Error(t, owned.Start())
	require.Same(t, tx, owned.Transaction())
	owned.Terminate()
	waitOwnedInvite(t, owned.Quiesced())
	require.Error(t, owned.Start())
	require.EqualValues(t, 1, conn.writes.Load())
}

func TestOwnedClientInvitePreparationHasNoSIPWrite(t *testing.T) {
	peer, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer peer.Close()
	dua, req := ownedInviteRequest(t, peer.LocalAddr().String())
	owned, err := dua.PrepareWriteInviteOwned(context.Background(), req)
	require.NoError(t, err)
	defer owned.Terminate()
	require.NotNil(t, owned.Transaction())
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(20*time.Millisecond)))
	buffer := make([]byte, 4096)
	_, _, err = peer.ReadFrom(buffer)
	require.Error(t, err, "preparation must not send a SIP packet")
	require.NoError(t, owned.Start())
	require.Error(t, owned.Start(), "Start is not a retry API")
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
	n, _, err := peer.ReadFrom(buffer)
	require.NoError(t, err)
	message, err := sip.ParseMessage(buffer[:n])
	require.NoError(t, err)
	require.Equal(t, req.String(), message.String())
	owned.Terminate()
	waitOwnedInvite(t, owned.Quiesced())
}

func TestOwnedClientInviteExactBranchACKOnActualUDP(t *testing.T) {
	peer, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer peer.Close()
	dua, req := ownedInviteRequest(t, peer.LocalAddr().String())
	owned, err := dua.PrepareWriteInviteOwned(context.Background(), req)
	require.NoError(t, err)
	defer owned.Terminate()
	require.NoError(t, owned.Start())
	buffer := make([]byte, 4096)
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
	n, addr, err := peer.ReadFrom(buffer)
	require.NoError(t, err)
	message, err := sip.ParseMessage(buffer[:n])
	require.NoError(t, err)
	invite := message.(*sip.Request)
	response := sip.NewResponseFromRequest(invite, 202, "Accepted", nil)
	response.To().Params.Add("tag", "owned-first")
	udpAddr := peer.LocalAddr().(*net.UDPAddr)
	response.AppendHeader(&sip.ContactHeader{Address: sip.Uri{User: "device", Host: "127.0.0.1", Port: udpAddr.Port}})
	_, err = peer.WriteTo([]byte(response.String()), addr)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := owned.NextResponse(ctx)
	require.NoError(t, err)
	// Receiving a response alone must not ACK or publish a confirmed dialog.
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(20*time.Millisecond)))
	_, _, err = peer.ReadFrom(buffer)
	require.Error(t, err)
	require.NoError(t, owned.AcceptResponse(got))
	ack, err := owned.PrepareACK()
	require.NoError(t, err)
	ack.Recipient.Host = "192.0.2.99" // Caller receives a copy, not the write target.
	require.NoError(t, owned.WritePreparedACK())
	require.Error(t, owned.WritePreparedACK())
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
	n, _, err = peer.ReadFrom(buffer)
	require.NoError(t, err)
	firstACK := string(buffer[:n])
	parsed, err := sip.ParseMessage(buffer[:n])
	require.NoError(t, err)
	require.Equal(t, sip.ACK, parsed.(*sip.Request).Method)
	_, err = peer.WriteTo([]byte(response.String()), addr)
	require.NoError(t, err)
	n, _, err = peer.ReadFrom(buffer)
	require.NoError(t, err)
	require.Equal(t, firstACK, string(buffer[:n]), "protocol retransmission must reuse exact prepared bytes")
	fork := response.Clone()
	fork.To().Params.Add("tag", "owned-other")
	_, err = peer.WriteTo([]byte(fork.String()), addr)
	require.NoError(t, err)
	select {
	case other := <-owned.UnmatchedResponses():
		require.Equal(t, "owned-other", other.To().Params.GetOr("tag", ""))
	case <-ctx.Done():
		t.Fatal("different branch must remain observable")
	}
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(20*time.Millisecond)))
	_, _, err = peer.ReadFrom(buffer)
	require.Error(t, err, "different branch must not receive first branch ACK")
	owned.Terminate()
	waitOwnedInvite(t, owned.Quiesced())
}

func TestOwnedClientInviteResponseCancellationDoesNotSendCANCEL(t *testing.T) {
	dua, req := ownedInviteRequest(t, "127.0.0.1:5060")
	conn := &ownedInviteConnection{}
	tx := sip.NewClientTx("owned-no-implicit-cancel", req, conn, slog.Default())
	owned := newOwnedClientInvite(dua, req, tx)
	defer owned.Terminate()
	require.NoError(t, owned.Start())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := owned.NextResponse(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.EqualValues(t, 1, conn.writes.Load())
	owned.Terminate()
	waitOwnedInvite(t, owned.Quiesced())
}

type ownedACKWriter struct {
	entered, release chan struct{}
	writes           atomic.Int32
}

func (w *ownedACKWriter) Request(context.Context, *sip.Request) (sip.ClientTransaction, error) {
	if w.writes.Add(1) == 1 {
		close(w.entered)
		<-w.release
		return nil, nil
	}
	return nil, errors.New("fixture ACK retransmission error")
}

func TestOwnedClientInviteJoinsExplicitACKAndReportsCallbackFailure(t *testing.T) {
	dua, req := ownedInviteRequest(t, "127.0.0.1:5060")
	tx := sip.NewClientTx("owned-ack-work", req, &ownedInviteConnection{}, slog.Default())
	owned := newOwnedClientInvite(dua, req, tx)
	defer owned.Terminate()
	require.NoError(t, owned.Start())
	response := sip.NewResponseFromRequest(req, 200, "OK", nil)
	response.To().Params.Add("tag", "owned-ack")
	response.AppendHeader(&sip.ContactHeader{Address: sip.Uri{User: "device", Host: "127.0.0.1", Port: 5060}})
	received := make(chan struct{})
	go func() { tx.Receive(response); close(received) }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := owned.NextResponse(ctx)
	require.NoError(t, err)
	waitOwnedInvite(t, received)
	require.NoError(t, owned.AcceptResponse(got))
	_, err = owned.PrepareACK()
	require.NoError(t, err)
	writer := &ownedACKWriter{entered: make(chan struct{}), release: make(chan struct{})}
	// Unit fault injection only. The real preparation API rejects TxRequester.
	dua.Client.TxRequester = writer
	var release sync.Once
	defer release.Do(func() { close(writer.release) })
	first := make(chan error, 1)
	go func() { first <- owned.WritePreparedACK() }()
	waitOwnedInvite(t, writer.entered)
	tx.Receive(response)
	select {
	case err := <-owned.WriteErrors():
		require.ErrorContains(t, err, "fixture ACK")
	case <-ctx.Done():
		t.Fatal("ACK callback failure was lost")
	}
	owned.Terminate()
	waitOwnedInvite(t, tx.Quiesced())
	select {
	case <-owned.Quiesced():
		t.Fatal("explicit ACK write is still entered")
	default:
	}
	release.Do(func() { close(writer.release) })
	require.NoError(t, <-first)
	waitOwnedInvite(t, owned.Quiesced())
	require.EqualValues(t, 2, writer.writes.Load())
}

func TestOwnedClientInviteRejectsUnfixedOrAlternatePreparation(t *testing.T) {
	for _, bad := range []string{"nil-context", "cancelled", "via-port", "contact-port", "tag", "route", "tx-requester", "content-length", "transport"} {
		t.Run(bad, func(t *testing.T) {
			dua, req := ownedInviteRequest(t, "127.0.0.1:5060")
			ctx := context.Background()
			switch bad {
			case "nil-context":
				ctx = nil
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "via-port":
				req.Via().Port = 0
			case "contact-port":
				req.Contact().Address.Port = 0
			case "tag":
				req.From().Params.Remove("tag")
			case "route":
				req.AppendHeader(sip.NewHeader("Route", "<sip:127.0.0.1:5060;lr>"))
			case "tx-requester":
				dua.Client.TxRequester = &ownedACKWriter{}
			case "content-length":
				req.RemoveHeader("Content-Length")
			case "transport":
				req.SetTransport("TLS")
			}
			owned, err := dua.PrepareWriteInviteOwned(ctx, req)
			require.Error(t, err)
			require.Nil(t, owned)
		})
	}
}

func TestOwnedClientInviteTCPPreparationSeparatesDialFromSIPWrite(t *testing.T) {
	peer, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer peer.Close()
	dua, req := ownedInviteRequest(t, peer.Addr().String())
	req.SetTransport("TCP")
	req.Via().Transport = "TCP"
	owned, err := dua.PrepareWriteInviteOwned(context.Background(), req)
	require.NoError(t, err)
	defer owned.Terminate()
	require.NoError(t, peer.(*net.TCPListener).SetDeadline(time.Now().Add(time.Second)))
	connection, err := peer.Accept()
	require.NoError(t, err)
	defer connection.Close()
	require.NoError(t, connection.SetReadDeadline(time.Now().Add(20*time.Millisecond)))
	buffer := make([]byte, len(req.String()))
	_, err = connection.Read(buffer)
	require.Error(t, err, "connection establishment must not send INVITE")
	require.NoError(t, owned.Start())
	require.NoError(t, connection.SetReadDeadline(time.Now().Add(time.Second)))
	_, err = io.ReadFull(connection, buffer)
	require.NoError(t, err)
	require.Equal(t, req.String(), string(buffer))
	owned.Terminate()
	waitOwnedInvite(t, owned.Quiesced())
}
