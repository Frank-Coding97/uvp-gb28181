package sip

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type TransactionRequestHandler func(req *Request, tx *ServerTx)
type UnhandledResponseHandler func(req *Response)
type ErrorHandler func(err error)

// RequestLifecycle brackets the middleware and business handler portion of a
// server request. TerminateGracefully remains outside that bracket so the
// application can quiesce handlers without waiting for UDP Timer J.
type RequestLifecycle interface {
	BeginBusiness(*Request, ServerTransaction) bool
	EndBusiness()
}

// RequestLease is reserved while a new server transaction is admitted. Its
// Release method must be idempotent because both the transaction layer and the
// outer server handler own a cleanup path.
type RequestLease interface {
	Release()
}

// RequestAdmission is an optional extension implemented by an application
// lifecycle owner that needs to reserve business work under the transaction
// store admission lock. Implementations return false without a lease when the
// application is already quiescing.
type RequestAdmission interface {
	ReserveBusiness(*Request, ServerTransaction) (RequestLease, bool)
}

func defaultRequestHandler(r *Request, tx *ServerTx) {
	DefaultLogger().Info("Unhandled sip request. OnRequest handler not added", "caller", "transactionLayer", "msg", r.Short())
}

func defaultUnhandledRespHandler(r *Response) {
	DefaultLogger().Info("TransactionLayer: Unhandled sip response. Possible retransmissions. Set UnhandledResponseHandler", "caller", "transactionLayer", "msg", r.Short())
}

type TransactionLayer struct {
	tpl           *TransportLayer
	reqHandler    TransactionRequestHandler
	unRespHandler UnhandledResponseHandler

	clientTransactions *transactionStore[*ClientTx]
	serverTransactions *transactionStore[*ServerTx]

	terminateOnConnClose  bool
	requestLifecycle      RequestLifecycle
	dispatchWork          *lifecycleGate
	fsmWork               *lifecycleGate
	serverAdmissionClosed bool
	clientAdmissionClosed bool
	closeOnce             sync.Once

	log *slog.Logger
}

type TransactionLayerOption func(tpl *TransactionLayer)

func WithTransactionLayerLogger(l *slog.Logger) TransactionLayerOption {
	return func(txl *TransactionLayer) {
		if l != nil {
			txl.log = l.With("caller", "TransactionLayer")
		}
	}
}

func WithTransactionLayerUnhandledResponseHandler(f func(r *Response)) TransactionLayerOption {
	return func(txl *TransactionLayer) {
		txl.unRespHandler = f
	}
}

// WithTransactionLayerTerminateOnConnClose enables termination of pending
// client and server transactions when the underlying connection closes,
// instead of waiting for timers to expire.
//
// Experimental
func WithTransactionLayerTerminateOnConnClose() TransactionLayerOption {
	return func(txl *TransactionLayer) {
		txl.terminateOnConnClose = true
	}
}

func NewTransactionLayer(tpl *TransportLayer, options ...TransactionLayerOption) *TransactionLayer {
	txl := &TransactionLayer{
		tpl:                tpl,
		clientTransactions: newTransactionStore[*ClientTx](),
		serverTransactions: newTransactionStore[*ServerTx](),
		dispatchWork:       &lifecycleGate{},
		fsmWork:            &lifecycleGate{},

		reqHandler:    defaultRequestHandler,
		unRespHandler: defaultUnhandledRespHandler,
	}
	txl.log = DefaultLogger().With("caller", "TransactionLayer")

	for _, o := range options {
		o(txl)
	}

	//Send all transport messages to our transaction layer
	tpl.OnMessage(txl.handleMessage)

	notify := func(conn Connection) { txl.OnConnectionClose(conn) }
	if tpl.tcp != nil {
		tpl.tcp.onConnClose = notify
	}
	if tpl.tls != nil {
		tpl.tls.onConnClose = notify
	}
	if tpl.ws != nil {
		tpl.ws.onConnClose = notify
	}
	if tpl.wss != nil {
		tpl.wss.onConnClose = notify
	}

	return txl
}

func (txl *TransactionLayer) OnRequest(h TransactionRequestHandler) {
	txl.reqHandler = h
}

