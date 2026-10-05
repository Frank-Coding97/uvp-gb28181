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
	// ViaHost/ViaPort/Transport 记录 INVITE 实际写入的 Via sent-by。
	//
	// dialog 内的后续请求（INFO/BYE）必须复用同一个 sent-by：sipgo 只在请求
	// 没有 Via 时才用 Client.host 生成，而 Client.host 为空时会被 transport 层
	// 填成本地监听套接字地址（[::]:5062）。RFC 3261 §18.2.1 规定 UAS 把应答发往
	// sent-by，那个地址不可路由 ⇒ INFO/BYE 永远收不到应答，拆除只能靠重传超时。
	// INVITE 在 prepareOutboundRequest 里显式写了地址，ACK 由 sipgo 克隆 INVITE
	// 的 Via，所以只有 INFO/BYE 需要在这里补齐。
	ViaHost   string
	ViaPort   int
	Transport string
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
	mu       sync.Mutex
	dialog   playbackDialog
	metadata PlaybackDialogMetadata
	closed   bool
	closing  bool
	// teardownConfirmed 记录设备是否已用 2xx 确认过 MANSRTSP TEARDOWN。
	// 它才是"会话已被拆掉"的确认点;byeConfirmed 只说明 BYE 这一跳拿到了应答。
	teardownConfirmed bool
	byeConfirmed      bool
	ackPending        bool
	inboundByeCSeq    *uint32
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
	viaHost, viaPort := "", 0
	if via := req.Via(); via != nil {
		viaHost, viaPort = via.Host, via.Port
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
		ViaHost: viaHost, ViaPort: viaPort, Transport: normalizeTransport(in.Transport),
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
	// sent-by 只来自 INVITE 的构造过程：dialog.Metadata() 读的是应答，
	// 拿不到这个信息，漏掉就会被后续 INFO/BYE 当成"没有 Via"重新生成。
	dialogMetadata.ViaHost = metadata.ViaHost
	dialogMetadata.ViaPort = metadata.ViaPort
	dialogMetadata.Transport = metadata.Transport
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

// playbackControlRecipient 解析回放控制类请求的目标地址。
//
// dialog 的远端目标是设备在 INVITE 应答里给出的 Contact(逐跳可达地址),
// 而 deviceURI 用的是平台自身域(公网地址),两者并不相同。所有在既有 dialog
// 内发送的请求都必须优先采用 Contact,否则报文会被发往平台自己而不是设备。
func (u *UAC) playbackControlRecipient(metadata PlaybackDialogMetadata) sip.Uri {
	recipient := u.deviceURI(metadata.ChannelID)
	if target := strings.TrimSpace(metadata.RemoteTarget); target != "" {
		var parsedTarget sip.Uri
		if err := sip.ParseUri(strings.Trim(target, "<>"), &parsedTarget); err == nil {
			recipient = parsedTarget
		}
	}
	return recipient
}

// bindPlaybackVia 给 dialog 内的后续请求补上可路由的 Via sent-by。
//
// sipgo 只有在请求完全没有 Via 时才生成一个，而生成时用的是 Client.host；
// 该字段为空时 transport 层会用本地监听套接字兜底，也就是 [::]:5062 这类
// 未指定地址。按 RFC 3261 §18.2.1，UAS 会把应答发往 Via 的 sent-by，于是
// 这样的 INFO/BYE 永远收不到应答，只能靠 UDP 重传超时结束（实测该设备
// 2890 条出向 BYE 零应答，拖动时间轴后的拆除固定耗时 4 秒并被 HTTP 层判超时）。
//
// 因此这里显式复用 INVITE 的 sent-by。metadata 缺这些字段时（历史会话）退回
// 按目的地址做一次路由解析，绝不留下空 host 让 transport 层兜底。
func (u *UAC) bindPlaybackVia(req *sip.Request, metadata PlaybackDialogMetadata) {
	if req == nil || req.Via() != nil {
		return
	}
	host := strings.TrimSpace(metadata.ViaHost)
	if host == "" {
		localIP, err := u.outboundIP(req.Destination())
		if err != nil {
			return
		}
		host = localIP
	}
	port := metadata.ViaPort
	if port <= 0 {
		port = u.sipPort
	}
	transport := strings.TrimSpace(metadata.Transport)
	if transport == "" {
		transport = req.Transport()
	}
	params := sip.NewParams()
	params.Add("branch", sip.GenerateBranchN(16))
	req.AppendHeader(&sip.ViaHeader{
		ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: normalizeTransport(transport),
		Host: host, Port: port, Params: params,
	})
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
	req := sip.NewRequest(sip.INFO, u.playbackControlRecipient(record.metadata))
	u.bindPlaybackVia(req, record.metadata)
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
	// PlaybackTeardownClosedByeUnconfirmed 表示设备已用 2xx 确认 MANSRTSP
	// TEARDOWN、本地 SIP 对话也已关闭,只是 BYE 没拿到应答(有一类设备恒不回
	// BYE)。平台侧已收敛,因此不是错误;但也不等于"完全干净"。
	PlaybackTeardownClosedByeUnconfirmed PlaybackTeardownResult = "sip_dialog_closed_bye_unconfirmed"
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
	// controlAccepted 表示设备已经用 2xx(+可接受的 MANSRTSP 应答)确认了这次拆除。
	// 未确认时必须保持 fail-closed(保留绑定、下次重试);已确认时 BYE 只是收尾动作。
	controlAccepted := false
	if !record.closing {
		record.closing = true
		cseq := record.metadata.CSeq + 1
		body, buildErr := mansrtsp.BuildTeardown(cseq)
		controlErr = buildErr
		if buildErr == nil {
			record.metadata.CSeq = cseq
			controlCtx, controlCancel := context.WithTimeout(ctx, playbackTeardownTimeout)
			// 必须与暂停/拖动走同一套目标解析:漏掉 Contact 覆盖会让这条
			// TEARDOWN INFO 被发往平台自身域(公网地址),设备永远收不到。
			req := sip.NewRequest(sip.INFO, u.playbackControlRecipient(record.metadata))
			u.bindPlaybackVia(req, record.metadata)
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
				controlAccepted = true
			} else if result, err := mansrtsp.ParseResponse(response.Body()); err != nil {
				controlErr = err
			} else if result.CSeq != cseq {
				controlErr = fmt.Errorf("%w: response CSeq %d, want %d", mansrtsp.ErrProtocol, result.CSeq, cseq)
			} else if result.Status != mansrtsp.ResultAccepted {
				controlErr = fmt.Errorf("%w: MANSRTSP %d %s", ErrPlaybackRejected, result.StatusCode, result.Reason)
			} else {
				controlAccepted = true
			}
		}
	}
	if controlAccepted {
		record.teardownConfirmed = true
	}
	if !record.byeConfirmed {
		byeCtx, byeCancel := context.WithTimeout(context.WithoutCancel(ctx), playbackTeardownTimeout)
		byeErr := record.dialog.Bye(byeCtx)
		byeCancel()
		if byeErr != nil {
			if !record.teardownConfirmed {
				return PlaybackTeardownPending, errors.Join(controlErr, byeErr)
			}
			// 设备已经确认了 MANSRTSP TEARDOWN(会话确实被拆掉),唯独没回 BYE。
			// 有一类设备恒不回 BYE —— 实测大华 DH-3H3405-ADG:全历史出向
			// 2908 条 BYE、应答 0 条(同一台设备的直播 dialog 是回 BYE 的)。
			// 而 GB/T 28181-2016 里"会话结束"的确认点本来就是 RTSP TEARDOWN
			// 的 2xx,BYE 只负责收 SIP 对话:它没被应答不构成"会话仍活着"的
			// 证据。若据此判失败,拆除永远不收敛 —— 每次停止/切录像都要耗满
			// 重试上限(退避 1/2/4/8/16s ≈ 30s)才释放通道,这段窗口内任何
			// 再操作都固定 429「当前通道已有回放会话」。
			//
			// 注意这仍然是 fail-closed:没拿到任何确认(INFO 失败/被拒)时
			// 走上面那个分支,保留绑定、保留可重试性。
			//
			// 可观测性:这类设备在 gb_sip_trace_message 里表现为长期存在的
			// method='BYE' AND direction='outbound' AND status_code=0。
		} else {
			record.byeConfirmed = true
		}
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
	if !record.byeConfirmed {
		// 平台侧已收敛(设备确认了 TEARDOWN、本地对话已关闭),但 BYE 没拿到应答。
		// 用独立返回值把"降级成功"与"完全干净"区分开,不升级成错误。
		return PlaybackTeardownClosedByeUnconfirmed, controlErr
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
