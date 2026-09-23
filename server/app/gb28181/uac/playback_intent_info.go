package uac

import (
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"sync"

	"github.com/emiago/sipgo/sip"
	"uvplatform.cn/uvp-gb28181/app/gb28181/mansrtsp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

// The operation's work domain owns every field and retains this object before
// persistence or connection creation. No separate dialog/transaction FSM exists.
type playbackIntentINFO struct {
	identity     playauth.DeviceSIPINFOIdentity
	branch       playauth.DeviceSIPKnownBranchIdentity
	request      *sip.Request
	tx           *sip.ClientTx
	response     *playauth.DeviceSIPINFOResponse
	prepared     bool
	workflowDone chan struct{}
	stopOnce     sync.Once
	stopDone     chan struct{}
	finished     bool
}

func (o *playbackIntentOperation) SendINFO(ctx context.Context, command playauth.DeviceSIPINFOCommand) error {
	return o.sendINFO(ctx, command, o.dua.Client.TransactionLayer().NewClientTransaction)
}

func (o *playbackIntentOperation) sendINFO(ctx context.Context, command playauth.DeviceSIPINFOCommand, create func(context.Context, *sip.Request) (*sip.ClientTx, error)) (result error) {
	if err := o.enter(ctx); err != nil {
		return err
	}
	defer o.leave()
	o.refreshBranchInventory()
	if !o.ackWritten || o.originalReleased || o.cleanup != nil || o.lost.Load() || o.lease.Context().Err() != nil || (o.info != nil && !o.info.finished) {
		return ErrPlaybackCleanupUnknown
	}
	select {
	case <-o.stopping:
		return ErrPlaybackCleanupUnknown
	default:
	}
	stored, err := o.store.LoadSIPInviteSteps(ctx, o.id)
	if err != nil {
		return err
	}
	var branch *playauth.DeviceSIPKnownBranch
	for _, step := range stored.Steps {
		if step.Identity == o.invite {
			branch = step.KnownBranch
		}
	}
	if branch == nil || len(branch.CleanupAttempts) != 0 {
		return ErrPlaybackCleanupUnknown
	}
	cseq := playbackLastINFOCSeq(o.invite.CSeq, branch)
	if cseq == math.MaxUint32 {
		return ErrPlaybackCleanupUnknown
	}
	body, err := command.Body(cseq + 1)
	if err != nil {
		return err
	}
	o.factMu.Lock()
	first := o.first
	o.factMu.Unlock()
	request, err := o.dua.BuildDialogINFO(o.request, first, cseq+1, mansrtsp.ContentType, body)
	if err != nil {
		return err
	}
	snapshot, err := snapshotPlaybackINFORequest(request, o.invite.StepID)
	if err != nil {
		return err
	}
	infoID, err := playauth.NewDeviceOperationIntentID()
	if err != nil {
		return err
	}
	c := &playbackIntentINFO{identity: playauth.DeviceSIPINFOIdentity{InfoID: infoID, Request: snapshot, Command: command},
		branch: branch.Identity, request: request, workflowDone: make(chan struct{}), stopDone: make(chan struct{})}
	c.branch.RouteSet = slices.Clone(branch.Identity.RouteSet)
	o.info, o.version = c, stored.Intent.RowVersion
	callCtx, cancel := context.WithCancel(ctx)
	stopLease := context.AfterFunc(o.lease.Context(), cancel)
	linkedDone := make(chan struct{})
	go func() {
		defer close(linkedDone)
		select {
		case <-o.stopping:
			cancel()
		case <-callCtx.Done():
		}
	}()
	defer func() {
		cancel()
		stopLease()
		<-linkedDone
		close(c.workflowDone) // runINFO and its cancellation link have both exited.
		finishCtx, stop := context.WithTimeout(context.Background(), playbackTeardownTimeout)
		defer stop()
		finishErr := o.finishINFO(finishCtx)
		if finishErr != nil || (result != nil && !errors.Is(result, ErrPlaybackRejected)) {
			o.lost.Store(true)
		}
		result = errors.Join(result, finishErr)
	}()
	return o.runINFO(callCtx, c, create)
}

func playbackLastINFOCSeq(invite uint32, branch *playauth.DeviceSIPKnownBranch) uint32 {
	if len(branch.InfoSteps) == 0 {
		return invite
	}
	return branch.InfoSteps[len(branch.InfoSteps)-1].Identity.Request.Request.CSeq
}

func (o *playbackIntentOperation) runINFO(ctx context.Context, c *playbackIntentINFO, create func(context.Context, *sip.Request) (*sip.ClientTx, error)) error {
	stored, err := o.store.PrepareSIPINFO(ctx, o.id, o.version, c.identity)
	if err != nil {
		return err
	}
	if stored.Intent.RowVersion != o.version+1 {
		return ErrPlaybackCleanupUnknown
	}
	c.prepared, o.version = true, stored.Intent.RowVersion
	stored, err = o.store.DispatchSIPINFO(ctx, o.id, o.version, c.identity.InfoID)
	if err != nil {
		return err
	}
	o.version = stored.Intent.RowVersion
	o.refreshBranchInventory()
	if ctx.Err() != nil || o.lease.Context().Err() != nil || o.lost.Load() {
		return ErrPlaybackCleanupUnknown
	}
	// TCP connect is network activity: the confirmed dispatch CAS precedes even
	// transaction creation. Keep any returned handle, including handle+error.
	c.tx, err = create(ctx, c.request)
	if err != nil || c.tx == nil {
		return errors.Join(ErrPlaybackCleanupUnknown, err)
	}
	stop := context.AfterFunc(ctx, c.tx.Terminate)
	defer stop()
	actual, err := snapshotPlaybackINFORequest(c.request, o.invite.StepID)
	if err != nil || !samePlaybackCleanupRequest(actual, c.identity.Request) {
		return errPlaybackIntentSnapshot
	}
	o.refreshBranchInventory()
	if ctx.Err() != nil || o.lease.Context().Err() != nil || o.lost.Load() {
		return ErrPlaybackCleanupUnknown
	}
	// No session.Do/buildReq here: those would rewrite the already durable CSeq.
	if err := c.tx.Init(); err != nil {
		return err
	}
	for {
		select {
		case response := <-c.tx.Responses():
			if response == nil || response.StatusCode < 100 || response.StatusCode > 699 || len(response.Body()) > 4096 || len(response.String()) > 65536 {
				return ErrPlaybackCleanupUnknown
			}
			if response.StatusCode < 200 {
				continue
			}
			fact, err := snapshotPlaybackINFOResponse(response, c.identity)
			if err != nil {
				return err
			}
			c.response = &fact // Bounded typed fact retained before any persistence.
			if fact.SIPStatus >= 300 || (fact.BodyClass == "mansrtsp" && (fact.MANSRTSPStatus < 200 || fact.MANSRTSPStatus >= 300)) {
				return ErrPlaybackRejected
			}
			return nil
		case <-c.tx.Done():
			return ErrPlaybackCleanupUnknown
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func snapshotPlaybackINFOResponse(r *sip.Response, i playauth.DeviceSIPINFOIdentity) (playauth.DeviceSIPINFOResponse, error) {
	if r == nil || r.StatusCode < 200 || r.StatusCode > 699 || len(r.Body()) > 4096 || len(r.String()) > 65536 {
		return playauth.DeviceSIPINFOResponse{}, errPlaybackIntentSnapshot
	}
	for _, name := range []string{"Via", "Call-ID", "CSeq", "From", "To"} {
		if len(r.GetHeaders(name)) != 1 {
			return playauth.DeviceSIPINFOResponse{}, errPlaybackIntentSnapshot
		}
	}
	req := i.Request.Request
	callID, seq, from, to, via := r.CallID(), r.CSeq(), r.From(), r.To(), r.Via()
	if callID == nil || seq == nil || from == nil || to == nil || via == nil || string(*callID) != req.CallID || seq.SeqNo != req.CSeq || seq.MethodName != sip.INFO ||
		from.Address.String() != req.FromURI || to.Address.String() != req.ToURI || singlePlaybackBranchTag(from.Params) != req.LocalTag || singlePlaybackBranchTag(to.Params) != i.Request.RemoteTag ||
		via.Params.GetOr("branch", "") != req.Branch {
		return playauth.DeviceSIPINFOResponse{}, errPlaybackIntentSnapshot
	}
	branches := 0
	for _, param := range via.Params {
		if strings.EqualFold(param.K, "branch") {
			branches++
		}
	}
	if branches != 1 {
		return playauth.DeviceSIPINFOResponse{}, errPlaybackIntentSnapshot
	}
	fact := playauth.DeviceSIPINFOResponse{InfoID: i.InfoID, CallID: req.CallID, CSeq: req.CSeq, LocalTag: req.LocalTag,
		RemoteTag: i.Request.RemoteTag, SIPStatus: r.StatusCode, BodyClass: "empty"}
	if r.StatusCode >= 300 {
		fact.BodyClass = "sip_rejected"
		return fact, nil
	}
	if len(r.Body()) == 0 {
		return fact, nil
	}
	if len(r.GetHeaders("Content-Type")) != 1 || r.ContentType() == nil || r.ContentType().Value() != mansrtsp.ContentType {
		return playauth.DeviceSIPINFOResponse{}, errPlaybackIntentSnapshot
	}
	parsed, err := mansrtsp.ParseResponse(r.Body())
	if err != nil || parsed.CSeq != req.CSeq {
		return playauth.DeviceSIPINFOResponse{}, errPlaybackIntentSnapshot
	}
	fact.BodyClass, fact.MANSRTSPStatus, fact.MANSRTSPCSeq = "mansrtsp", parsed.StatusCode, parsed.CSeq
	return fact, nil
}

// Caller owns work. Stop/join is bounded to the caller, but an expired deadline
// retains the strong owner and original lease for a later observation retry.
func (o *playbackIntentOperation) finishINFO(ctx context.Context) error {
	c := o.info
	if c == nil || c.finished {
		return nil
	}
	select {
	case <-c.workflowDone:
	case <-ctx.Done():
		return ctx.Err()
	}
	c.stopOnce.Do(func() {
		go func() {
			if c.tx != nil {
				c.tx.Terminate()
				<-c.tx.Quiesced()
			}
			close(c.stopDone)
		}()
	})
	select {
	case <-c.stopDone:
	case <-ctx.Done():
		return ctx.Err()
	}
	stored, err := o.store.LoadSIPInviteSteps(ctx, o.id)
	if err != nil {
		return err
	}
	var branch *playauth.DeviceSIPKnownBranch
	for _, step := range stored.Steps {
		if step.Identity == o.invite {
			branch = step.KnownBranch
		}
	}
	if branch == nil || !samePlaybackINFOBranch(branch.Identity, c.branch) {
		return ErrPlaybackCleanupUnknown
	}
	var found *playauth.DeviceSIPINFOStep
	for ai := range branch.InfoSteps {
		if branch.InfoSteps[ai].Identity.InfoID == c.identity.InfoID {
			found = &branch.InfoSteps[ai]
		}
	}
	if found == nil {
		if c.prepared || c.tx != nil {
			return ErrPlaybackCleanupUnknown
		}
	} else {
		if found.Identity.Command != c.identity.Command || !samePlaybackCleanupRequest(found.Identity.Request, c.identity.Request) {
			return ErrPlaybackCleanupUnknown
		}
		if c.response != nil {
			stored, err = o.store.ObserveSIPINFOResponse(ctx, o.id, stored.Intent.RowVersion, *c.response)
			if err != nil {
				return err
			}
		}
		stored, err = o.store.ObserveSIPINFOQuiesced(ctx, o.id, stored.Intent.RowVersion, c.identity.InfoID)
		if err != nil {
			return err
		}
	}
	o.version, c.finished = stored.Intent.RowVersion, true
	return nil
}

func samePlaybackINFOBranch(a, b playauth.DeviceSIPKnownBranchIdentity) bool {
	return a.InviteStepID == b.InviteStepID && a.CallID == b.CallID && a.LocalTag == b.LocalTag && a.RemoteTag == b.RemoteTag &&
		a.CSeq == b.CSeq && a.StatusCode == b.StatusCode && a.RemoteTarget == b.RemoteTarget && slices.Equal(a.RouteSet, b.RouteSet)
}
