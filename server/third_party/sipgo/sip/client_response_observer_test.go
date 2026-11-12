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

type responseCaptureFixture struct {
	captured, lost   atomic.Int32
	entered, release chan struct{}
}

func (s *responseCaptureFixture) CaptureResponse(*Response) {
	if s.entered != nil {
		close(s.entered)
		<-s.release
	}
	s.captured.Add(1)
}
func (s *responseCaptureFixture) ObservationLost() { s.lost.Add(1) }

func responseObserverLayer(t *testing.T) *TransactionLayer {
	t.Helper()
	tp := NewTransportLayer(net.DefaultResolver, NewParser(), nil)
	layer := NewTransactionLayer(tp, WithTransactionLayerUnhandledResponseHandler(func(*Response) {}))
	t.Cleanup(func() { layer.Close(); _ = tp.Close() })
	return layer
}

func TestClientResponseObserverCapturesBeforeClosedTransactionLookup(t *testing.T) {
	layer := responseObserverLayer(t)
	req, _, _ := testCreateInvite(t, "sip:127.0.0.99:5060", "tcp", "127.0.0.2:5060")
	conn := &lifecycleClientConn{}
	sink := &responseCaptureFixture{}
	_, err := layer.ObserveClientResponses(req, conn, sink)
	require.NoError(t, err)
	key, err := ClientTxKeyMake(req)
	require.NoError(t, err)
	tx := NewClientTx(key, req, conn, slog.Default())
	layer.clientTransactions.items[key] = tx
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	tx.OnTerminate(func(key string, err error) { close(entered); <-release; layer.clientTxTerminate(key, err) })
	terminated := make(chan struct{})
	go func() { tx.Terminate(); close(terminated) }()
	waitClientLifecycle(t, entered)
	require.True(t, tx.isQuiescing())
	_, exists := layer.getClientTx(key)
	require.True(t, exists, "actual terminating transaction is still in the map")
	response := NewResponseFromRequest(req, 200, "OK", nil)
	layer.handleMessage(response)
	require.EqualValues(t, 1, sink.captured.Load(), "capture completes synchronously before FSM lookup can discard")
	require.NoError(t, layer.handleResponse(response), "quiescing Receive rejects without involving the observer")
	require.EqualValues(t, 1, sink.captured.Load())
	once.Do(func() { close(release) })
	waitClientLifecycle(t, terminated)
	waitClientLifecycle(t, tx.Quiesced())
}

func TestClientResponseObserverCapacityAndExplicitClose(t *testing.T) {
	layer := responseObserverLayer(t)
	conn := &lifecycleClientConn{}
	request, _, _ := testCreateInvite(t, "sip:127.0.0.99:5060", "tcp", "127.0.0.2:5060")
	sink := &responseCaptureFixture{}
	first, err := layer.ObserveClientResponses(request, conn, sink)
	require.NoError(t, err)
	_, err = layer.ObserveClientResponses(request.Clone(), conn, &responseCaptureFixture{})
	require.ErrorIs(t, err, ErrClientResponseObservation)
	for n := 1; n < maxClientResponseObservers; n++ {
		r := request.Clone()
		r.Via().Params.Add("branch", GenerateBranch())
		_, err = layer.ObserveClientResponses(r, conn, &responseCaptureFixture{})
		require.NoError(t, err)
	}
	r := request.Clone()
	r.Via().Params.Add("branch", GenerateBranch())
	_, err = layer.ObserveClientResponses(r, conn, &responseCaptureFixture{})
	require.ErrorIs(t, err, ErrClientResponseObservation)
	layer.captureClientResponse(NewResponseFromRequest(request, 200, "OK", nil))
	require.EqualValues(t, 1, sink.captured.Load(), "capacity cannot evict the first owner")
	first.Close()
	require.EqualValues(t, 1, sink.lost.Load())
	select {
	case <-first.Done():
	default:
		t.Fatal("observation close did not complete")
	}
	_, err = layer.ObserveClientResponses(r, conn, &responseCaptureFixture{})
	require.NoError(t, err)
	layer.Close()
	_, err = layer.ObserveClientResponses(request, conn, &responseCaptureFixture{})
	require.ErrorIs(t, err, ErrClientResponseObservation)
	require.Zero(t, conn.writes.Load(), "registration is never network authority")
}

func TestClientResponseObserverConnectionLossIsExact(t *testing.T) {
	layer := responseObserverLayer(t)
	connA, connB := &lifecycleClientConn{}, &lifecycleClientConn{}
	sinkA, sinkB := &responseCaptureFixture{}, &responseCaptureFixture{}
	a, _, _ := testCreateInvite(t, "sip:127.0.0.99:5060", "tcp", "127.0.0.2:5060")
	b := a.Clone()
	b.Via().Params.Add("branch", GenerateBranch())
	_, err := layer.ObserveClientResponses(a, connA, sinkA)
	require.NoError(t, err)
	_, err = layer.ObserveClientResponses(b, connB, sinkB)
	require.NoError(t, err)
	layer.OnConnectionClose(connA)
	require.EqualValues(t, 1, sinkA.lost.Load())
	require.Zero(t, sinkB.lost.Load())
	layer.captureClientResponse(NewResponseFromRequest(a, 200, "OK", nil))
	require.EqualValues(t, 1, sinkA.captured.Load(), "transport loss does not erase the owner")
}

func TestClientResponseObserverCloseJoinsCapture(t *testing.T) {
	layer := responseObserverLayer(t)
	req, _, _ := testCreateInvite(t, "sip:127.0.0.99:5060", "tcp", "127.0.0.2:5060")
	sink := &responseCaptureFixture{entered: make(chan struct{}), release: make(chan struct{})}
	owner, err := layer.ObserveClientResponses(req, &lifecycleClientConn{}, sink)
	require.NoError(t, err)
	var once sync.Once
	defer once.Do(func() { close(sink.release) })
	captured := make(chan struct{})
	go func() { layer.captureClientResponse(NewResponseFromRequest(req, 200, "OK", nil)); close(captured) }()
	waitClientLifecycle(t, sink.entered)
	closed := make(chan struct{})
	go func() { owner.Close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("close overtook entered synchronous capture")
	case <-time.After(15 * time.Millisecond):
	}
	once.Do(func() { close(sink.release) })
	waitClientLifecycle(t, captured)
	waitClientLifecycle(t, closed)
	require.EqualValues(t, 1, sink.captured.Load())
	require.EqualValues(t, 1, sink.lost.Load())
}
