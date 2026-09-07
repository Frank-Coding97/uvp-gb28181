package sipgo

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/emiago/sipgo/sip"
)

var ErrOwnedCleanupState = errors.New("owned branch cleanup unavailable or invalid state")

// OwnedBranchCleanup owns ONE dedicated cleanup ACK and BYE attempt. It is
// not authorization or a dialog recovery executor. The durable caller must
// validate both snapshots, commit each dispatch CAS, and retain this handle
// until Quiesced on every result. Never use its transaction to call Init.
type OwnedBranchCleanup struct {
	client                             *Client
	ack, bye                           *sip.Request
	mu                                 sync.Mutex
	tx                                 *sip.ClientTx
	prepared, ready, closing, txExited bool
	ackStarted, ackWritten, byeStarted bool
	active                             int
	quiesced                           chan struct{}
	quiescedClosed                     bool
}

// NewBranchCleanup builds without connecting or writing. The caller supplies
// the next durable BYE sequence, not a recovered permission. It must first
// quiesce the original dialog owner and validate the original branch evidence.
// This deliberately excludes RewriteContact and implicit authentication.
func (ua *DialogUA) NewBranchCleanup(invite *sip.Request, response *sip.Response, byeCSeq uint32) (*OwnedBranchCleanup, error) {
	if ua == nil || ua.Client == nil || ua.Client.UserAgent == nil || ua.Client.TransactionLayer() == nil || ua.Client.TxRequester != nil || ua.RewriteContact ||
		!validCleanupInvite(invite) || !validOwnedInviteResponse(invite, response) || byeCSeq <= invite.CSeq().SeqNo || len(response.String()) > 65536 {
		return nil, ErrOwnedCleanupState
	}
	if !validCleanupTarget(response.Contact().Address) || len(response.GetHeaders("Record-Route")) > 8 {
		return nil, ErrOwnedCleanupState
	}
	for _, h := range response.GetHeaders("Record-Route") {
		var uri sip.Uri
		params := sip.NewParams()
		name, err := sip.ParseAddressValue(h.Value(), &uri, &params)
		if err != nil || name != "" || len(params) != 0 || !validCleanupTarget(uri) {
			return nil, ErrOwnedCleanupState
		}
	}
	requestCopy, responseCopy := cloneOwnedCleanupRequest(invite), response.Clone()
	requestCopy.SetBody(nil) // Only dialog headers are used, never replay SDP.
	responseCopy.SetBody(nil)
	session := &DialogClientSession{Dialog: Dialog{InviteRequest: requestCopy, InviteResponse: responseCopy}, UA: ua}
	session.Dialog.Init()
	ack := newAckRequestUAC(requestCopy, responseCopy, nil)
	session.buildReq(ack)
	bye := newByeRequestUAC(requestCopy, responseCopy, nil)
	via := invite.Via().Clone()
	via.Params.Add("branch", sip.GenerateBranchN(16))
	bye.PrependHeader(via)
	bye.AppendHeader(sip.HeaderClone(invite.Contact()))
	session.lastCSeqNo.Store(byeCSeq - 1)
	session.buildReq(bye)
	for _, r := range []*sip.Request{ack, bye} {
		target := r.Recipient
		if route := r.Route(); route != nil {
			target = route.Address
		}
		port := target.Port
		if port == 0 {
			port = 5060
		}
		r.SetDestination(net.JoinHostPort(strings.Trim(target.Host, "[]"), strconv.Itoa(port)))
	}
	return &OwnedBranchCleanup{client: ua.Client, ack: ack, bye: bye, quiesced: make(chan struct{})}, nil
}

func validCleanupTarget(uri sip.Uri) bool {
	// sipgo's structured builders leave Scheme empty; Uri.String emits sip.
	return (uri.Scheme == "" || uri.Scheme == "sip") && uri.Host != "" && uri.Port >= 0 && uri.Port <= 65535 && uri.Password == "" &&
		!uri.Wildcard && !uri.HierarhicalSlashes && len(uri.Headers) == 0 && len(uri.String()) <= 1024 && !strings.ContainsAny(uri.String(), "\r\n\x00\t ")
}

