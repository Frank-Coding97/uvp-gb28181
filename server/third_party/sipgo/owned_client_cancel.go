package sipgo

import (
	"context"
	"errors"

	"github.com/emiago/sipgo/sip"
)

// PrepareCancel retains the original INVITE's sole CANCEL transaction before
// any SIP write. It may connect TCP. A returned request is only a validation
// copy, not a durable dispatch permit. The parent owns the child on all paths.
func (o *OwnedClientInvite) PrepareCancel(ctx context.Context) (*sip.Request, error) {
	return o.prepareCancel(ctx, o.session.UA.Client.TransactionLayer().NewClientTransaction)
}

func (o *OwnedClientInvite) prepareCancel(ctx context.Context, create func(context.Context, *sip.Request) (*sip.ClientTx, error)) (*sip.Request, error) {
	if ctx == nil {
		return nil, ErrOwnedInviteState
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	o.mu.Lock()
	if !o.started || o.closing || o.finalObserved || o.cancelPrepared {
		o.mu.Unlock()
		return nil, ErrOwnedInviteState
	}
	o.cancelPrepared = true
	o.active++ // Includes connection preparation before cancelTx is installed.
	invite := o.session.InviteRequest
	request := newCancelRequest(invite)
	request.AppendHeader(sip.HeaderClone(invite.MaxForwards()))
	request.AppendHeader(sip.HeaderClone(invite.Contact()))
	request.AppendHeader(&sip.CSeqHeader{SeqNo: invite.CSeq().SeqNo, MethodName: sip.CANCEL})
	request.SetBody(nil)
	request.SetTransport(invite.MessageData.Transport())
	request.SetDestination(invite.MessageData.Destination())
	o.mu.Unlock()
	defer o.endWork()
	// CANCEL is out of dialog: do not apply a provisional response's routes,
	// remote Contact, CSeq changes or destination rewriting via buildReq.
	tx, err := create(ctx, request)
	if tx == nil {
		return nil, errors.Join(ErrOwnedInviteState, err)
	}
	o.mu.Lock()
	o.cancelTx = tx
	stop := o.closing || o.finalObserved || err != nil || ctx.Err() != nil
	o.mu.Unlock()
	go func() {
		<-tx.Quiesced()
		o.mu.Lock()
		o.cancelExited = true
		o.closeQuiescedLocked()
		o.mu.Unlock()
	}()
	if stop {
		tx.Terminate()
		return nil, errors.Join(ErrOwnedInviteState, err, ctx.Err())
	}
	return request.Clone(), nil
}

// StartCancel is once-only and requires an observed provisional response.
// The caller must first commit the exact CANCEL's unique dispatch CAS. Final
// response observation and this start reservation linearize under the parent
// mutex; a later 2xx is still a live fact, never cancellation success.
func (o *OwnedClientInvite) StartCancel() error {
	o.mu.Lock()
	if o.closing || o.finalObserved || !o.provisionalObserved || o.cancelTx == nil || o.cancelExited || o.cancelStarted {
		o.mu.Unlock()
		return ErrOwnedInviteState
	}
	o.cancelStarted = true
	o.active++
	tx := o.cancelTx
	o.mu.Unlock()
	defer o.endWork()
	if err := tx.Init(); err != nil {
		tx.Terminate()
		return err
	}
	return nil
}

// CancelTransaction exposes the retained handle for observation, not separate
// ownership or permission to call Init. Parent Terminate/Quiesced cover it.
func (o *OwnedClientInvite) CancelTransaction() *sip.ClientTx {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.cancelTx
}

// NextCancelResponse performs no implicit SIP work. A 200 here proves only a
// response to CANCEL; it cannot close the INVITE/dialog or complete cleanup.
func (o *OwnedClientInvite) NextCancelResponse(ctx context.Context) (*sip.Response, error) {
	if ctx == nil {
		return nil, ErrOwnedInviteState
	}
	o.mu.Lock()
	if !o.cancelStarted || o.closing || o.cancelTx == nil {
		o.mu.Unlock()
		return nil, ErrOwnedInviteState
	}
	o.active++
	tx := o.cancelTx
	o.mu.Unlock()
	defer o.endWork()
	select {
	case response := <-tx.Responses():
		if response == nil {
			return nil, ErrOwnedInviteState
		}
		copy := response.Clone()
		copy.SetBody(append([]byte(nil), response.Body()...))
		return copy, nil
	case <-tx.Done():
		return nil, errors.Join(ErrOwnedInviteState, tx.Err())
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
