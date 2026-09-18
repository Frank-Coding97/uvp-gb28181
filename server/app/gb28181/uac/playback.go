package uac

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"

	"uvplatform.cn/uvp-gb28181/app/gb28181/mansrtsp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
)

var (
	ErrPlaybackUnavailable    = errors.New("playback dialog unavailable")
	ErrPlaybackClosed         = errors.New("playback dialog closed")
	ErrPlaybackRejected       = errors.New("playback control rejected")
	ErrPlaybackCleanupUnknown = errors.New("playback dialog cleanup unconfirmed")
	ErrPlaybackACKPending     = errors.New("playback ACK unconfirmed; cleanup required")
)

const playbackTeardownTimeout = 2 * time.Second

type PlaybackInviteRequest struct {
	DeviceID, ChannelID, Destination, Transport, SSRC, SDP string
}

type PlaybackDialogMetadata struct {
	CallID, LocalTag, RemoteTag, RemoteTarget string
	RouteSet                                  []string
	CSeq                                      uint32
	StatusCode                                int
	DeviceID, ChannelID, SSRC                 string
}

type PlaybackInfoAction string

const (
	PlaybackInfoPlay     PlaybackInfoAction = "play"
	PlaybackInfoPause    PlaybackInfoAction = "pause"
	PlaybackInfoResume   PlaybackInfoAction = "resume"
	PlaybackInfoSeek     PlaybackInfoAction = "seek"
	PlaybackInfoScale    PlaybackInfoAction = "scale"
	PlaybackInfoTeardown PlaybackInfoAction = "teardown"
)

type PlaybackInfoRequest struct {
	Action                    PlaybackInfoAction
	Position, SegmentDuration time.Duration
	Scale                     float64
}

type PlaybackControlResult = mansrtsp.Result

type playbackDialog interface {
	WaitAnswer(context.Context) error
	Ack(context.Context) error
	Do(context.Context, *sip.Request) (*sip.Response, error)
	Bye(context.Context) error
	Close() error
	StatusCode() int
	Metadata() PlaybackDialogMetadata
	ReadBye(*sip.Request, sip.ServerTransaction) error
}

type playbackDialogTransport interface {
	WriteInvite(context.Context, *sip.Request) (playbackDialog, error)
}

type sipgoPlaybackDialogTransport struct {
	client *sipgo.Client
}

func (t *sipgoPlaybackDialogTransport) WriteInvite(ctx context.Context, req *sip.Request) (playbackDialog, error) {
	contact := req.Contact()
	if contact == nil {
		return nil, fmt.Errorf("PLAYBACK INVITE 缺少 Contact")
	}
	cache := sipgo.NewDialogClientCache(t.client, *contact)
	dialog, err := cache.WriteInvite(ctx, req)
	if err != nil {
		return nil, err
	}
	return &sipgoPlaybackDialog{session: dialog}, nil
}

type sipgoPlaybackDialog struct {
	session *sipgo.DialogClientSession
}