func validCleanupInvite(r *sip.Request) bool {
	if r == nil || r.Method != sip.INVITE || len(r.String()) > 65536 || len(r.GetHeaders("Route")) != 0 {
		return false
	}
	for _, h := range []string{"Via", "From", "To", "Call-ID", "CSeq", "Contact"} {
		if len(r.GetHeaders(h)) != 1 {
			return false
		}
	}
	v, c := r.Via(), r.Contact()
	return v != nil && c != nil && r.From() != nil && r.To() != nil && r.CallID() != nil && *r.CallID() != "" &&
		r.CSeq() != nil && r.CSeq().MethodName == sip.INVITE && r.CSeq().SeqNo > 0 && ownedSingleTag(r.From().Params) != "" &&
		(r.MessageData.Transport() == "UDP" || r.MessageData.Transport() == "TCP") && v.Transport == r.MessageData.Transport() &&
		v.Host != "" && v.Port > 0 && v.Port <= 65535 && v.Params.GetOr("branch", "") != "" &&
		validCleanupTarget(c.Address) && c.Address.Port > 0
}

// sipgo treats primitive headers as immutable and Clone shares their pointers.
// Our observation API promises caller isolation, including Call-ID and limits.
func cloneOwnedCleanupRequest(r *sip.Request) *sip.Request {
	copy := r.Clone()
	copy.SetBody(append([]byte(nil), r.Body()...))
	for _, h := range r.Headers() {
		switch h := h.(type) {
		case *sip.CallIDHeader:
			v := *h
			copy.ReplaceHeader(&v)
		case *sip.MaxForwardsHeader:
			v := *h
			copy.ReplaceHeader(&v)
		case *sip.ContentLengthHeader:
			v := *h
			copy.ReplaceHeader(&v)
		case *sip.ContentTypeHeader:
			v := *h
			copy.ReplaceHeader(&v)
		case *sip.ExpiresHeader:
			v := *h
			copy.ReplaceHeader(&v)
		}
	}
	return copy
}

func (o *OwnedBranchCleanup) ACKRequest() *sip.Request { return cloneOwnedCleanupRequest(o.ack) }
func (o *OwnedBranchCleanup) BYERequest() *sip.Request { return cloneOwnedCleanupRequest(o.bye) }
func (o *OwnedBranchCleanup) Transaction() *sip.ClientTx {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.tx
}

// PrepareBYE may connect TCP, but never writes SIP or calls Init. The owner
// already exists before preparation begins, including on failure/cancellation.
func (o *OwnedBranchCleanup) PrepareBYE(ctx context.Context) (*sip.Request, error) {
	return o.prepareBYE(ctx, o.client.TransactionLayer().NewClientTransaction)
}

func (o *OwnedBranchCleanup) prepareBYE(ctx context.Context, create func(context.Context, *sip.Request) (*sip.ClientTx, error)) (*sip.Request, error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, ErrOwnedCleanupState
	}
	o.mu.Lock()
	if o.closing || o.prepared {
		o.mu.Unlock()
		return nil, ErrOwnedCleanupState
	}
	o.prepared = true
	o.active++
	o.mu.Unlock()
	defer o.endWork()
	r := cloneOwnedCleanupRequest(o.bye)
	tx, err := create(ctx, r)
	o.mu.Lock()
	o.tx = tx // Even a factory returning both a handle and an error is owned.
	stop := o.closing || err != nil || ctx.Err() != nil || tx == nil || r.String() != o.bye.String() ||
		r.MessageData.Destination() != o.bye.MessageData.Destination() || r.MessageData.Transport() != o.bye.MessageData.Transport()
	o.ready, o.closing = !stop, stop
	o.mu.Unlock()
	if tx != nil {
		go func() {
			<-tx.Quiesced()
			o.mu.Lock()
			o.txExited, o.closing = true, true
			o.closeQuiescedLocked()
			o.mu.Unlock()
		}()
	}
	if stop {
		if tx != nil {
			tx.Terminate()
		}
		return nil, errors.Join(ErrOwnedCleanupState, err, ctx.Err())
	}
	return cloneOwnedCleanupRequest(r), nil
}

