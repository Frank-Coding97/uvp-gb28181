package sipgo

import (
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/emiago/sipgo/sip"
)

type businessScopeLifecycle struct {
	begin chan struct{}
	end   chan struct{}
}

func (l *businessScopeLifecycle) BeginBusiness(*sip.Request, sip.ServerTransaction) bool {
	close(l.begin)
	return true
}

func (l *businessScopeLifecycle) EndBusiness() {
	close(l.end)
}

func TestLoggingHandleRequestEndsBusinessBeforeUDPGracefulTail(t *testing.T) {
	ua, err := NewUA()
	require.NoError(t, err)
	defer ua.Close()

	lifecycle := &businessScopeLifecycle{
		begin: make(chan struct{}),
		end:   make(chan struct{}),
	}
	srv, err := NewServer(ua, WithServerRequestLifecycle(lifecycle))
	require.NoError(t, err)

	serverConn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer serverConn.Close()
	clientConn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer clientConn.Close()

	serverAddr := serverConn.LocalAddr().String()
	clientAddr := clientConn.LocalAddr().String()
	req, _, _ := createTestInvite(t, "sip:"+serverAddr, "UDP", clientAddr)
	req.SetSource(clientAddr)
	tx := sip.NewServerTx(
		"business-scope",
		req,
		&sip.UDPConnection{PacketConn: serverConn, PacketAddr: serverAddr},
		slog.Default(),
	)
	require.NoError(t, tx.Init())

	responseErr := make(chan error, 1)
	srv.OnInvite(func(req *sip.Request, tx sip.ServerTransaction) {
		responseErr <- tx.Respond(sip.NewResponseFromRequest(req, sip.StatusOK, "OK", nil))
	})

	handleDone := make(chan struct{})
	go func() {
		srv.handleRequest(req, tx)
		close(handleDone)
	}()

	<-lifecycle.begin
	select {
	case err := <-responseErr:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("final UDP response was not sent")
	}
	<-lifecycle.end

	// EndBusiness is observed before the outer graceful termination wait. The
	// final UDP response keeps Timer J alive until the transaction is closed.
	select {
	case <-tx.Done():
		t.Fatal("UDP transaction terminated before graceful tail was released")
	default:
	}
	select {
	case <-handleDone:
		t.Fatal("handleRequest waited in the business scope")
	case <-time.After(20 * time.Millisecond):
	}

	tx.Terminate()
	select {
	case <-handleDone:
	case <-time.After(time.Second):
		t.Fatal("graceful UDP tail did not finish after transaction close")
	}
}
