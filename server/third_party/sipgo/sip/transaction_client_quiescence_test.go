package sip

import (
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type lifecycleClientConn struct {
	writes       atomic.Int32
	closes       atomic.Int32
	closeEntered chan struct{}
	closeRelease chan struct{}
	writeEntered chan struct{}
	writeRelease chan struct{}
	blockWriteAt int32
}

func (c *lifecycleClientConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5060}
}
func (c *lifecycleClientConn) WriteMsg(Message) error {
	n := c.writes.Add(1)
	if c.writeEntered != nil && (c.blockWriteAt == 0 || c.blockWriteAt == n) {
		close(c.writeEntered)
		<-c.writeRelease
	}
	return nil
}
func (c *lifecycleClientConn) Ref(n int) int { return n }
func (c *lifecycleClientConn) Close() error  { return nil }
func (c *lifecycleClientConn) TryClose() (int, error) {
	c.closes.Add(1)
	if c.closeEntered != nil {
		close(c.closeEntered)
		<-c.closeRelease
	}
	return 0, nil
}

func newLifecycleClientTx(t *testing.T, conn *lifecycleClientConn) (*ClientTx, *Response) {
	t.Helper()
	req, _, _ := testCreateInvite(t, "sip:127.0.0.99:5060", "tcp", "127.0.0.2:5060")
	tx := NewClientTx("lifecycle-fixture", req, conn, slog.Default())
	require.NoError(t, tx.Init())
	t.Cleanup(tx.Terminate)
	return tx, NewResponseFromRequest(req, 200, "OK", nil)
}

func waitClientLifecycle(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("lifecycle fixture did not reach its barrier")
	}
}

func TestClientTransactionQuiescenceRejectsCallbacksAfterTerminate(t *testing.T) {
	conn := &lifecycleClientConn{}
	tx, response := newLifecycleClientTx(t, conn)
	firstDone := make(chan struct{})
	go func() { tx.Receive(response); close(firstDone) }()
	<-tx.Responses()
	waitClientLifecycle(t, firstDone)
	var callbacks atomic.Int32
	require.True(t, tx.OnRetransmission(func(*Response) { callbacks.Add(1) }))
	tx.Terminate()
	waitClientLifecycle(t, tx.Done())
	tx.Receive(response)
	require.Zero(t, callbacks.Load(), "terminated transaction must reject a later response before invoking ACK hooks")
}

func TestClientTransactionQuiescenceWaitsForConnectionRelease(t *testing.T) {
	conn := &lifecycleClientConn{closeEntered: make(chan struct{}), closeRelease: make(chan struct{})}
	tx, _ := newLifecycleClientTx(t, conn)
	var release sync.Once
	defer release.Do(func() { close(conn.closeRelease) })
	terminated := make(chan struct{})
	go func() { tx.Terminate(); close(terminated) }()
	waitClientLifecycle(t, conn.closeEntered)
	waitClientLifecycle(t, tx.Done())
	// A second Terminate is not proof that the first TryClose has returned.
	tx.Terminate()
	observer, ok := any(tx).(interface{ Quiesced() <-chan struct{} })
	require.True(t, ok, "ClientTx needs a separate local-quiescence signal; Done is already closed here")
	select {
	case <-observer.Quiesced():
		t.Fatal("connection release still blocked")
	default:
	}
	release.Do(func() { close(conn.closeRelease) })
	waitClientLifecycle(t, terminated)
	waitClientLifecycle(t, observer.Quiesced())
	require.EqualValues(t, 1, conn.closes.Load())
}

func TestClientTransactionQuiescenceWaitsForEnteredRetransmission(t *testing.T) {
	conn := &lifecycleClientConn{}
	tx, response := newLifecycleClientTx(t, conn)
	firstDone := make(chan struct{})
	go func() { tx.Receive(response); close(firstDone) }()
	<-tx.Responses()
	waitClientLifecycle(t, firstDone)
	entered, release, exited := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	require.True(t, tx.OnRetransmission(func(*Response) { close(entered); <-release }))
	go func() { tx.Receive(response); close(exited) }()
	waitClientLifecycle(t, entered)
	tx.Terminate()
	waitClientLifecycle(t, tx.Done())
	observer, ok := any(tx).(interface{ Quiesced() <-chan struct{} })
	require.True(t, ok, "Terminate returned while an ACK retransmission callback is still entered")
	select {
	case <-observer.Quiesced():
		t.Fatal("callback still running")
	default:
	}
	once.Do(func() { close(release) })
	waitClientLifecycle(t, exited)
	waitClientLifecycle(t, observer.Quiesced())
}

