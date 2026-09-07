package uac

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

const maxPlaybackIntentOperations = 64

// This registry is a bounded, strong cleanup owner, not a legacy dialog store.
// None of these objects expose Do/Bye/INFO or claim remote completion.
type playbackIntentOperation struct {
	store                 *playauth.DeviceOperationIntentStore
	id                    playauth.DeviceOperationIntentIdentity
	lease                 playauth.DeviceOperationLease
	owned                 *sipgo.OwnedClientInvite
	request               *sip.Request
	invite                playauth.DeviceSIPInviteIdentity
	input                 PlaybackInviteRequest
	version               int64
	work                  chan struct{} // Serial workflow admission; never an application mutex.
	started               bool
	lost                  atomic.Bool
	stopping              chan struct{}
	stopOnce, releaseOnce sync.Once
	stopDone              chan struct{}
	readerCancel          context.CancelFunc
	readerDone            chan struct{}
	events                chan struct{} // Coalesced notification, never a response backlog.
	factMu                sync.Mutex    // Only copies facts, never calls DB/SIP or waits.
	first                 *sip.Response
	unsupported           bool
	provisional, rejected bool
	readerErr             error
}

func (u *UAC) beginPlaybackIntentOperation(ctx context.Context, store *playauth.DeviceOperationIntentStore, barrier *playauth.DeviceOperationBarrier, id playauth.DeviceOperationIntentIdentity, version int64, stepID string, input PlaybackInviteRequest) (*playbackIntentOperation, error) {
	if ctx == nil || u == nil || u.client == nil || u.client.TxRequester != nil || store == nil || barrier == nil || id.Kind != "playback" || id.TargetScope != "channel" || input.DeviceID != id.DeviceCode || input.ChannelID != id.TargetCode {
		return nil, ErrPlaybackUnavailable
	}
	o := &playbackIntentOperation{store: store, id: id, input: input, work: make(chan struct{}, 1), stopping: make(chan struct{}), stopDone: make(chan struct{}), events: make(chan struct{}, 1)}
	u.playbackIntentMu.Lock()
	if len(u.playbackIntents) >= maxPlaybackIntentOperations || u.playbackIntents[id.OperationID] != nil {
		u.playbackIntentMu.Unlock()
		return nil, ErrPlaybackUnavailable
	}
	if u.playbackIntents == nil {
		u.playbackIntents = make(map[string]*playbackIntentOperation)
	}
	u.playbackIntents[id.OperationID] = o
	u.playbackIntentMu.Unlock()
	removeUnused := func() {
		if o.lease != nil {
			o.lease.Release()
		}
		u.playbackIntentMu.Lock()
		delete(u.playbackIntents, id.OperationID)
		u.playbackIntentMu.Unlock()
	}
	lease, err := barrier.BeginEpoch(ctx, id.DeviceCode, id.DeviceEpoch)
	if err != nil {
		removeUnused()
		return nil, err
	}
	o.lease = lease
	request, stored, err := u.prepareStoredPlaybackInvite(lease.Context(), store, id, version, stepID, input)
	if err != nil {
		removeUnused()
		return nil, err
	}
	o.request, o.version = request, stored.Intent.RowVersion
	for _, step := range stored.Steps {
		if step.Identity.StepID == stepID {
			o.invite = step.Identity
		}
	}
	dua := &sipgo.DialogUA{Client: u.client, ContactHDR: *request.Contact()}
	o.owned, err = dua.PrepareWriteInviteOwned(lease.Context(), request)
	if err != nil {
		removeUnused()
		return nil, err
	}
	snapshot, err := snapshotPlaybackIntentRequest(o.owned.Request())
	if err != nil || playbackIntentStorageIdentity(stepID, snapshot) != o.invite {
		_ = o.CloseLocal(context.Background())
		return o, errPlaybackIntentSnapshot
	}
	// Cancellation requests cleanup; it never drops the reader or the lease.
	go func() {
		select {
		case <-lease.Context().Done():
		case <-o.stopping:
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), playbackTeardownTimeout)
		_ = o.Cancel(cleanupCtx)
		cancel()
		cleanupCtx, cancel = context.WithTimeout(context.Background(), playbackTeardownTimeout)
		_ = o.CloseLocal(cleanupCtx)
		cancel()
	}()
	return o, nil
}

