package sipgo

import (
	"context"
	"errors"
	"sync"

	"github.com/emiago/sipgo/sip"
)

var ErrOwnedInviteState = errors.New("owned INVITE unavailable or invalid state")

// OwnedClientInvite retains a concrete transaction BEFORE its first SIP write.
// It is an internal integration seam, not authorization: its caller must own
// the device operation and commit dispatch permission before calling Start.
// The caller must not use Session().Invite/WaitAnswer/Ack while this owner is
// active: those legacy methods have implicit transactions and retransmissions.
type OwnedClientInvite struct {
	session                             *DialogClientSession
	tx                                  *sip.ClientTx
	mu                                  sync.Mutex
	started, closing, transactionExited bool
	active                              int
	quiesced                            chan struct{}
	quiescedClosed                      bool
	accepted, ackPrepared, ackStarted   bool
	ack                                 *sip.Request
	unmatched                           chan *sip.Response
	branches                            []*sip.Response
	branchesIncomplete                  bool
	branchChanges                       chan struct{}
	branchesFrozen                      bool
	quarantine                          []*sip.Response
	quarantineIncomplete                bool
	quarantineChanges                   chan struct{}
	responseObservation                 *sip.ClientResponseObservation
	writeErrors                         chan error
	provisionalObserved, finalObserved  bool
	cancelPrepared, cancelStarted       bool
	cancelTx                            *sip.ClientTx
	cancelExited                        bool
}

// PrepareWriteInviteOwned accepts an already fixed request. It may establish a
// transport connection but sends no SIP message and does not call Init. The
// request is cloned; connection-time mutations never overwrite caller material.
// A durable caller must recheck the returned Request against its fixed snapshot
// before Start. Dynamic Via/Contact ports and test TxRequester are not supported.
func (ua *DialogUA) PrepareWriteInviteOwned(ctx context.Context, request *sip.Request) (*OwnedClientInvite, error) {
	if ua == nil || ua.Client == nil || ua.Client.UserAgent == nil || ua.Client.TransactionLayer() == nil || ua.Client.TxRequester != nil || ctx == nil || request == nil || request.Method != sip.INVITE {
		return nil, ErrOwnedInviteState
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, name := range []string{"Via", "Contact", "From", "To", "Call-ID", "CSeq", "Max-Forwards", "Content-Length"} {
		if len(request.GetHeaders(name)) != 1 {
			return nil, ErrOwnedInviteState
		}
	}
	via, contact := request.Via(), request.Contact()
	if via == nil || via.Host == "" || via.Port <= 0 || via.Port > 65535 || contact == nil || contact.Address.Host == "" || contact.Address.Port <= 0 || contact.Address.Port > 65535 ||
		request.From() == nil || request.To() == nil || request.CallID() == nil || request.CSeq() == nil || request.CSeq().MethodName != sip.INVITE ||
		request.MessageData.Destination() == "" || request.MaxForwards() == nil || request.ContentLength() == nil || int(*request.ContentLength()) != len(request.Body()) ||
		(request.MessageData.Transport() != "UDP" && request.MessageData.Transport() != "TCP") || via.Transport != request.MessageData.Transport() ||
		ownedSingleTag(request.From().Params) == "" || *request.CallID() == "" || request.CSeq().SeqNo == 0 || via.Params.GetOr("branch", "") == "" {
		return nil, ErrOwnedInviteState
	}
	// v1 durable material does not describe outbound Route headers. Do not
	// silently route a future CANCEL differently from the persisted INVITE.
	if len(request.GetHeaders("Route")) != 0 {
		return nil, ErrOwnedInviteState
	}
	prepared := request.Clone()
	prepared.SetBody(append([]byte(nil), request.Body()...))
	tx, err := ua.Client.TransactionLayer().NewClientTransaction(ctx, prepared)
	if err != nil {
		return nil, err
	}
	o := newOwnedClientInvite(ua, prepared, tx)
	o.responseObservation, err = ua.Client.TransactionLayer().ObserveClientResponses(prepared, tx.Connection(), ownedInviteResponseSink{o})
	if err != nil {
		o.Terminate()
		<-o.Quiesced()
		return nil, err
	}
	return o, nil
}

func newOwnedClientInvite(ua *DialogUA, request *sip.Request, tx *sip.ClientTx) *OwnedClientInvite {
	session := &DialogClientSession{Dialog: Dialog{InviteRequest: request}, UA: ua, inviteTx: tx}
	session.Dialog.Init()
	o := &OwnedClientInvite{session: session, tx: tx, quiesced: make(chan struct{}), unmatched: make(chan *sip.Response, 1), branchChanges: make(chan struct{}, 1), writeErrors: make(chan error, 1)}
	o.quarantineChanges = make(chan struct{}, 1)
	if !tx.OnRetransmission(func(response *sip.Response) { o.observeBranch(response, true) }) {
		o.closing, o.branchesIncomplete = true, true
	}
	go func() {
		<-tx.Quiesced()
		o.mu.Lock()
		o.transactionExited, o.closing = true, true
		cancelTx := o.cancelTx
		o.closeQuiescedLocked()
		o.mu.Unlock()
		if cancelTx != nil {
			cancelTx.Terminate()
		}
	}()
	return o
}

func (o *OwnedClientInvite) Transaction() *sip.ClientTx    { return o.tx }
func (o *OwnedClientInvite) Session() *DialogClientSession { return o.session }
func (o *OwnedClientInvite) Request() *sip.Request {
	r := o.session.InviteRequest.Clone()
	r.SetBody(append([]byte(nil), o.session.InviteRequest.Body()...))
	return r
}

// Start is once-only even when Init returns an error after a partial write.
// The handle and transaction remain available on every error. The caller must
// Terminate and observe Quiesced; an error is not evidence of zero dispatch.
func (o *OwnedClientInvite) Start() error {
	o.mu.Lock()
	if o.started || o.closing || o.branchesIncomplete {
		o.mu.Unlock()
		return ErrOwnedInviteState
	}
	o.started = true
	o.active++
	o.mu.Unlock()
	defer o.endWork()
	return o.tx.Init()
}

func (o *OwnedClientInvite) endWork() {
	o.mu.Lock()
	o.active--
	o.closeQuiescedLocked()
	o.mu.Unlock()
}

func (o *OwnedClientInvite) closeQuiescedLocked() {
	if o.transactionExited && o.active == 0 && (o.cancelTx == nil || o.cancelExited) && !o.quiescedClosed {
		o.branchesFrozen = true
		o.quiescedClosed = true
		close(o.quiesced)
	}
}

// Terminate requests local shutdown only. No CANCEL/BYE is generated. It may
// wait behind entered transaction work; call outside application locks.
func (o *OwnedClientInvite) Terminate() {
	o.mu.Lock()
	o.closing = true
	cancelTx := o.cancelTx
	o.mu.Unlock()
	o.tx.Terminate()
	if cancelTx != nil {
		cancelTx.Terminate()
	}
}

// Quiesced joins both concrete transactions and all entered owned work,
// including CANCEL connection preparation and explicit first writes.
// It does not mean the remote dialog, media, or device operation is complete.
func (o *OwnedClientInvite) Quiesced() <-chan struct{} { return o.quiesced }

func (o *OwnedClientInvite) beginWork() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.started || o.closing {
		return false
	}
	o.active++
	return true
}

