package sip

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type interleavingConnection struct{ addr net.Addr }

func (c *interleavingConnection) LocalAddr() net.Addr    { return c.addr }
func (c *interleavingConnection) WriteMsg(Message) error { return nil }
func (c *interleavingConnection) Ref(int) int            { return 1 }
func (c *interleavingConnection) TryClose() (int, error) { return 0, nil }
func (c *interleavingConnection) Close() error           { return nil }

type blockingWriteConnection struct {
	addr    net.Addr
	started chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func (c *blockingWriteConnection) LocalAddr() net.Addr    { return c.addr }
func (c *blockingWriteConnection) Ref(int) int            { return 1 }
func (c *blockingWriteConnection) TryClose() (int, error) { return 1, nil }
func (c *blockingWriteConnection) WriteMsg(Message) error {
	select {
	case <-c.started:
	default:
		close(c.started)
	}
	<-c.closed
	return nil
}
func (c *blockingWriteConnection) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

type interleavingAdmission struct {
	mu       sync.Mutex
	active   int
	closed   bool
	done     chan struct{}
	reserved chan struct{}
	begin    chan struct{}
}

type interleavingLease struct {
	owner *interleavingAdmission
	once  sync.Once
}

func (l *interleavingLease) Release() {
	if l == nil || l.owner == nil {
		return
	}
	l.once.Do(l.owner.release)
}

func (a *interleavingAdmission) ReserveBusiness(_ *Request, _ ServerTransaction) (RequestLease, bool) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil, false
	}
	if a.done == nil {
		a.done = make(chan struct{})
	}
	a.active++
	a.mu.Unlock()
	select {
	case <-a.reserved:
	default:
		close(a.reserved)
	}
	lease := &interleavingLease{owner: a}
	return lease, true
}

func (a *interleavingAdmission) BeginBusiness(_ *Request, tx ServerTransaction) bool {
	if tx == nil || tx.(interface{ RequestLease() RequestLease }).RequestLease() == nil {
		return false
	}
	select {
	case <-a.begin:
	default:
		close(a.begin)
	}
	return true
}

func (a *interleavingAdmission) EndBusiness() {}

func (a *interleavingAdmission) close() {
	a.mu.Lock()
	if !a.closed {
		a.closed = true
		if a.done == nil {
			a.done = make(chan struct{})
		}
		if a.active == 0 {
			close(a.done)
		}
	}
	a.mu.Unlock()
}

func (a *interleavingAdmission) release() {
	a.mu.Lock()
	a.active--
	if a.closed && a.active == 0 {
		close(a.done)
	}
	a.mu.Unlock()
}

func (a *interleavingAdmission) wait(ctx context.Context) error {
	a.mu.Lock()
	done := a.done
	a.mu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestLoggingTransactionAdmissionReservationSurvivesQuiesceBeforeDispatch(t *testing.T) {
	tp := NewTransportLayer(net.DefaultResolver, NewParser(), nil)
	txl := NewTransactionLayer(tp)
	defer tp.Close()

	source := "127.0.0.1:5061"
	tp.udp.pool.Add(source, &interleavingConnection{addr: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5060}})

	admission := &interleavingAdmission{
		reserved: make(chan struct{}),
		begin:    make(chan struct{}),
	}
	txl.SetRequestLifecycle(admission)
	handlerStarted := make(chan struct{})
	handlerRelease := make(chan struct{})
	handlerDone := make(chan struct{})
	var handlerCalls atomic.Int32
	txl.OnRequest(func(req *Request, tx *ServerTx) {
		handlerCalls.Add(1)
		close(handlerStarted)
		<-handlerRelease
		if admission.BeginBusiness(req, tx) {
			// Match the outer server's business scope. The transaction layer's
			// defer performs the independent fallback release as well.
			tx.RequestLease().Release()
		}
		close(handlerDone)
	})

	req := testCreateRequest(t, "OPTIONS", "sip:127.0.0.1:5060", "UDP", source)
	req.SetSource(source)
	txl.handleMessage(req)
	<-admission.reserved
	<-handlerStarted

	// The transaction is already in the store while dispatch is paused in the
	// request handler. Quiescing must close new admission and retain its lease.
	txl.QuiesceRequests()
	admission.close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	require.ErrorIs(t, admission.wait(ctx), context.DeadlineExceeded)
	cancel()

	newReq := testCreateRequest(t, "OPTIONS", "sip:127.0.0.1:5060", "UDP", source)
	newReq.SetSource(source)
	require.NoError(t, txl.handleRequest(newReq))
	require.EqualValues(t, 1, handlerCalls.Load())

	close(handlerRelease)
	<-handlerDone
	require.NoError(t, admission.wait(context.Background()))

	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, txl.CloseContext(ctx))
}