func (txl *TransactionLayer) SetRequestLifecycle(lifecycle RequestLifecycle) {
	txl.requestLifecycle = lifecycle
}

// QuiesceRequests closes only new server-transaction admission. Existing
// transactions remain routable for retransmissions, ACK, CANCEL and responses.
// The transaction-store mutex is deliberately held while flipping the bit so
// a request cannot race the admission decision.
func (txl *TransactionLayer) QuiesceRequests() {
	txl.serverTransactions.lock()
	txl.serverAdmissionClosed = true
	txl.serverTransactions.unlock()
}

// OnConnectionClose is called when a reliable transport connection (TCP, TLS,
// WS, WSS) is closed by the remote side or due to a read error.
func (txl *TransactionLayer) OnConnectionClose(conn Connection) {
	if txl.terminateOnConnClose {
		txl.terminateClientTransactions(conn)
		txl.terminateServerTransactions(conn)
	}
}

func (txl *TransactionLayer) terminateClientTransactions(conn Connection) {
	txl.clientTransactions.mu.RLock()
	for _, tx := range txl.clientTransactions.items {
		if tx.conn == conn {
			err := fmt.Errorf("connection closed: %w", ErrTransactionTransport)
			tx.goTracked(func() { tx.spinFsmWithError(client_input_transport_err, err) })
		}
	}
	txl.clientTransactions.mu.RUnlock()
}

func (txl *TransactionLayer) terminateServerTransactions(conn Connection) {
	txl.serverTransactions.mu.RLock()
	for _, tx := range txl.serverTransactions.items {
		if tx.conn == conn {
			err := fmt.Errorf("connection closed: %w", ErrTransactionTransport)
			tx.goTracked(func() { tx.spinFsmWithError(server_input_transport_err, err) })
		}
	}
	txl.serverTransactions.mu.RUnlock()
}

// handleMessage is entry for handling requests and responses from transport
func (txl *TransactionLayer) handleMessage(msg Message) {
	// Having concurency here we increased throghput but also solving deadlock
	// Current client transactions are blocking on passUp and this may block when calling tx.Receive
	// forking here can remove this

	switch msg := msg.(type) {
	case *Request:
		txl.dispatchWork.goRun(func() { txl.handleRequestBackground(msg) })
	case *Response:
		txl.dispatchWork.goRun(func() { txl.handleResponseBackground(msg) })
	default:
		txl.dispatchWork.goRun(func() { txl.log.Error("unsupported message, skip it") })
	}
}

func (txl *TransactionLayer) handleRequestBackground(req *Request) {
	if err := txl.handleRequest(req); err != nil {
		txl.log.Error("Server tx failed to handle request", "error", err, "req", req.StartLine())
	}
}

func (txl *TransactionLayer) handleRequest(req *Request) error {
	if req.IsCancel() {
		// Match transaction https://datatracker.ietf.org/doc/html/rfc3261#section-9.2
		// 	The CANCEL method requests that the TU at the server side cancel a
		//    pending transaction.  The TU determines the transaction to be
		//    cancelled by taking the CANCEL request, and then assuming that the
		//    request method is anything but CANCEL or AC

		// For now we only match INVITE
		key, err := makeServerTxKey(req, INVITE)
		if err != nil {
			txl.rejectMalformedRequest(req, err)
			return fmt.Errorf("make key failed: %w", err)
		}

		tx, exists := txl.getServerTx(key)
		if exists {
			// Send 200 OK for CANCEL first, then terminate the INVITE.
			// The CANCEL client transaction retransmits until it receives
			// a response; sending 200 OK before the 487 ensures the UAC
			// stops retransmitting immediately.
			if err := tx.conn.WriteMsg(NewResponseFromRequest(req, StatusOK, "OK", nil)); err != nil {
				return fmt.Errorf("Failed to respond 200 for CANCEL: %w", err)
			}

			// Now let the FSM terminate the INVITE transaction (sends 487)
			if err := tx.Receive(req); err != nil {
				return fmt.Errorf("failed to receive req: %w", err)
			}
			return nil
		}
		// Now proceed as normal transaction, and let developer decide what todo with this CANCEL
	}

	key, err := makeServerTxKey(req, "")
	if err != nil {
		txl.rejectMalformedRequest(req, err)
		return fmt.Errorf("make key failed: %w", err)
	}

	return txl.serverTxRequest(req, key)
}

