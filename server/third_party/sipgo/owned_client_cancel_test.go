package sipgo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
)

func TestOwnedClientCancelActualUDP(t *testing.T) {
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
	prepared, err := owned.PrepareCancel(context.Background())
	require.NoError(t, err)
	require.NotNil(t, owned.CancelTransaction())
	require.Equal(t, sip.CANCEL, prepared.Method)
	for _, header := range []string{"Via", "From", "To", "Call-ID", "Contact", "Max-Forwards"} {
		require.Equal(t, req.GetHeader(header).Value(), prepared.GetHeader(header).Value())
	}
	require.Equal(t, req.CSeq().SeqNo, prepared.CSeq().SeqNo)
	require.Equal(t, sip.CANCEL, prepared.CSeq().MethodName)
	require.Empty(t, prepared.Body())
	require.Empty(t, prepared.GetHeaders("Content-Type"))
	require.Equal(t, req.Destination(), prepared.Destination())
	require.Error(t, owned.StartCancel(), "no provisional response yet")
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(20*time.Millisecond)))
	_, _, err = peer.ReadFrom(buffer)
	require.Error(t, err, "prepare and premature Start cannot send CANCEL")
	provisional := sip.NewResponseFromRequest(invite, 180, "Ringing", nil)
	_, err = peer.WriteTo([]byte(provisional.String()), addr)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = owned.NextResponse(ctx)
	require.NoError(t, err)
	prepared.Recipient.Host = "192.0.2.9"
	require.NoError(t, owned.StartCancel())
	require.Error(t, owned.StartCancel())
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
	n, _, err = peer.ReadFrom(buffer)
	require.NoError(t, err)
	message, err = sip.ParseMessage(buffer[:n])
	require.NoError(t, err)
	got := message.(*sip.Request)
	require.Equal(t, sip.CANCEL, got.Method)
	require.Equal(t, invite.Recipient.String(), got.Recipient.String())
	response := sip.NewResponseFromRequest(got, 200, "OK", nil)
	_, err = peer.WriteTo([]byte(response.String()), addr)
	require.NoError(t, err)
	result, err := owned.NextCancelResponse(ctx)
	require.NoError(t, err)
	require.Equal(t, 200, result.StatusCode)
	select {
	case <-owned.Quiesced():
		t.Fatal("CANCEL 200 is not INVITE closure")
	default:
	}
	late := sip.NewResponseFromRequest(invite, 200, "OK", nil)
	_, err = peer.WriteTo([]byte(late.String()), addr)
	require.NoError(t, err)
	result, err = owned.NextResponse(ctx)
	require.NoError(t, err)
	require.Equal(t, 200, result.StatusCode, "late success remains observable")
	owned.Terminate()
	waitOwnedInvite(t, owned.Quiesced())
}

func TestOwnedClientCancelFinalObservedBeforeStart(t *testing.T) {
	for _, status := range []int{200, 486} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			dua, req := ownedInviteRequest(t, "127.0.0.1:5060")
			tx := sip.NewClientTx("owned-final", req, &ownedInviteConnection{}, slog.Default())
			owned := newOwnedClientInvite(dua, req, tx)
			defer owned.Terminate()
			require.NoError(t, owned.Start())
			_, err := owned.PrepareCancel(context.Background())
			require.NoError(t, err)
			response := sip.NewResponseFromRequest(req, status, "fixture", nil)
			received := make(chan struct{})
			go func() { tx.Receive(response); close(received) }()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err = owned.NextResponse(ctx)
			require.NoError(t, err)
			waitOwnedInvite(t, received)
			require.Error(t, owned.StartCancel())
			owned.Terminate()
			waitOwnedInvite(t, owned.Quiesced())
		})
	}
}