func TestClientTransactionQuiescenceWaitsForTerminationCallback(t *testing.T) {
	conn := &lifecycleClientConn{}
	tx, _ := newLifecycleClientTx(t, conn)
	entered, release, exited := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	require.True(t, tx.OnTerminate(func(string, error) { close(entered); <-release }))
	go func() { tx.Terminate(); close(exited) }()
	waitClientLifecycle(t, entered)
	waitClientLifecycle(t, tx.Done())
	observer, ok := any(tx).(interface{ Quiesced() <-chan struct{} })
	require.True(t, ok)
	select {
	case <-observer.Quiesced():
		t.Fatal("onTerminate still running")
	default:
	}
	once.Do(func() { close(release) })
	waitClientLifecycle(t, exited)
	waitClientLifecycle(t, observer.Quiesced())
	require.EqualValues(t, 1, conn.closes.Load())
}

func TestClientTransactionQuiescenceWaitsForInitialWrite(t *testing.T) {
	conn := &lifecycleClientConn{writeEntered: make(chan struct{}), writeRelease: make(chan struct{})}
	req, _, _ := testCreateInvite(t, "sip:127.0.0.99:5060", "tcp", "127.0.0.2:5060")
	tx := NewClientTx("initial-write-fixture", req, conn, slog.Default())
	var once sync.Once
	defer once.Do(func() { close(conn.writeRelease) })
	initialized := make(chan error, 1)
	go func() { initialized <- tx.Init() }()
	waitClientLifecycle(t, conn.writeEntered)
	tx.Terminate()
	observer, ok := any(tx).(interface{ Quiesced() <-chan struct{} })
	require.True(t, ok)
	select {
	case <-observer.Quiesced():
		t.Fatal("initial write still running")
	default:
	}
	once.Do(func() { close(conn.writeRelease) })
	select {
	case err := <-initialized:
		require.Error(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Init did not exit after write returned")
	}
	waitClientLifecycle(t, observer.Quiesced())
	tx.mu.Lock()
	require.Nil(t, tx.timer_a)
	require.Nil(t, tx.timer_b, "Init must not install a timer after Terminate stopped the transaction")
	tx.mu.Unlock()
}

func TestClientTransactionQuiescenceWaitsForTimerResend(t *testing.T) {
	conn := &lifecycleClientConn{writeEntered: make(chan struct{}), writeRelease: make(chan struct{}), blockWriteAt: 2}
	tx, _ := newLifecycleClientTx(t, conn)
	var once sync.Once
	defer once.Do(func() { close(conn.writeRelease) })
	// Fire a real timer callback at the same FSM entry used by Timer A.
	tx.mu.Lock()
	tx.timer_a_time = time.Second
	tx.timer_a = time.AfterFunc(time.Millisecond, func() { tx.spinFsm(client_input_timer_a) })
	tx.mu.Unlock()
	waitClientLifecycle(t, conn.writeEntered)
	exited := make(chan struct{})
	go func() { tx.Terminate(); close(exited) }()
	waitClientLifecycle(t, tx.Done())
	select {
	case <-tx.Quiesced():
		t.Fatal("resend WriteMsg still blocked")
	default:
	}
	once.Do(func() { close(conn.writeRelease) })
	waitClientLifecycle(t, exited)
	waitClientLifecycle(t, tx.Quiesced())
	writes := conn.writes.Load()
	tx.spinFsm(client_input_timer_a)
	require.Equal(t, writes, conn.writes.Load())
}

func TestClientTransactionQuiescenceRejectsAlreadyQueuedResponse(t *testing.T) {
	conn := &lifecycleClientConn{}
	tx, response := newLifecycleClientTx(t, conn)
	firstDone := make(chan struct{})
	go func() { tx.Receive(response); close(firstDone) }()
	<-tx.Responses()
	waitClientLifecycle(t, firstDone)
	var callbacks atomic.Int32
	require.True(t, tx.OnRetransmission(func(*Response) { callbacks.Add(1) }))
	tx.fsmMu.Lock()
	var unlock sync.Once
	defer unlock.Do(tx.fsmMu.Unlock)
	received, terminated := make(chan struct{}), make(chan struct{})
	go func() { tx.Receive(response); close(received) }()
	require.Eventually(t, func() bool { tx.lifeMu.Lock(); defer tx.lifeMu.Unlock(); return tx.activeWork == 1 }, time.Second, time.Millisecond)
	go func() { tx.Terminate(); close(terminated) }()
	waitClientLifecycle(t, tx.Done())
	select {
	case <-tx.Quiesced():
		t.Fatal("queued receive and Terminate tail have not exited")
	default:
	}
	unlock.Do(tx.fsmMu.Unlock)
	waitClientLifecycle(t, received)
	waitClientLifecycle(t, terminated)
	waitClientLifecycle(t, tx.Quiesced())
	require.Zero(t, callbacks.Load())
}

func TestClientTransactionQuiescenceCoversNaturalDeleteAndConcurrentStop(t *testing.T) {
	conn := &lifecycleClientConn{}
	tx, response := newLifecycleClientTx(t, conn)
	firstDone := make(chan struct{})
	go func() { tx.Receive(response); close(firstDone) }()
	<-tx.Responses()
	waitClientLifecycle(t, firstDone)
	tx.mu.Lock()
	require.NotNil(t, tx.timer_m, "200 must install the actual accepted-state timer")
	tx.mu.Unlock()
	var callbacks, terminations atomic.Int32
	require.True(t, tx.OnRetransmission(func(*Response) { callbacks.Add(1) }))
	require.True(t, tx.OnTerminate(func(string, error) { terminations.Add(1) }))
	var wg sync.WaitGroup
	for n := 0; n < 100; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			switch n % 4 {
			case 0:
				tx.Receive(response)
			case 1:
				tx.spinFsm(client_input_timer_m)
			default:
				tx.Terminate()
			}
		}(n)
	}
	wg.Wait()
	waitClientLifecycle(t, tx.Quiesced())
	require.EqualValues(t, 1, conn.closes.Load())
	require.EqualValues(t, 1, terminations.Load())
	before := callbacks.Load()
	tx.Receive(response)
	tx.spinFsm(client_input_timer_a)
	tx.spinFsmWithErrorAsync(client_input_transport_err, ErrTransactionTransport)
	tx.Terminate()
	require.Equal(t, before, callbacks.Load())
	tx.mu.Lock()
	require.Nil(t, tx.timer_a)
	require.Nil(t, tx.timer_b)
	require.Nil(t, tx.timer_d)
	require.Nil(t, tx.timer_m)
	tx.mu.Unlock()
	tx.lifeMu.Lock()
	require.Zero(t, tx.activeWork)
	tx.lifeMu.Unlock()
}