// rejectMalformedRequest sends a stateless 400 Bad Request response when a
// request is too malformed to create a transaction (e.g. missing CSeq or Via).
//
// Per RFC 3261 Section 8.2:
//
//	If a UAS does not understand a header field in a request (that is,
//	the header field is not defined in this specification or in any
//	installed extensions), the server MUST ignore that header field and
//	continue processing the message.
//
// And Section 16.3 (Request Validation):
//
//	If the request contains a malformed header field that the proxy
//	requires, the proxy SHOULD return a 400 (Bad Request) response.
//
// Without this, the sender retransmits indefinitely because it never
// receives any response.
func (txl *TransactionLayer) rejectMalformedRequest(req *Request, reason error) {
	// Build a minimal 400 response from whatever headers the request has.
	// NewResponseFromRequest safely skips nil CSeq, From, To, Call-ID.
	res := NewResponseFromRequest(req, StatusBadRequest, "Bad Request", nil)

	if err := txl.tpl.WriteMsg(res); err != nil {
		txl.log.Error("Failed to send stateless 400 for malformed request",
			"error", err,
			"reason", reason.Error(),
			"req", req.StartLine(),
		)
	}
}

func (txl *TransactionLayer) serverTxRequest(req *Request, key string) error {
	txl.serverTransactions.lock()
	tx, exists := txl.serverTransactions.items[key]
	if exists {
		txl.serverTransactions.unlock()
		if err := tx.Receive(req); err != nil {
			return fmt.Errorf("failed to receive req: %w", err)
		}
		return nil
	}
	if txl.serverAdmissionClosed {
		txl.serverTransactions.unlock()
		// The request has already passed transport read filtering and SIP
		// parsing. During quiesce, a new unmatched request is intentionally
		// dropped without adding a protocol response or auth bypass.
		return nil
	}

	tx, err := txl.serverTxCreate(req, key)
	if err != nil {
		txl.serverTransactions.unlock()
		return err
	}

	var lease RequestLease
	if admission, ok := txl.requestLifecycle.(RequestAdmission); ok {
		var accepted bool
		lease, accepted = admission.ReserveBusiness(req, tx)
		if !accepted {
			if lease != nil {
				lease.Release()
			}
			txl.serverTransactions.unlock()
			tx.Terminate()
			return nil
		}
		if lease != nil {
			defer lease.Release()
		}
		tx.setRequestLease(lease)
	}

	// Put the admitted transaction in the store while the same lock still
	// protects the admission decision. A later QuiesceRequests call therefore
	// cannot close the business gate between reservation and insertion.
	txl.serverTransactions.items[key] = tx
	tx.OnTerminate(txl.serverTxTerminate)
	txl.serverTransactions.unlock()

	// pass request and transaction to handler
	txl.reqHandler(req, tx)
	return nil
}

func (txl *TransactionLayer) serverTxCreate(req *Request, key string) (*ServerTx, error) {
	// Connection must exist by transport layer or it will be created
	// What if connection setup can not be made fast enough?
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := txl.tpl.serverRequestConnection(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("server tx get connection failed: %w", err)
	}

	tx := NewServerTx(key, req, conn, txl.log, txl.fsmWork)
	return tx, tx.Init()
}

func (txl *TransactionLayer) handleResponseBackground(res *Response) {
	if err := txl.handleResponse(res); err != nil {
		txl.log.Error("Client tx failed to handle response", "error", err)
	}
}

func (txl *TransactionLayer) handleResponse(res *Response) error {
	key, err := ClientTxKeyMake(res)
	if err != nil {
		return fmt.Errorf("make key failed: %w", err)
	}

	tx, exists := txl.getClientTx(key)
	if !exists {
		// RFC 3261 - 17.1.1.2.
		// Not matched responses should be passed directly to the UA
		txl.unRespHandler(res)
		return nil
	}

	tx.Receive(res)
	return nil
}