func (d *sipgoPlaybackDialog) WaitAnswer(ctx context.Context) error {
	return d.session.WaitAnswer(ctx, sipgo.AnswerOptions{})
}
func (d *sipgoPlaybackDialog) Ack(ctx context.Context) error { return d.session.Ack(ctx) }
func (d *sipgoPlaybackDialog) Do(ctx context.Context, req *sip.Request) (*sip.Response, error) {
	return d.session.Do(ctx, req)
}
func (d *sipgoPlaybackDialog) Bye(ctx context.Context) error {
	// ReadBye and ACK retransmission failures can end sipgo's local state
	// without an acknowledged outbound BYE. Record operations are serialized.
	if d.session.LoadState() == sip.DialogStateEnded {
		return ErrPlaybackCleanupUnknown
	}
	if err := d.session.Bye(ctx); err != nil {
		return err
	}
	// Require the normal cancellation cause, not errors.Is: an asynchronous
	// ACK error can wrap context.Canceled. The state/cause publication gap is
	// also unknown, never a successful receipt.
	if d.session.LoadState() != sip.DialogStateEnded || context.Cause(d.session.Context()) != context.Canceled {
		return ErrPlaybackCleanupUnknown
	}
	return nil
}
func (d *sipgoPlaybackDialog) Close() error { return d.session.Close() }
func (d *sipgoPlaybackDialog) ReadBye(req *sip.Request, tx sip.ServerTransaction) error {
	return d.session.ReadBye(req, tx)
}
func (d *sipgoPlaybackDialog) StatusCode() int {
	if d.session.InviteResponse == nil {
		return 0
	}
	return int(d.session.InviteResponse.StatusCode)
}
func (d *sipgoPlaybackDialog) Metadata() PlaybackDialogMetadata {
	metadata := PlaybackDialogMetadata{}
	request, response := d.session.InviteRequest, d.session.InviteResponse
	if request != nil {
		if callID := request.CallID(); callID != nil {
			metadata.CallID = string(*callID)
		}
		if from := request.From(); from != nil {
			metadata.LocalTag, _ = from.Params.Get("tag")
		}
		if cseq := request.CSeq(); cseq != nil {
			metadata.CSeq = cseq.SeqNo
		}
	}
	if response == nil {
		return metadata
	}
	metadata.StatusCode = int(response.StatusCode)
	if to := response.To(); to != nil {
		metadata.RemoteTag, _ = to.Params.Get("tag")
	}
	if contact := response.Contact(); contact != nil {
		metadata.RemoteTarget = contact.Address.String()
	}
	for _, route := range response.GetHeaders("record-route") {
		metadata.RouteSet = append(metadata.RouteSet, route.Value())
	}
	return metadata
}

type playbackDialogRecord struct {
	mu             sync.Mutex
	dialog         playbackDialog
	metadata       PlaybackDialogMetadata
	closed         bool
	closing        bool
	byeConfirmed   bool
	ackPending     bool
	inboundByeCSeq *uint32
}

type PlaybackDialogStore struct {
	mu        sync.RWMutex
	transport playbackDialogTransport
	dialogs   map[string]*playbackDialogRecord
}

func NewPlaybackDialogStore(transport playbackDialogTransport) *PlaybackDialogStore {
	return &PlaybackDialogStore{transport: transport, dialogs: make(map[string]*playbackDialogRecord)}
}

func (s *PlaybackDialogStore) get(callID string) *playbackDialogRecord {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dialogs[callID]
}

func (s *PlaybackDialogStore) put(record *playbackDialogRecord) {
	s.mu.Lock()
	s.dialogs[record.metadata.CallID] = record
	s.mu.Unlock()
}

func (s *PlaybackDialogStore) remove(callID string, record *playbackDialogRecord) {
	s.mu.Lock()
	if s.dialogs[callID] == record {
		delete(s.dialogs, callID)
	}
	s.mu.Unlock()
}

func (u *UAC) buildPlaybackInviteRequest(in PlaybackInviteRequest) (*sip.Request, PlaybackDialogMetadata, error) {
	if u == nil || strings.TrimSpace(in.DeviceID) == "" || strings.TrimSpace(in.ChannelID) == "" ||
		strings.TrimSpace(in.Destination) == "" || strings.TrimSpace(in.SSRC) == "" || strings.TrimSpace(in.SDP) == "" {
		return nil, PlaybackDialogMetadata{}, fmt.Errorf("PLAYBACK INVITE 缺少设备、通道、目的地址、SSRC 或 SDP")
	}
	req := sip.NewRequest(sip.INVITE, u.deviceURI(in.ChannelID))
	req.SetBody([]byte(in.SDP))
	req.AppendHeader(sip.NewHeader("Subject", fmt.Sprintf("%s:%s,%s:0", in.ChannelID, in.SSRC, u.serverID)))
	req.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	req.AppendHeader(u.platformFromHeader())
	if err := u.prepareOutboundRequest(req, in.Destination, in.Transport, true); err != nil {
		return nil, PlaybackDialogMetadata{}, err
	}

	cseqText := u.nextCSeq()
	cseq, err := strconv.ParseUint(cseqText, 10, 32)
	if err != nil || cseq == 0 {
		return nil, PlaybackDialogMetadata{}, fmt.Errorf("PLAYBACK INVITE CSeq 不合法: %q", cseqText)
	}
	callID := fmt.Sprintf("playback-%s-%s-%d", u.serverID, sip.GenerateTagN(16), time.Now().UnixNano())
	callIDHeader := sip.CallIDHeader(callID)
	req.AppendHeader(&callIDHeader)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: uint32(cseq), MethodName: sip.INVITE})
	return req, PlaybackDialogMetadata{
		CallID: callID, CSeq: uint32(cseq), DeviceID: in.DeviceID,
		ChannelID: in.ChannelID, SSRC: in.SSRC,
	}, nil
}