func TestLoggingTransactionLayerQuiesceDropsOnlyNewUnmatchedRequests(t *testing.T) {
	tp := NewTransportLayer(net.DefaultResolver, NewParser(), nil)
	txl := NewTransactionLayer(tp)
	defer tp.Close()

	var handlerCalls atomic.Int32
	txl.OnRequest(func(*Request, *ServerTx) { handlerCalls.Add(1) })
	txl.QuiesceRequests()

	req := testCreateRequest(t, "OPTIONS", "sip:127.0.0.1:5060", "UDP", "127.0.0.1:5061")
	require.NoError(t, txl.serverTxRequest(req, "unmatched-after-quiesce"))
	require.Zero(t, handlerCalls.Load())

	// A transaction admitted before quiesce remains routable for its matching
	// retransmission path.
	key := "existing-before-quiesce"
	tx := NewServerTx(key, req, nil, slog.Default(), txl.fsmWork)
	require.NoError(t, tx.Init())
	txl.serverTransactions.items[key] = tx
	require.NoError(t, txl.serverTxRequest(req, key))
	tx.Terminate()
}

func TestLoggingTransactionLayerCloseContextReleasesUDPGracefulDispatch(t *testing.T) {
	tp := NewTransportLayer(net.DefaultResolver, NewParser(), nil)
	txl := NewTransactionLayer(tp)
	defer tp.Close()

	req, _, _ := testCreateInvite(t, "sip:127.0.0.1:5060", "UDP", "127.0.0.1:5061")
	tx := NewServerTx("graceful-udp", req, nil, slog.Default(), txl.fsmWork)
	require.NoError(t, tx.Init())
	tx.fsmMu.Lock()
	tx.fsmResp = NewResponseFromRequest(req, StatusOK, "OK", nil)
	tx.fsmMu.Unlock()
	txl.serverTransactions.items[tx.Key()] = tx

	started := make(chan struct{})
	finished := make(chan struct{})
	require.True(t, txl.dispatchWork.goRun(func() {
		close(started)
		tx.TerminateGracefully()
		close(finished)
	}))
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, txl.CloseContext(ctx))
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("graceful UDP dispatch did not join after transaction close")
	}
}

func TestLoggingTransportCloseUnblocksTransactionFSMBeforeJoin(t *testing.T) {
	tp := NewTransportLayer(net.DefaultResolver, NewParser(), nil)
	txl := NewTransactionLayer(tp)

	conn := &blockingWriteConnection{
		addr:    &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5060},
		started: make(chan struct{}),
		closed:  make(chan struct{}),
	}
	tp.udp.pool.Add("blocking-write", conn)
	req, _, _ := testCreateInvite(t, "sip:127.0.0.1:5060", "UDP", "127.0.0.1:5061")
	tx := NewServerTx("blocking-write", req, conn, slog.Default(), txl.fsmWork)
	require.NoError(t, tx.Init())
	txl.serverTransactions.items[tx.Key()] = tx
	tx.OnTerminate(txl.serverTxTerminate)

	respondDone := make(chan error, 1)
	go func() {
		respondDone <- tx.Respond(NewResponseFromRequest(req, StatusOK, "OK", nil))
	}()
	<-conn.started

	closeDone := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = txl.CloseContext(ctx)
		close(closeDone)
	}()
	select {
	case <-closeDone:
		t.Fatal("transaction join completed before the blocked transport write was closed")
	case <-time.After(20 * time.Millisecond):
	}

	// A transaction FSM may hold fsmMu while writing. Closing the transport is
	// the release signal; only then can Terminate and the lifecycle join finish.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	require.NoError(t, tp.CloseContext(ctx))
	cancel()
	require.NoError(t, <-respondDone)
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("transaction lifecycle did not join after transport close")
	}
}

func TestLoggingTransportCloseContextJoinsLoopbackReaders(t *testing.T) {
	t.Run("tcp", func(t *testing.T) {
		tp := NewTransportLayer(net.DefaultResolver, NewParser(), nil)
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		require.NoError(t, err)
		serveDone := make(chan error, 1)
		go func() { serveDone <- tp.ServeTCP(listener) }()

		conn, err := net.Dial("tcp4", listener.Addr().String())
		require.NoError(t, err)
		require.Eventually(t, func() bool { return tp.tcp.pool.Size() > 0 }, time.Second, time.Millisecond)

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, tp.CloseContext(ctx))
		select {
		case <-serveDone:
		case <-time.After(time.Second):
			t.Fatal("TCP Serve did not return after CloseContext")
		}
		_ = conn.Close()
	})

	t.Run("udp", func(t *testing.T) {
		tp := NewTransportLayer(net.DefaultResolver, NewParser(), nil)
		listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
		require.NoError(t, err)
		serveDone := make(chan error, 1)
		go func() { serveDone <- tp.ServeUDP(listener) }()
		require.Eventually(t, func() bool { return tp.udp.pool.Size() > 0 }, time.Second, time.Millisecond)

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, tp.CloseContext(ctx))
		select {
		case <-serveDone:
		case <-time.After(time.Second):
			t.Fatal("UDP Serve did not return after CloseContext")
		}
	})
}

func TestLoggingLifecycleGateWaitReturnsContextErrorUntilWorkFinishes(t *testing.T) {
	var gate lifecycleGate
	started := make(chan struct{})
	release := make(chan struct{})
	require.True(t, gate.goRun(func() {
		close(started)
		<-release
	}))
	<-started
	gate.close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, gate.wait(ctx), context.Canceled)
	close(release)
	require.NoError(t, gate.wait(context.Background()))
	require.False(t, gate.goRun(func() {}))
	require.NoError(t, gate.wait(context.Background()))
}