// WriteACK requires the confirmed dedicated ACK CAS. It writes once through
// the already-owned BYE connection; no new transport lookup or builder runs.
// Only successful return from this attempt can enable its StartBYE.
func (o *OwnedBranchCleanup) WriteACK() error {
	o.mu.Lock()
	if o.closing || !o.ready || o.ackStarted {
		o.mu.Unlock()
		return ErrOwnedCleanupState
	}
	o.ackStarted = true
	o.active++
	tx := o.tx
	o.mu.Unlock()
	defer o.endWork()
	err := tx.Connection().WriteMsg(cloneOwnedCleanupRequest(o.ack))
	o.mu.Lock()
	o.ackWritten = err == nil
	o.mu.Unlock()
	return err
}

// StartBYE requires the separate confirmed BYE CAS after WriteACK succeeded.
// A failure consumes this explicit attempt, including a partially written BYE.
func (o *OwnedBranchCleanup) StartBYE() error {
	o.mu.Lock()
	if o.closing || !o.ready || !o.ackWritten || o.byeStarted {
		o.mu.Unlock()
		return ErrOwnedCleanupState
	}
	o.byeStarted = true
	o.active++
	tx := o.tx
	o.mu.Unlock()
	defer o.endWork()
	if err := tx.Init(); err != nil {
		tx.Terminate()
		return err
	}
	return nil
}

// NextResponse is only observation. Neither 481, Done, timeout nor Quiesced
// proves remote closure; a 2xx still needs exact durable branch attribution.
func (o *OwnedBranchCleanup) NextResponse(ctx context.Context) (*sip.Response, error) {
	if ctx == nil {
		return nil, ErrOwnedCleanupState
	}
	o.mu.Lock()
	if o.closing || !o.byeStarted {
		o.mu.Unlock()
		return nil, ErrOwnedCleanupState
	}
	o.active++
	tx := o.tx
	o.mu.Unlock()
	defer o.endWork()
	select {
	case r := <-tx.Responses():
		if r == nil || r.StatusCode < 100 || r.StatusCode > 699 || len(r.String()) > 65536 {
			return nil, ErrOwnedCleanupState
		}
		for _, name := range []string{"From", "To", "Call-ID", "CSeq"} {
			h := r.GetHeaders(name)
			if len(h) != 1 || h[0].Value() != o.bye.GetHeader(name).Value() {
				return nil, ErrOwnedCleanupState
			}
		}
		copy := r.Clone()
		copy.SetBody(append([]byte(nil), r.Body()...))
		return copy, nil
	case <-tx.Done():
		return nil, errors.Join(ErrOwnedCleanupState, tx.Err())
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (o *OwnedBranchCleanup) endWork() {
	o.mu.Lock()
	o.active--
	o.closeQuiescedLocked()
	o.mu.Unlock()
}

func (o *OwnedBranchCleanup) closeQuiescedLocked() {
	if o.closing && o.active == 0 && (o.tx == nil || o.txExited) && !o.quiescedClosed {
		o.quiescedClosed = true
		close(o.quiesced)
	}
}

func (o *OwnedBranchCleanup) Terminate() {
	o.mu.Lock()
	o.closing = true
	tx := o.tx
	o.closeQuiescedLocked()
	o.mu.Unlock()
	if tx != nil {
		tx.Terminate()
	}
}

// Quiesced joins connection preparation, the actual BYE transaction and every
// entered explicit ACK/BYE write/read. It is not a remote completion receipt.
func (o *OwnedBranchCleanup) Quiesced() <-chan struct{} { return o.quiesced }
