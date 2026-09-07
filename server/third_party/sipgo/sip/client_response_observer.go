package sip

import (
	"errors"
	"sync"
)

const maxClientResponseObservers = 64

var ErrClientResponseObservation = errors.New("client response observation unavailable")

// ClientResponseSink is an internal observation-only integration seam. Methods
// run on the receive path: bounded memory operations only, no SQL, network,
// waiting for consumers, or application callbacks. Capture must detach any
// retained material; the original message still goes through the normal FSM.
type ClientResponseSink interface {
	CaptureResponse(*Response)
	ObservationLost()
}

// ClientResponseObservation outlives its transaction. It is not network
// permission and is never expired by timers or by transaction termination.
type ClientResponseObservation struct {
	layer    *TransactionLayer
	key      string
	conn     Connection
	selector *DetachedInviteResponseSelector
	sink     ClientResponseSink
	mu       sync.Mutex
	closed   bool
	done     chan struct{}
}

// ObserveClientResponses must be installed before Init can write the request.
// Duplicate keys and capacity exhaustion fail closed; old owners are not evicted.
func (txl *TransactionLayer) ObserveClientResponses(request *Request, conn Connection, sink ClientResponseSink) (*ClientResponseObservation, error) {
	if request == nil || request.Method != INVITE || request.CSeq() == nil || request.CSeq().MethodName != INVITE || conn == nil || sink == nil {
		return nil, ErrClientResponseObservation
	}
	key, err := ClientTxKeyMake(request)
	if err != nil {
		return nil, ErrClientResponseObservation
	}
	return txl.registerResponseObservation(key, conn, nil, sink)
}

func (txl *TransactionLayer) registerResponseObservation(key string, conn Connection, selector *DetachedInviteResponseSelector, sink ClientResponseSink) (*ClientResponseObservation, error) {
	txl.responseObserverMu.Lock()
	defer txl.responseObserverMu.Unlock()
	if txl.responseObserversClosed || len(txl.responseObservers) >= maxClientResponseObservers || txl.responseObservers[key] != nil {
		return nil, ErrClientResponseObservation
	}
	if txl.responseObservers == nil {
		txl.responseObservers = make(map[string]*ClientResponseObservation)
	}
	o := &ClientResponseObservation{layer: txl, key: key, conn: conn, selector: selector, sink: sink, done: make(chan struct{})}
	txl.responseObservers[key] = o
	return o, nil
}

func (o *ClientResponseObservation) capture(response *Response) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.closed {
		if o.selector != nil && !o.selector.matches(response) {
			o.sink.ObservationLost()
			return
		}
		o.sink.CaptureResponse(response)
	}
}

func (o *ClientResponseObservation) lost() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.closed {
		o.sink.ObservationLost()
	}
}

// Close joins synchronous capture and marks loss before removing the owner.
// Retained snapshots belong to the sink and must not be cleared by Close.
func (o *ClientResponseObservation) Close() {
	o.mu.Lock()
	if !o.closed {
		o.closed = true
		o.sink.ObservationLost()
		close(o.done)
	}
	o.mu.Unlock()
	o.layer.responseObserverMu.Lock()
	if o.layer.responseObservers[o.key] == o {
		delete(o.layer.responseObservers, o.key)
	}
	o.layer.responseObserverMu.Unlock()
}

func (o *ClientResponseObservation) Done() <-chan struct{} { return o.done }

func (txl *TransactionLayer) captureClientResponse(response *Response) {
	key, err := ClientTxKeyMake(response)
	if err != nil {
		return // No safe owner selection; normal malformed-message handling remains.
	}
	txl.responseObserverMu.Lock()
	o := txl.responseObservers[key]
	txl.responseObserverMu.Unlock()
	if o != nil {
		o.capture(response)
	}
}

func (txl *TransactionLayer) responseConnectionLost(conn Connection) {
	txl.responseObserverMu.Lock()
	var affected []*ClientResponseObservation
	for _, o := range txl.responseObservers {
		if o.conn == conn {
			affected = append(affected, o)
		}
	}
	txl.responseObserverMu.Unlock()
	for _, o := range affected {
		o.lost()
	}
}

func (txl *TransactionLayer) closeResponseObservers() {
	txl.responseObserverMu.Lock()
	txl.responseObserversClosed = true
	observers := txl.responseObservers
	txl.responseObservers = nil
	txl.responseObserverMu.Unlock()
	for _, o := range observers {
		o.Close()
	}
}