func TestOwnedClientCancelPreparationRetainsLateTransaction(t *testing.T) {
	for _, terminate := range []bool{false, true} {
		t.Run(fmt.Sprint(terminate), func(t *testing.T) {
			dua, req := ownedInviteRequest(t, "127.0.0.1:5060")
			tx := sip.NewClientTx("owned-late-prepare", req, &ownedInviteConnection{}, slog.Default())
			owned := newOwnedClientInvite(dua, req, tx)
			defer owned.Terminate()
			require.NoError(t, owned.Start())
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			conn := &ownedInviteConnection{}
			prepared := make(chan error, 1)
			go func() {
				_, err := owned.prepareCancel(context.Background(), func(ctx context.Context, r *sip.Request) (*sip.ClientTx, error) {
					close(entered)
					<-release
					return sip.NewClientTx("owned-late-cancel", r, conn, slog.Default()), nil
				})
				prepared <- err
			}()
			waitOwnedInvite(t, entered)
			if terminate {
				owned.Terminate()
				waitOwnedInvite(t, tx.Quiesced())
				select {
				case <-owned.Quiesced():
					t.Fatal("prepare still owns unfinished connection work")
				default:
				}
			} else {
				response := sip.NewResponseFromRequest(req, 200, "OK", nil)
				go tx.Receive(response)
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_, err := owned.NextResponse(ctx)
				require.NoError(t, err)
			}
			releaseOnce.Do(func() { close(release) })
			require.Error(t, <-prepared)
			require.NotNil(t, owned.CancelTransaction(), "late handle must be retained even on error")
			waitOwnedInvite(t, owned.CancelTransaction().Quiesced())
			require.Zero(t, conn.writes.Load())
			owned.Terminate()
			waitOwnedInvite(t, owned.Quiesced())
		})
	}
}

type ownedCancelCloseConnection struct {
	ownedInviteConnection
	closeEntered, closeRelease chan struct{}
}

func (c *ownedCancelCloseConnection) TryClose() (int, error) {
	close(c.closeEntered)
	<-c.closeRelease
	return 0, nil
}

func TestOwnedClientCancelQuiescenceJoinsWriteAndClose(t *testing.T) {
	for _, blockWrite := range []bool{false, true} {
		t.Run(fmt.Sprint(blockWrite), func(t *testing.T) {
			dua, req := ownedInviteRequest(t, "127.0.0.1:5060")
			tx := sip.NewClientTx("owned-child-join", req, &ownedInviteConnection{}, slog.Default())
			owned := newOwnedClientInvite(dua, req, tx)
			require.NoError(t, owned.Start())
			conn := &ownedCancelCloseConnection{closeEntered: make(chan struct{}), closeRelease: make(chan struct{})}
			if blockWrite {
				conn.entered, conn.release = make(chan struct{}), make(chan struct{})
			}
			var writeOnce, closeOnce sync.Once
			defer func() {
				if blockWrite {
					writeOnce.Do(func() { close(conn.release) })
				}
				closeOnce.Do(func() { close(conn.closeRelease) })
				owned.Terminate()
			}()
			_, err := owned.prepareCancel(context.Background(), func(ctx context.Context, r *sip.Request) (*sip.ClientTx, error) {
				return sip.NewClientTx("owned-child-join-cancel", r, conn, slog.Default()), nil
			})
			require.NoError(t, err)
			go tx.Receive(sip.NewResponseFromRequest(req, 180, "Ringing", nil))
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err = owned.NextResponse(ctx)
			require.NoError(t, err)
			started := make(chan error, 1)
			go func() { started <- owned.StartCancel() }()
			if blockWrite {
				waitOwnedInvite(t, conn.entered)
			} else {
				require.NoError(t, <-started)
			}
			terminated := make(chan struct{})
			go func() { owned.Terminate(); close(terminated) }()
			waitOwnedInvite(t, conn.closeEntered)
			waitOwnedInvite(t, owned.CancelTransaction().Done())
			select {
			case <-owned.Quiesced():
				t.Fatal("child close/write still entered")
			default:
			}
			closeOnce.Do(func() { close(conn.closeRelease) })
			if blockWrite {
				select {
				case <-owned.Quiesced():
					t.Fatal("child initial write still entered")
				default:
				}
				writeOnce.Do(func() { close(conn.release) })
				require.Error(t, <-started)
			}
			waitOwnedInvite(t, terminated)
			waitOwnedInvite(t, owned.Quiesced())
			require.EqualValues(t, 1, conn.writes.Load())
		})
	}
}