func (u *UAC) InvitePlayback(ctx context.Context, in PlaybackInviteRequest) (PlaybackDialogMetadata, error) {
	if u == nil || u.playbackDialogs == nil || u.playbackDialogs.transport == nil {
		return PlaybackDialogMetadata{}, ErrPlaybackUnavailable
	}
	ctx, cancel := withSIPCommandTimeout(ctx)
	defer cancel()
	req, metadata, err := u.buildPlaybackInviteRequest(in)
	if err != nil {
		return metadata, err
	}
	cseq := strconv.FormatUint(uint64(metadata.CSeq), 10)
	u.recordBegin(metrics.TxInvite, metadata.CallID, cseq, metadata.DeviceID)
	dialog, err := u.playbackDialogs.transport.WriteInvite(ctx, req)
	if err != nil {
		u.recordEnd(metadata.CallID, cseq, 0, false)
		return metadata, fmt.Errorf("PLAYBACK INVITE 失败: %w", err)
	}

	answered := make(chan error, 1)
	go func() { answered <- dialog.WaitAnswer(ctx) }()
	select {
	case waitErr := <-answered:
		metadata.StatusCode = dialog.StatusCode()
		if waitErr != nil {
			_ = dialog.Close()
			u.recordEnd(metadata.CallID, cseq, metadata.StatusCode, false)
			if metadata.StatusCode >= 300 {
				return metadata, fmt.Errorf("%w: SIP %d: %v", ErrPlaybackRejected, metadata.StatusCode, waitErr)
			}
			return metadata, fmt.Errorf("PLAYBACK INVITE 应答失败: %w", waitErr)
		}
	case <-ctx.Done():
		_ = dialog.Close()
		u.recordEnd(metadata.CallID, cseq, 0, false)
		return metadata, fmt.Errorf("PLAYBACK INVITE 应答超时: %w", ctx.Err())
	}

	dialogMetadata := dialog.Metadata()
	dialogMetadata.CallID = metadata.CallID
	dialogMetadata.CSeq = metadata.CSeq
	dialogMetadata.StatusCode = dialog.StatusCode()
	dialogMetadata.DeviceID = metadata.DeviceID
	dialogMetadata.ChannelID = metadata.ChannelID
	dialogMetadata.SSRC = metadata.SSRC
	record := &playbackDialogRecord{dialog: dialog, metadata: dialogMetadata}
	// Publishing the record must not allow control/cleanup to race the ACK.
	record.mu.Lock()
	u.playbackDialogs.put(record)
	if err := dialog.Ack(ctx); err != nil {
		record.closing, record.ackPending = true, true
		record.mu.Unlock()
		u.recordEnd(metadata.CallID, cseq, dialogMetadata.StatusCode, false)
		return dialogMetadata, errors.Join(ErrPlaybackACKPending, fmt.Errorf("发送 PLAYBACK ACK 失败: %w", err))
	}
	record.mu.Unlock()
	u.recordEnd(metadata.CallID, cseq, dialogMetadata.StatusCode, true)
	return dialogMetadata, nil
}