func (o *playbackIntentOperation) enter(ctx context.Context) error {
	if ctx == nil {
		return ErrPlaybackUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case o.work <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (o *playbackIntentOperation) leave() { <-o.work }

func (o *playbackIntentOperation) Start(ctx context.Context) error {
	if err := o.enter(ctx); err != nil {
		return err
	}
	defer o.leave()
	if o.started || o.lost.Load() || o.lease.Context().Err() != nil {
		return ErrPlaybackUnavailable
	}
	select {
	case <-o.stopping:
		return ErrPlaybackUnavailable
	default:
	}
	o.started = true
	callCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(o.lease.Context(), cancel)
	defer func() { stop(); cancel() }()
	stored, err := o.store.DispatchSIPInviteStep(callCtx, o.id, o.version, o.invite.StepID)
	if err != nil {
		o.lost.Store(true)
		return err
	}
	o.version = stored.Intent.RowVersion
	if o.lease.Context().Err() != nil || callCtx.Err() != nil {
		o.lost.Store(true)
		return ErrPlaybackCleanupUnknown
	}
	err = o.owned.Start()
	o.startReader()
	if err != nil {
		o.lost.Store(true)
	}
	return err
}

func (o *playbackIntentOperation) capture(response *sip.Response) {
	if response == nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return
	}
	o.factMu.Lock()
	defer o.factMu.Unlock()
	if len(response.Body()) > 65536 || len(response.String()) > 65536 {
		o.unsupported = true
		o.lost.Store(true)
		return
	}
	if o.first == nil {
		o.first = response.Clone()
		o.first.SetBody(append([]byte(nil), response.Body()...))
	} else if !samePlaybackIntentResponse(o.first, response) {
		o.unsupported = true
		o.lost.Store(true)
	}
}

func samePlaybackIntentResponse(a, b *sip.Response) bool {
	if a.StatusCode != b.StatusCode {
		return false
	}
	for _, name := range []string{"Call-ID", "CSeq", "From", "To", "Contact", "Record-Route"} {
		ah, bh := a.GetHeaders(name), b.GetHeaders(name)
		if len(ah) != len(bh) {
			return false
		}
		for i := range ah {
			if ah[i].Value() != bh[i].Value() {
				return false
			}
		}
	}
	return true
}

func (o *playbackIntentOperation) startReader() {
	ctx, cancel := context.WithCancel(context.Background())
	o.readerCancel, o.readerDone = cancel, make(chan struct{})
	var readers sync.WaitGroup
	readers.Add(2)
	go func() {
		defer readers.Done()
		for {
			response, err := o.owned.NextResponse(ctx)
			o.capture(response) // Strong copy precedes queueing or persistence.
			o.factMu.Lock()
			if response != nil {
				o.provisional = o.provisional || response.StatusCode < 200
				o.rejected = o.rejected || response.StatusCode >= 300
			}
			if err != nil {
				o.readerErr = err
			}
			o.factMu.Unlock()
			select {
			case o.events <- struct{}{}:
			default:
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		defer readers.Done()
		for {
			select {
			case response := <-o.owned.UnmatchedResponses():
				o.capture(response)
			case <-o.owned.WriteErrors():
				o.lost.Store(true)
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() { readers.Wait(); close(o.readerDone) }()
}

// Facts are bounded and sticky; the reader cannot be held up by database work
// or a burst of provisional responses. Only the workflow consumes this signal.
func (o *playbackIntentOperation) waitResponse(ctx context.Context, finalOnly bool, leaseDone <-chan struct{}) (*sip.Response, error) {
	for {
		select {
		case <-o.stopping:
			return nil, ErrPlaybackCleanupUnknown
		case <-leaseDone:
			return nil, ErrPlaybackCleanupUnknown
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		o.factMu.Lock()
		first, provisional, rejected, unsupported, err := o.first, o.provisional, o.rejected, o.unsupported, o.readerErr
		o.factMu.Unlock()
		if unsupported {
			return nil, ErrPlaybackCleanupUnknown
		}
		if first != nil {
			return first, nil
		}
		if rejected {
			return nil, ErrPlaybackRejected
		}
		if !finalOnly && provisional {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		select {
		case <-o.events:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-leaseDone:
			return nil, ErrPlaybackCleanupUnknown
		case <-o.stopping:
			return nil, ErrPlaybackCleanupUnknown
		}
	}
}

// ReadAndAccept drives the real first-branch workflow. It does not publish an
// established legacy playback; its operation remains a cleanup-pending owner.
func (o *playbackIntentOperation) ReadAndAccept(ctx context.Context) (PlaybackDialogMetadata, error) {
	if err := o.enter(ctx); err != nil {
		return PlaybackDialogMetadata{}, err
	}
	defer o.leave()
	if !o.started || o.readerDone == nil {
		return PlaybackDialogMetadata{}, ErrPlaybackUnavailable
	}
	response, err := o.waitResponse(ctx, true, o.lease.Context().Done())
	if err != nil {
		return PlaybackDialogMetadata{}, err
	}
	persistCtx, cancel := context.WithTimeout(context.Background(), playbackTeardownTimeout)
	stored, err := observeStoredPlaybackBranch(persistCtx, o.store, o.id, o.version, o.invite, o.request, response)
	cancel()
	if err != nil {
		o.lost.Store(true)
		return PlaybackDialogMetadata{}, err
	}
	o.version = stored.Intent.RowVersion
	if o.lost.Load() || o.lease.Context().Err() != nil || ctx.Err() != nil {
		return PlaybackDialogMetadata{}, ErrPlaybackCleanupUnknown
	}
	if err := o.owned.AcceptResponse(response); err != nil {
		o.lost.Store(true)
		return PlaybackDialogMetadata{}, err
	}
	if _, err := o.owned.PrepareACK(); err != nil {
		o.lost.Store(true)
		return PlaybackDialogMetadata{}, err
	}
	var branch *playauth.DeviceSIPKnownBranch
	for _, step := range stored.Steps {
		if step.Identity.StepID == o.invite.StepID {
			branch = step.KnownBranch
		}
	}
	if branch == nil {
		o.lost.Store(true)
		return PlaybackDialogMetadata{}, ErrPlaybackCleanupUnknown
	}
	stored, err = o.store.DispatchSIPKnownBranchACK(ctx, o.id, o.version, branch.Identity)
	if err != nil {
		o.lost.Store(true)
		return PlaybackDialogMetadata{}, err
	}
	o.version = stored.Intent.RowVersion
	if o.lease.Context().Err() != nil || ctx.Err() != nil {
		o.lost.Store(true)
		return PlaybackDialogMetadata{}, ErrPlaybackCleanupUnknown
	}
	if err := o.owned.WritePreparedACK(); err != nil {
		o.lost.Store(true)
		return PlaybackDialogMetadata{}, err
	}
	metadata := (&sipgoPlaybackDialog{session: o.owned.Session()}).Metadata()
	metadata.DeviceID, metadata.ChannelID, metadata.SSRC = o.input.DeviceID, o.input.ChannelID, o.input.SSRC
	return metadata, nil
}

func (o *playbackIntentOperation) Cancel(ctx context.Context) error {
	if err := o.enter(ctx); err != nil {
		return err
	}
	defer o.leave()
	if !o.started || o.readerDone == nil || o.lost.Load() {
		return ErrPlaybackCleanupUnknown
	}
	response, err := o.waitResponse(ctx, false, nil)
	if err != nil {
		return err
	}
	if response != nil {
		return ErrPlaybackCleanupUnknown
	}
	request, err := o.owned.PrepareCancel(ctx)
	if err != nil {
		return err
	}
	stored, err := prepareStoredPlaybackCancel(ctx, o.store, o.id, o.version, o.invite, request)
	if err != nil {
		o.lost.Store(true)
		return err
	}
	o.version = stored.Intent.RowVersion
	var cancel *playauth.DeviceSIPCancel
	for _, step := range stored.Steps {
		if step.Identity.StepID == o.invite.StepID {
			cancel = step.Cancel
		}
	}
	if cancel == nil {
		o.lost.Store(true)
		return ErrPlaybackCleanupUnknown
	}
	stored, err = o.store.DispatchSIPCancel(ctx, o.id, o.version, cancel.Identity)
	if err != nil {
		o.lost.Store(true)
		return err
	}
	o.version = stored.Intent.RowVersion
	if err := o.owned.StartCancel(); err != nil {
		return err
	}
	for {
		response, err := o.owned.NextCancelResponse(ctx)
		if err != nil {
			return err
		}
		if response.StatusCode >= 200 {
			break
		}
	}
	// Even CANCEL 200 is not INVITE completion. Keep the original reader alive
	// through a final response or the caller's bounded cleanup deadline.
	_, err = o.waitResponse(ctx, true, nil)
	if errors.Is(err, ErrPlaybackRejected) {
		return nil
	}
	return err
}

// CloseLocal/recovery can only persist exact facts and join local work. Readback
// never restores network permission. A durable handoff still means pending,
// not closed: the registry retains this session and never advances a watermark.
func (o *playbackIntentOperation) CloseLocal(ctx context.Context) error {
	if ctx == nil {
		return ErrPlaybackUnavailable
	}
	o.lost.Store(true)
	o.stopOnce.Do(func() {
		close(o.stopping)
		go func() { o.owned.Terminate(); <-o.owned.Quiesced(); close(o.stopDone) }()
	})
	select {
	case <-o.stopDone:
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := o.enter(ctx); err != nil {
		return err
	}
	defer o.leave()
	if o.readerCancel != nil {
		o.readerCancel()
		select {
		case <-o.readerDone:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	// All callback producers and readers have exited. Do not let cancellation
	// win a select over an already queued unsupported branch or write error.
	for draining := true; draining; {
		select {
		case response := <-o.owned.UnmatchedResponses():
			o.capture(response)
		case <-o.owned.WriteErrors():
			o.lost.Store(true)
		default:
			draining = false
		}
	}
	o.factMu.Lock()
	first, unsupported := o.first, o.unsupported
	o.factMu.Unlock()
	if unsupported {
		return ErrPlaybackCleanupUnknown
	}
	persistCtx, cancel := context.WithTimeout(ctx, playbackTeardownTimeout)
	defer cancel()
	loaded, err := o.store.LoadSIPInviteSteps(persistCtx, o.id)
	if err != nil {
		return err
	}
	matched := false
	for _, step := range loaded.Steps {
		if step.Identity == o.invite {
			matched = true
		}
	}
	if !matched {
		return ErrPlaybackCleanupUnknown
	}
	if first != nil {
		_, err = observeStoredPlaybackBranch(persistCtx, o.store, o.id, loaded.Intent.RowVersion, o.invite, o.request, first)
		if err != nil {
			return errors.Join(ErrPlaybackCleanupUnknown, err)
		}
	}
	o.releaseOnce.Do(o.lease.Release)
	return nil
}