// NextResponse is deliberately an event read, not legacy WaitAnswer: it never
// sends CANCEL, retries authentication, accepts a dialog, or writes an ACK.
// The application workflow serializes event processing and persistence.
func (o *OwnedClientInvite) NextResponse(ctx context.Context) (*sip.Response, error) {
	if ctx == nil || !o.beginWork() {
		return nil, ErrOwnedInviteState
	}
	defer o.endWork()
	select {
	case response := <-o.tx.Responses():
		if response == nil {
			return nil, ErrOwnedInviteState
		}
		o.observeBranch(response, false)
		copy := cloneOwnedBranchResponse(response)
		o.mu.Lock()
		if response.StatusCode >= 200 {
			o.finalObserved = true
		} else if response.StatusCode >= 100 {
			o.provisionalObserved = true
		}
		o.mu.Unlock()
		return copy, nil
	case <-o.tx.Done():
		return nil, errors.Join(ErrOwnedInviteState, o.tx.Err())
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// AcceptResponse publishes only the first exact response. Call only after the
// application has committed this branch's fixed material. The caller must
// serialize this method with PrepareACK and other session operations.
func (o *OwnedClientInvite) AcceptResponse(response *sip.Response) error {
	if !validOwnedInviteResponse(o.session.InviteRequest, response) {
		return ErrOwnedInviteState
	}
	id, err := sip.DialogIDFromResponse(response)
	if err != nil {
		return ErrOwnedInviteState
	}
	o.mu.Lock()
	if !o.started || o.closing || o.accepted {
		o.mu.Unlock()
		return ErrOwnedInviteState
	}
	o.accepted, o.finalObserved = true, true
	o.active++
	o.mu.Unlock()
	defer o.endWork()
	copy := response.Clone()
	copy.SetBody(append([]byte(nil), response.Body()...))
	o.session.InviteResponse, o.session.ID = copy, id
	o.session.setState(sip.DialogStateEstablished)
	return nil
}

// PrepareACK freezes one ACK, including its new Via branch and ordered routes.
// The returned copy is for validation, not an externally mutable send buffer.
// This method itself does not authorize dispatch or write network bytes.
func (o *OwnedClientInvite) PrepareACK() (*sip.Request, error) {
	o.mu.Lock()
	if o.closing || !o.accepted || o.ackPrepared {
		o.mu.Unlock()
		return nil, ErrOwnedInviteState
	}
	o.ackPrepared = true
	o.active++
	o.mu.Unlock()
	defer o.endWork()
	ack := newAckRequestUAC(o.session.InviteRequest, o.session.InviteResponse, nil)
	o.session.buildReq(ack)
	o.mu.Lock()
	o.ack = ack
	o.mu.Unlock()
	return ack.Clone(), nil
}

// WritePreparedACK is a one-shot explicit write; the durable caller must have
// committed the first-ACK permission before entering. Only matching responses
// may retransmit that same immutable ACK. No database or application lock is
// acquired in the transaction callback. First-write failure retains the handle
// and registered callback; it does not grant another explicit attempt.
func (o *OwnedClientInvite) WritePreparedACK() error {
	o.mu.Lock()
	if o.closing || o.ack == nil || o.ackStarted || o.branchesIncomplete || len(o.branches) > 1 {
		o.mu.Unlock()
		return ErrOwnedInviteState
	}
	o.ackStarted = true
	o.active++
	ack, expected := o.ack, o.session.InviteResponse.Clone()
	o.mu.Unlock()
	defer o.endWork()
	write := func() error {
		// The dialog builder ran once above. Rebuilding on retransmission can
		// append another route-set or change a CSeq; write a private clone only.
		return o.session.UA.Client.WriteRequest(ack.Clone(), func(*Client, *sip.Request) error { return nil })
	}
	if !o.tx.OnRetransmission(func(response *sip.Response) {
		if !sameOwnedInviteResponse(expected, response) {
			return
		}
		if err := write(); err != nil {
			// Report to the application workflow outside transaction work. In
			// particular, do not invoke arbitrary session OnState callbacks here.
			select {
			case o.writeErrors <- err:
			default:
			}
		}
	}) {
		return ErrOwnedInviteState
	}
	if err := write(); err != nil {
		return err
	}
	o.session.setState(sip.DialogStateConfirmed)
	return nil
}

// UnmatchedResponses is a legacy best-effort hint. ObservedBranches retains
// bounded material even if this channel is full. Neither API proves coverage.
func (o *OwnedClientInvite) UnmatchedResponses() <-chan *sip.Response { return o.unmatched }

// WriteErrors reports a retransmission write failure without claiming that the
// remote dialog ended. The owner must consume this alongside unmatched events.
func (o *OwnedClientInvite) WriteErrors() <-chan error { return o.writeErrors }

func ownedSingleTag(params sip.HeaderParams) string {
	var tag string
	count := 0
	for _, p := range params {
		if p.K == "tag" {
			tag = p.V
			count++
		}
	}
	if count != 1 {
		return ""
	}
	return tag
}

func validOwnedInviteResponse(invite *sip.Request, response *sip.Response) bool {
	if response == nil || !response.IsSuccess() {
		return false
	}
	for _, name := range []string{"Call-ID", "CSeq", "From", "To", "Contact"} {
		if len(response.GetHeaders(name)) != 1 {
			return false
		}
	}
	callID, cseq, from, to := response.CallID(), response.CSeq(), response.From(), response.To()
	return callID != nil && cseq != nil && from != nil && to != nil && response.Contact() != nil &&
		string(*callID) == string(*invite.CallID()) && cseq.MethodName == sip.INVITE && cseq.SeqNo == invite.CSeq().SeqNo &&
		from.Address.String() == invite.From().Address.String() && to.Address.String() == invite.To().Address.String() &&
		ownedSingleTag(from.Params) != "" && ownedSingleTag(from.Params) == ownedSingleTag(invite.From().Params) && ownedSingleTag(to.Params) != ""
}

func sameOwnedInviteResponse(expected, response *sip.Response) bool {
	if response == nil || response.StatusCode != expected.StatusCode {
		return false
	}
	for _, name := range []string{"Call-ID", "CSeq", "From", "To", "Contact"} {
		actual, want := response.GetHeaders(name), expected.GetHeaders(name)
		if len(actual) != 1 || len(want) != 1 || actual[0].Value() != want[0].Value() {
			return false
		}
	}
	actual, want := response.GetHeaders("Record-Route"), expected.GetHeaders("Record-Route")
	if len(actual) != len(want) {
		return false
	}
	for i := range actual {
		if actual[i].Value() != want[i].Value() {
			return false
		}
	}
	return true
}