func buildPlaybackInfoBody(request PlaybackInfoRequest, cseq uint32) ([]byte, error) {
	switch request.Action {
	case PlaybackInfoPlay:
		return mansrtsp.BuildPlay(cseq)
	case PlaybackInfoPause:
		return mansrtsp.BuildPause(cseq)
	case PlaybackInfoResume:
		return mansrtsp.BuildResume(cseq)
	case PlaybackInfoSeek:
		return mansrtsp.BuildSeek(cseq, request.Position, request.SegmentDuration)
	case PlaybackInfoScale:
		return mansrtsp.BuildScale(cseq, request.Scale)
	case PlaybackInfoTeardown:
		return mansrtsp.BuildTeardown(cseq)
	default:
		return nil, fmt.Errorf("%w: unknown playback action %q", mansrtsp.ErrInvalidArgument, request.Action)
	}
}

func (u *UAC) SendPlaybackInfo(ctx context.Context, callID string, request PlaybackInfoRequest) (PlaybackControlResult, error) {
	if u == nil || u.playbackDialogs == nil {
		return PlaybackControlResult{}, ErrPlaybackUnavailable
	}
	record := u.playbackDialogs.get(callID)
	if record == nil {
		return PlaybackControlResult{}, ErrPlaybackClosed
	}
	record.mu.Lock()
	defer record.mu.Unlock()
	if record.closed || record.closing {
		return PlaybackControlResult{}, ErrPlaybackClosed
	}

	cseq := record.metadata.CSeq + 1
	body, err := buildPlaybackInfoBody(request, cseq)
	if err != nil {
		return PlaybackControlResult{}, err
	}
	recipient := u.deviceURI(record.metadata.ChannelID)
	if target := strings.TrimSpace(record.metadata.RemoteTarget); target != "" {
		var parsedTarget sip.Uri
		if err := sip.ParseUri(strings.Trim(target, "<>"), &parsedTarget); err == nil {
			recipient = parsedTarget
		}
	}
	req := sip.NewRequest(sip.INFO, recipient)
	req.SetBody(body)
	req.AppendHeader(sip.NewHeader("Content-Type", mansrtsp.ContentType))
	callIDHeader := sip.CallIDHeader(record.metadata.CallID)
	req.AppendHeader(&callIDHeader)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: cseq, MethodName: sip.INFO})

	record.metadata.CSeq = cseq
	response, err := record.dialog.Do(ctx, req)
	if err != nil {
		return PlaybackControlResult{}, fmt.Errorf("发送 PLAYBACK INFO 失败: %w", err)
	}
	if !is2xx(response.StatusCode) {
		return PlaybackControlResult{Status: mansrtsp.ResultRejected, StatusCode: int(response.StatusCode), Reason: response.Reason, CSeq: cseq},
			fmt.Errorf("%w: SIP %d %s", ErrPlaybackRejected, response.StatusCode, response.Reason)
	}
	if len(response.Body()) == 0 {
		return PlaybackControlResult{Status: mansrtsp.ResultAccepted, StatusCode: int(response.StatusCode), Reason: response.Reason, CSeq: cseq}, nil
	}
	result, err := mansrtsp.ParseResponse(response.Body())
	if err != nil {
		return PlaybackControlResult{}, err
	}
	if result.CSeq != cseq {
		return PlaybackControlResult{}, fmt.Errorf("%w: response CSeq %d, want %d", mansrtsp.ErrProtocol, result.CSeq, cseq)
	}
	if result.Status != mansrtsp.ResultAccepted {
		return result, fmt.Errorf("%w: MANSRTSP %d %s", ErrPlaybackRejected, result.StatusCode, result.Reason)
	}
	return result, nil
}