func TestClientTransactionQuiescenceCanBeRequestedInsideCallback(t *testing.T) {
	conn := &lifecycleClientConn{}
	tx, response := newLifecycleClientTx(t, conn)
	firstDone := make(chan struct{})
	go func() { tx.Receive(response); close(firstDone) }()
	<-tx.Responses()
	waitClientLifecycle(t, firstDone)
	require.True(t, tx.OnRetransmission(func(*Response) {
		tx.Terminate()
		select {
		case <-tx.Quiesced():
			t.Error("callback still owns work")
		default:
		}
	}))
	exited := make(chan struct{})
	go func() { tx.Receive(response); close(exited) }()
	waitClientLifecycle(t, exited)
	waitClientLifecycle(t, tx.Quiesced())
}

func TestClientTransactionQuiescenceOwnsQueuedInternalError(t *testing.T) {
	tx, _ := newLifecycleClientTx(t, &lifecycleClientConn{})
	tx.fsmMu.Lock()
	var unlock sync.Once
	defer unlock.Do(tx.fsmMu.Unlock)
	tx.spinFsmWithErrorAsync(client_input_transport_err, ErrTransactionTransport)
	// Registration is synchronous, even if the goroutine has not started.
	tx.lifeMu.Lock()
	active := tx.activeWork
	tx.lifeMu.Unlock()
	require.Equal(t, 1, active)
	terminated := make(chan struct{})
	go func() { tx.Terminate(); close(terminated) }()
	waitClientLifecycle(t, tx.Done())
	select {
	case <-tx.Quiesced():
		t.Fatal("queued internal error has not exited")
	default:
	}
	unlock.Do(tx.fsmMu.Unlock)
	waitClientLifecycle(t, terminated)
	waitClientLifecycle(t, tx.Quiesced())
	require.ErrorIs(t, tx.Err(), ErrTransactionCanceled, "queued transport error must not overwrite the termination result")
}