func (txl *TransactionLayer) Request(ctx context.Context, req *Request) (*ClientTx, error) {
	tx, err := txl.NewClientTransaction(ctx, req)
	if err != nil {
		return nil, err
	}

	if err := tx.Init(); err != nil {
		tx.Terminate()
		return nil, err
	}
	return tx, nil
}

func (txl *TransactionLayer) NewClientTransaction(ctx context.Context, req *Request) (*ClientTx, error) {
	if req.IsAck() {
		return nil, fmt.Errorf("ACK request must be sent directly through transport")
	}

	key, err := ClientTxKeyMake(req)
	if err != nil {
		return nil, err
	}

	tx, err := txl.clientTxRequest(ctx, req, key)
	if err != nil {
		return nil, err
	}
	return tx, nil
}

func (txl *TransactionLayer) clientTxRequest(ctx context.Context, req *Request, key string) (*ClientTx, error) {
	conn, err := txl.tpl.ClientRequestConnection(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("client transcation failed to request connection: %w", err)
	}

	txl.clientTransactions.lock()
	tx, exists := txl.clientTransactions.items[key]
	if exists {
		txl.clientTransactions.unlock()
		conn.TryClose()
		return nil, fmt.Errorf("client transaction %q already exists", key)
	}
	tx = NewClientTx(key, req, conn, txl.log, txl.fsmWork)

	if txl.clientAdmissionClosed {
		txl.clientTransactions.unlock()
		_, _ = conn.TryClose()
		return nil, ErrTransactionLayerClosed
	}
	txl.clientTransactions.items[key] = tx
	tx.OnTerminate(txl.clientTxTerminate)
	txl.clientTransactions.unlock()
	return tx, nil
}

func (txl *TransactionLayer) Respond(res *Response) (*ServerTx, error) {
	key, err := ServerTxKeyMake(res)
	if err != nil {
		return nil, err
	}

	tx, exists := txl.getServerTx(key)
	if !exists {
		return nil, fmt.Errorf("transaction does not exists")
	}

	err = tx.Respond(res)
	if err != nil {
		return nil, err
	}

	return tx, nil
}

func (txl *TransactionLayer) clientTxTerminate(key string, err error) {
	if !txl.clientTransactions.drop(key) {
		txl.log.Info("Non existing client tx was removed", "tx", key)
	}
}

func (txl *TransactionLayer) serverTxTerminate(key string, err error) {
	if !txl.serverTransactions.drop(key) {
		txl.log.Info("Non existing server tx was removed", "tx", key)
	}
}

// RFC 17.1.3.
func (txl *TransactionLayer) getClientTx(key string) (*ClientTx, bool) {
	return txl.clientTransactions.get(key)
	// tx, ok := txl.clientTransactions.get(key)
	// if !ok {
	// 	return nil, false
	// }
	// return tx.(*ClientTx), true
}

// RFC 17.2.3.
func (txl *TransactionLayer) getServerTx(key string) (*ServerTx, bool) {
	return txl.serverTransactions.get(key)
	// tx, ok := txl.serverTransactions.get(key)
	// if !ok {
	// 	return nil, false
	// }
	// return tx.(*ServerTx), true
}

var ErrTransactionLayerClosed = errors.New("transaction layer is closed")

// CloseContext terminates transactions first so a dispatch blocked in
// TerminateGracefully is released before its completion gate is joined.
func (txl *TransactionLayer) CloseContext(ctx context.Context) error {
	txl.closeOnce.Do(func() {
		txl.serverTransactions.lock()
		txl.serverAdmissionClosed = true
		txl.serverTransactions.unlock()
		txl.clientTransactions.lock()
		txl.clientAdmissionClosed = true
		txl.clientTransactions.unlock()

		txl.clientTransactions.terminateAll()
		txl.serverTransactions.terminateAll()
		txl.dispatchWork.close()
		txl.fsmWork.close()
		txl.log.Debug("transaction layer closed")
	})
	return errors.Join(txl.dispatchWork.wait(ctx), txl.fsmWork.wait(ctx))
}

func (txl *TransactionLayer) Close() {
	_ = txl.CloseContext(context.Background())
}

func (txl *TransactionLayer) Transport() *TransportLayer {
	return txl.tpl
}