// Action implements the narrow playback.PlaybackActioner contract without
// coupling the UAC package to the playback registry's domain types.
func (u *UAC) Action(ctx context.Context, callID, action string, positionSeconds, scale float64, segmentDuration time.Duration) (float64, float64, error) {
	request := PlaybackInfoRequest{Action: PlaybackInfoAction(action), Position: time.Duration(positionSeconds * float64(time.Second)), Scale: scale, SegmentDuration: segmentDuration}
	if action == string(PlaybackInfoSeek) {
		request.Position = time.Duration(positionSeconds * float64(time.Second))
	}
	if _, err := u.SendPlaybackInfo(ctx, callID, request); err != nil {
		return 0, 0, err
	}
	return positionSeconds, scale, nil
}

type PlaybackTeardownResult string

const (
	PlaybackTeardownMissing PlaybackTeardownResult = "missing"
	PlaybackTeardownPending PlaybackTeardownResult = "pending"
	PlaybackTeardownClosed  PlaybackTeardownResult = "sip_dialog_closed"
)

// TeardownPlayback retains the legacy missing-is-idempotent behavior. Its nil
// error is not cleanup evidence; evidence consumers must inspect the typed result.
func (u *UAC) TeardownPlayback(ctx context.Context, callID string) error {
	if u == nil || u.playbackDialogs == nil {
		return nil
	}
	_, err := u.TeardownPlaybackResult(ctx, callID)
	return err
}

// TeardownPlaybackResult describes only the original in-memory SIP dialog.
// Missing after restart or a previous deletion is never a closed receipt, and
// even sip_dialog_closed says nothing about RTP, viewers or device cleanup.
func (u *UAC) TeardownPlaybackResult(ctx context.Context, callID string) (result PlaybackTeardownResult, err error) {
	if u == nil || u.playbackDialogs == nil {
		return PlaybackTeardownMissing, ErrPlaybackUnavailable
	}
	record := u.playbackDialogs.get(callID)
	if record == nil {
		return PlaybackTeardownMissing, nil
	}
	record.mu.Lock()
	notifyEnd := false
	metadata := record.metadata
	defer func() {
		record.mu.Unlock()
		if notifyEnd {
			err = errors.Join(err, u.playbackEnded(context.Background(), metadata, "bye"))
		}
	}()
	if record.closed {
		return PlaybackTeardownClosed, nil
	}
	if record.ackPending && !record.byeConfirmed {
		// Keep the known answer for cleanup, but do not gain permission for an
		// automatic ACK retry from a legacy error path. A precise inbound BYE
		// may still close this dialog; durable retry authorization comes later.
		return PlaybackTeardownPending, ErrPlaybackCleanupUnknown
	}
	if record.inboundByeCSeq != nil && !record.byeConfirmed {
		// sipgo already set Ended before attempting the inbound response.
		// Only the same inbound exchange may resolve this uncertainty.
		return PlaybackTeardownPending, ErrPlaybackCleanupUnknown
	}

	var controlErr error
	if !record.closing {
		record.closing = true
		cseq := record.metadata.CSeq + 1
		body, buildErr := mansrtsp.BuildTeardown(cseq)
		controlErr = buildErr
		if buildErr == nil {
			record.metadata.CSeq = cseq
			controlCtx, controlCancel := context.WithTimeout(ctx, playbackTeardownTimeout)
			req := sip.NewRequest(sip.INFO, u.deviceURI(record.metadata.ChannelID))
			req.SetBody(body)
			req.AppendHeader(sip.NewHeader("Content-Type", mansrtsp.ContentType))
			callIDHeader := sip.CallIDHeader(record.metadata.CallID)
			req.AppendHeader(&callIDHeader)
			req.AppendHeader(&sip.CSeqHeader{SeqNo: cseq, MethodName: sip.INFO})
			response, err := record.dialog.Do(controlCtx, req)
			controlCancel()
			if err != nil {
				controlErr = fmt.Errorf("发送 PLAYBACK TEARDOWN 失败: %w", err)
			} else if !is2xx(response.StatusCode) {
				controlErr = fmt.Errorf("%w: SIP %d %s", ErrPlaybackRejected, response.StatusCode, response.Reason)
			} else if len(response.Body()) == 0 {
				// GB/T 28181-2016 9.8.3.2 permits a SIP 200 response without a MANSRTSP body.
			} else if result, err := mansrtsp.ParseResponse(response.Body()); err != nil {
				controlErr = err
			} else if result.CSeq != cseq {
				controlErr = fmt.Errorf("%w: response CSeq %d, want %d", mansrtsp.ErrProtocol, result.CSeq, cseq)
			} else if result.Status != mansrtsp.ResultAccepted {
				controlErr = fmt.Errorf("%w: MANSRTSP %d %s", ErrPlaybackRejected, result.StatusCode, result.Reason)
			}
		}
	}
	if !record.byeConfirmed {
		byeCtx, byeCancel := context.WithTimeout(context.WithoutCancel(ctx), playbackTeardownTimeout)
		byeErr := record.dialog.Bye(byeCtx)
		byeCancel()
		if byeErr != nil {
			return PlaybackTeardownPending, errors.Join(controlErr, byeErr)
		}
		record.byeConfirmed = true
	}
	closeErr := record.dialog.Close()
	if closeErr == nil {
		record.closed = true
		u.playbackDialogs.remove(callID, record)
		notifyEnd = record.inboundByeCSeq != nil
	}
	if errors.Is(controlErr, context.Canceled) || errors.Is(controlErr, context.DeadlineExceeded) {
		controlErr = nil
	}
	if closeErr != nil {
		return PlaybackTeardownPending, errors.Join(controlErr, closeErr)
	}
	return PlaybackTeardownClosed, controlErr
}