func TestOwnedClientCancelFailedInitCannotRetry(t *testing.T) {
	dua, req := ownedInviteRequest(t, "127.0.0.1:5060")
	tx := sip.NewClientTx("owned-child-error", req, &ownedInviteConnection{}, slog.Default())
	owned := newOwnedClientInvite(dua, req, tx)
	defer owned.Terminate()
	require.NoError(t, owned.Start())
	conn := &ownedInviteConnection{writeErr: errors.New("fixture partial CANCEL write")}
	_, err := owned.prepareCancel(context.Background(), func(ctx context.Context, r *sip.Request) (*sip.ClientTx, error) {
		return sip.NewClientTx("owned-child-error-cancel", r, conn, slog.Default()), nil
	})
	require.NoError(t, err)
	go tx.Receive(sip.NewResponseFromRequest(req, 180, "Ringing", nil))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = owned.NextResponse(ctx)
	require.NoError(t, err)
	require.Error(t, owned.StartCancel())
	waitOwnedInvite(t, owned.CancelTransaction().Quiesced())
	require.Error(t, owned.StartCancel())
	require.EqualValues(t, 1, conn.writes.Load())
	owned.Terminate()
	waitOwnedInvite(t, owned.Quiesced())
}

func TestOwnedClientCancelActualTCP(t *testing.T) {
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
	require.NoError(t, owned.Start())
	require.NoError(t, connection.SetReadDeadline(time.Now().Add(time.Second)))
	buffer := make([]byte, len(req.String()))
	_, err = io.ReadFull(connection, buffer)
	require.NoError(t, err)
	prepared, err := owned.PrepareCancel(context.Background())
	require.NoError(t, err)
	require.NoError(t, connection.SetReadDeadline(time.Now().Add(20*time.Millisecond)))
	_, err = connection.Read(buffer)
	require.Error(t, err, "preparation has no CANCEL bytes")
	_, err = connection.Write([]byte(sip.NewResponseFromRequest(req, 180, "Ringing", nil).String()))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = owned.NextResponse(ctx)
	require.NoError(t, err)
	require.NoError(t, owned.StartCancel())
	buffer = make([]byte, len(prepared.String()))
	require.NoError(t, connection.SetReadDeadline(time.Now().Add(time.Second)))
	_, err = io.ReadFull(connection, buffer)
	require.NoError(t, err)
	require.Equal(t, prepared.String(), string(buffer))
	owned.Terminate()
	waitOwnedInvite(t, owned.Quiesced())
}

func TestOwnedClientCancelConcurrentStartIsOnceOnly(t *testing.T) {
	dua, req := ownedInviteRequest(t, "127.0.0.1:5060")
	tx := sip.NewClientTx("owned-concurrent-cancel", req, &ownedInviteConnection{}, slog.Default())
	owned := newOwnedClientInvite(dua, req, tx)
	defer owned.Terminate()
	require.NoError(t, owned.Start())
	conn := &ownedInviteConnection{}
	_, err := owned.prepareCancel(context.Background(), func(ctx context.Context, r *sip.Request) (*sip.ClientTx, error) {
		return sip.NewClientTx("owned-concurrent-cancel-child", r, conn, slog.Default()), nil
	})
	require.NoError(t, err)
	go tx.Receive(sip.NewResponseFromRequest(req, 180, "Ringing", nil))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = owned.NextResponse(ctx)
	require.NoError(t, err)
	results := make(chan error, 20)
	for n := 0; n < 20; n++ {
		go func() { results <- owned.StartCancel() }()
	}
	wins := 0
	for n := 0; n < 20; n++ {
		if err := <-results; err == nil {
			wins++
		} else {
			require.ErrorIs(t, err, ErrOwnedInviteState)
		}
	}
	require.Equal(t, 1, wins)
	require.EqualValues(t, 1, conn.writes.Load())
	var wg sync.WaitGroup
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func() { defer wg.Done(); owned.Terminate() }()
	}
	wg.Wait()
	waitOwnedInvite(t, owned.Quiesced())
}