func (u *UAC) HandlePlaybackBye(req *sip.Request, tx sip.ServerTransaction) (bool, error) {
	if req == nil || tx == nil {
		return false, fmt.Errorf("PLAYBACK BYE 请求或事务为空")
	}
	callID := ""
	if header := req.CallID(); header != nil {
		callID = string(*header)
	}
	if u == nil || u.playbackDialogs == nil {
		err := tx.Respond(sip.NewResponseFromRequest(req, sip.StatusCallTransactionDoesNotExists, "Call/Transaction Does Not Exist", nil))
		return false, err
	}
	record := u.playbackDialogs.get(callID)
	if record == nil {
		err := tx.Respond(sip.NewResponseFromRequest(req, sip.StatusCallTransactionDoesNotExists, "Call/Transaction Does Not Exist", nil))
		return false, err
	}
	record.mu.Lock()
	fromTag, toTag := "", ""
	if from := req.From(); from != nil {
		fromTag, _ = from.Params.Get("tag")
	}
	if to := req.To(); to != nil {
		toTag, _ = to.Params.Get("tag")
	}
	cseq := req.CSeq()
	if record.closed || req.Method != sip.BYE || cseq == nil || cseq.MethodName != sip.BYE ||
		fromTag == "" || toTag == "" || fromTag != record.metadata.RemoteTag || toTag != record.metadata.LocalTag ||
		(record.inboundByeCSeq != nil && *record.inboundByeCSeq != cseq.SeqNo) {
		record.mu.Unlock()
		err := tx.Respond(sip.NewResponseFromRequest(req, sip.StatusCallTransactionDoesNotExists, "Call/Transaction Does Not Exist", nil))
		return false, err
	}
	record.closing = true
	if record.inboundByeCSeq == nil {
		seq := cseq.SeqNo
		record.inboundByeCSeq = &seq
	}
	metadata := record.metadata
	if err := record.dialog.ReadBye(req, tx); err != nil {
		record.mu.Unlock()
		return true, err
	}
	record.byeConfirmed = true
	if err := record.dialog.Close(); err != nil {
		record.mu.Unlock()
		return true, err
	}
	record.closed = true
	u.playbackDialogs.remove(callID, record)
	record.mu.Unlock()
	if err := u.playbackEnded(context.Background(), metadata, "bye"); err != nil {
		return true, err
	}
	return true, nil
}
