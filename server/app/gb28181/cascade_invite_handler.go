package gb28181

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"go.uber.org/zap"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/catalog"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/control"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/media"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.cn/uvp-gb28181/app/gb28181/cascade/repository"
	gbroutes "uvplatform.cn/uvp-gb28181/app/gb28181/routes"
	"uvplatform.cn/uvp-gb28181/app/gb28181/sdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// cascadePeer 是上级平台在本次事务里实际使用的网络身份。
//
// ⚠️ 不要拿源 IP 当身份。同一台上级平台可能双网卡 / 经 NAT / 走 VPN，
// 主动发起请求时的源地址只取决于到我们这边的路由，跟它回复我们时用的源地址
// 可以不是一个（220 WVP 实测：回我们 REGISTER 用 192.168.10.220:8160，
// 发 INVITE 却用 VPN 地址 10.8.0.2:8160）。把 IP 写进匹配条件，
// 结果就是「同一台平台一半的报文认不出来」，而配置里填哪个 IP 都不对。
//
// 稳定的身份是：From 用户（上级 SIP 服务器 ID）+ 端口 + 传输层。
// 端口不能省 —— 同一台上级平台可以挂多个接入实例，端口是唯一区分依据。
type cascadePeer struct {
	host      string
	port      int
	transport string
	fromUser  string
}

func (p cascadePeer) String() string {
	if p.host == "" && p.fromUser == "" {
		return "unparsed"
	}
	return fmt.Sprintf("%s:%d/%s from=%s", p.host, p.port, p.transport, p.fromUser)
}

func cascadePeerFromRequest(req *sip.Request) (cascadePeer, bool) {
	if req == nil || req.From() == nil {
		return cascadePeer{}, false
	}
	host, portText, err := net.SplitHostPort(req.Source())
	if err != nil {
		return cascadePeer{}, false
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return cascadePeer{}, false
	}
	return cascadePeer{
		host:      host,
		port:      port,
		transport: strings.ToLower(strings.TrimSpace(req.Transport())),
		fromUser:  req.From().Address.User,
	}, true
}

// identify 按身份认定平台，不看源 IP。上游换网卡/经 NAT 时靠它兜底。
func (p cascadePeer) identify(platform model.GbCascadePlatform) bool {
	return p.fromUser == platform.UpstreamServerID &&
		p.port == platform.Port &&
		p.transport == strings.ToLower(strings.TrimSpace(platform.Transport))
}

// match 是严格口径：身份一致之外，源 IP 也要对上配置的 Host。
func (p cascadePeer) match(platform model.GbCascadePlatform) bool {
	return p.identify(platform) && strings.EqualFold(p.host, platform.Host)
}

// sameSession 用于建立后的 ACK/BYE 比对。同一个对话框的后续请求同样可能
// 换一张网卡发出来，所以只认 From + 端口 + 传输层；端口是阻止
// 「另一个接入实例拿同一个 Call-ID 拆本对话框」的那道闸。
func (p cascadePeer) sameSession(observed cascadePeer) bool {
	return p.fromUser == observed.fromUser && p.port == observed.port && p.transport == observed.transport
}

// The SIP flow identifies the upper platform. To/Request-URI identifies its
// published channel, which is resolved only inside that platform's projection.
func matchCascadeInvitePeer(req *sip.Request, p model.GbCascadePlatform) bool {
	peer, ok := cascadePeerFromRequest(req)
	return ok && peer.match(p)
}

// matchCascadeSessionPeer 判定 ACK/BYE 是否属于本会话。
func matchCascadeSessionPeer(req *sip.Request, session *cascadeVideoSession) bool {
	peer, ok := cascadePeerFromRequest(req)
	return ok && peer.sameSession(session.peer)
}

type cascadeVideoSession struct {
	dialog   *sipgo.DialogServerSession
	platform model.GbCascadePlatform
	// peer 是 INVITE 到达时看到的实际网络身份，后续 ACK/BYE 拿它比对，
	// 而不是拿可能已经过期的平台配置里的 Host。
	peer         cascadePeer
	cancel       context.CancelFunc
	acknowledged atomic.Bool
}

type cascadeVideoRuntime struct {
	client   *sipgo.Client
	store    *repository.GormRepository
	sources  *media.Provider
	sender   *media.Sender
	nodes    media.SenderNodeLookup
	mu       sync.Mutex
	sessions map[string]*cascadeVideoSession
	// ZLM's stop API addresses a sender by source + SSRC, not SIP dialog.
	senders map[string]bool
	closed  bool
	wg      sync.WaitGroup
}

const (
	cascadeVideoEventACKInvalid       = "cascade.video.ack_invalid"
	cascadeVideoEventFailed           = "cascade.video.failed"
	cascadeVideoEventRTPCleanupFailed = "cascade.video.rtp_cleanup_failed"
	cascadeVideoEventAnswerFailed     = "cascade.video.answer_failed"
	cascadeVideoEventEstablished      = "cascade.video.established"
	cascadeVideoEventStateSaveFailed  = "cascade.video.state_save_failed"
	cascadeVideoEventReleaseTimeout   = "cascade.video.release_timeout"
	cascadeVideoEventByeFailed        = "cascade.video.bye_failed"
	cascadeVideoEventPeerMismatch     = "cascade.video.peer_mismatch"
)

var cascadeVideo atomic.Pointer[cascadeVideoRuntime]

func setupCascadeVideoRuntime(server sipRuntimeServer) error {
	if cascadeVideo.Load() != nil {
		return fmt.Errorf("历史级联会话尚未清理完成")
	}
	receiver, ok := server.(interface {
		SetCascadeDialogHandler(func(*sip.Request, sip.ServerTransaction) bool)
	})
	if !ok || playSvc == nil || zlmRegistry == nil {
		return nil
	}
	client, err := server.(cascadeClientProvider).NewCascadeClient()
	if err != nil {
		return err
	}
	h := &cascadeVideoRuntime{client: client, store: repository.NewGormRepository(app.DB()), sources: media.NewProvider(media.NewPlayServiceAdapter(playSvc)), sender: media.NewSender(zlmRegistry, nil), nodes: zlmRegistry, sessions: map[string]*cascadeVideoSession{}, senders: map[string]bool{}}
	// Persisted sender identities allow cleanup after a process crash. Never
	// reconstruct a SIP dialog from stale rows or stop the shared source stream.
	if err := h.recoverSenders(); err != nil {
		return err
	}
	cascadeVideo.Store(h)
	gbroutes.SetCascadeSourceLeaseChecker(h.sources)
	receiver.SetCascadeDialogHandler(h.Handle)
	return nil
}

func (h *cascadeVideoRuntime) Handle(req *sip.Request, tx sip.ServerTransaction) bool {
	if req == nil {
		return false
	}
	if req.Method == sip.INVITE {
		// Audio INVITEs keep their established broadcast handler.
		video := false
		for _, line := range strings.Split(string(req.Body()), "\n") {
			fields := strings.Fields(line)
			if len(fields) > 0 && fields[0] == "m=video" {
				video = true
				break
			}
		}
		if !video {
			return false
		}
		h.invite(req, tx)
		return true
	}
	id, err := sip.DialogIDFromRequestUAS(req)
	if err != nil {
		return false
	}
	h.mu.Lock()
	session := h.sessions[id]
	h.mu.Unlock()
	if session == nil {
		return false
	}
	if !matchCascadeSessionPeer(req, session) {
		if req.Method != sip.ACK {
			_ = tx.Respond(sip.NewResponseFromRequest(req, 403, "Forbidden", nil))
		}
		return true
	}
	switch req.Method {
	case sip.ACK:
		if err := session.dialog.ReadAck(req, tx); err != nil {
			h.log(cascadeVideoEventACKInvalid, session.platform.ID, err)
		} else {
			session.acknowledged.Store(true)
		}
	case sip.BYE:
		if err := session.dialog.ReadBye(req, tx); err != nil {
			_ = tx.Respond(sip.NewResponseFromRequest(req, 481, "Dialog Does Not Exist", nil))
		} else {
			session.cancel()
		}
	default:
		return false
	}
	return true
}

func (h *cascadeVideoRuntime) invite(req *sip.Request, tx sip.ServerTransaction) {
	responseRequest := req
	// platformID 只在认定平台后才有值；认定之前它是 0，但报文里必须带上
	// 实际观测到的 peer —— 否则线上只留一行 {"platformId": 0}，
	// 而真正需要的信息（源地址/端口/传输层/From）一个都没有，没法排查。
	var platformID uint64
	fail := func(code int, err error) {
		peer, _ := cascadePeerFromRequest(req)
		h.log(cascadeVideoEventFailed, platformID, err, zap.String("peer", peer.String()))
		_ = tx.Respond(sip.NewResponseFromRequest(responseRequest, code, "Cascade Play Failed", nil))
	}
	if req.To() == nil || req.From() == nil || req.CallID() == nil || req.CSeq() == nil {
		fail(400, fmt.Errorf("incomplete INVITE headers"))
		return
	}
	if tag, _ := req.To().Params.Get("tag"); tag != "" {
		fail(488, fmt.Errorf("re-INVITE is not supported"))
		return
	}
	offer, err := sdp.ParseCascadeVideoOffer(req.Body())
	if err != nil {
		fail(488, err)
		return
	}
	peer, ok := cascadePeerFromRequest(req)
	if !ok {
		fail(400, fmt.Errorf("unparsable INVITE source"))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	platforms, err := h.store.ListPlatforms(ctx)
	if err != nil {
		fail(500, err)
		return
	}
	var platform *model.GbCascadePlatform
	var strictCount int
	var identityOnly []model.GbCascadePlatform
	for i := range platforms {
		switch {
		case peer.match(platforms[i]):
			strictCount++
			if platform == nil {
				platform = &platforms[i]
			}
		case peer.identify(platforms[i]):
			identityOnly = append(identityOnly, platforms[i])
		}
	}
	if strictCount > 1 {
		fail(403, fmt.Errorf("ambiguous upstream: %d platforms match %s", strictCount, peer))
		return
	}
	if strictCount == 0 {
		// 源 IP 与配置不符：上游双网卡 / 走 VPN / 经 NAT 时会有这一半的报文。
		// 退一步按身份认定，但仍然要求唯一 —— 认不出来好过认错。
		if len(identityOnly) > 1 {
			fail(403, fmt.Errorf("ambiguous upstream: %d platforms match %s", len(identityOnly), peer))
			return
		}
		if len(identityOnly) == 1 {
			platform = &identityOnly[0]
			h.log(cascadeVideoEventPeerMismatch, platform.ID, nil,
				zap.String("peer", peer.String()),
				zap.String("configuredHost", identityOnly[0].Host))
		}
	}
	if platform == nil || !platform.Enabled {
		fail(403, fmt.Errorf("unknown or disabled upstream (platforms=%d, peer=%s)", len(platforms), peer))
		return
	}
	platformID = platform.ID
	subject := req.GetHeader("Subject")
	if subject == nil {
		fail(400, fmt.Errorf("missing Subject"))
		return
	}
	parts := strings.Split(subject.Value(), ",")
	if len(parts) != 2 || strings.SplitN(parts[0], ":", 2)[0] != req.Recipient.User || strings.SplitN(parts[1], ":", 2)[0] != platform.UpstreamServerID {
		fail(403, fmt.Errorf("Subject identity mismatch"))
		return
	}
	projection, err := h.store.ProjectionSnapshot(ctx, platform.ID)
	if err != nil {
		fail(500, err)
		return
	}
	snapshot, err := catalog.Build(*projection, catalog.SourceFacts{})
	if err != nil {
		fail(500, err)
		return
	}
	published := req.Recipient.User
	target, ok := snapshot.LookupPublishedChannel(published)
	if !ok {
		fail(404, fmt.Errorf("channel is not shared to platform %d", platform.ID))
		return
	}
	source, err := control.NewGormTargetLoader(app.DB()).Load(ctx, target.SourceDeviceID, target.SourceChannelID)
	if err != nil {
		fail(404, err)
		return
	}
	if !source.DeviceOnline || !source.ChannelOnline {
		fail(480, fmt.Errorf("source channel offline"))
		return
	}
	ua := sipgo.DialogUA{Client: h.client, RewriteContact: true, ContactHDR: sip.ContactHeader{Address: sip.Uri{User: platform.LocalDeviceID, Host: platform.LocalSIPIP, Port: platform.LocalSIPPort}}}
	dialog, err := ua.ReadInvite(req, tx)
	if err != nil {
		fail(400, err)
		return
	}
	responseRequest = dialog.InviteRequest
	sessionCtx, sessionCancel := context.WithCancel(dialog.Context())
	session := &cascadeVideoSession{dialog: dialog, platform: *platform, peer: peer, cancel: func() { sessionCancel(); tx.Terminate() }}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		sessionCancel()
		fail(503, fmt.Errorf("cascade stopping"))
		return
	}
	count := 0
	for _, active := range h.sessions {
		if active.platform.ID == platform.ID {
			count++
		}
	}
	if platform.MaxStreams > 0 && count >= platform.MaxStreams {
		h.mu.Unlock()
		sessionCancel()
		fail(486, fmt.Errorf("platform stream limit reached"))
		return
	}
	h.sessions[dialog.ID] = session
	h.wg.Add(1)
	h.mu.Unlock()
	defer h.wg.Done()
	defer func() { sessionCancel(); h.mu.Lock(); delete(h.sessions, dialog.ID); h.mu.Unlock(); _ = dialog.Close() }()
	_ = dialog.Respond(100, "Trying", nil)
	provisionCtx, provisionCancel := context.WithTimeout(sessionCtx, 30*time.Second)
	defer provisionCancel()
	lease, err := h.sources.Acquire(provisionCtx, media.AcquireRequest{ConsumerKey: dialog.ID, DeviceID: source.DeviceCode, ChannelID: source.ChannelCode})
	if err != nil {
		fail(503, err)
		return
	}
	defer lease.Release(context.Background())
	key := fmt.Sprintf("%d/%s/%s/%s", lease.Source.NodeID, lease.Source.App, lease.Source.StreamID, offer.SSRC)
	h.mu.Lock()
	busy := h.senders[key]
	if !busy {
		h.senders[key] = true
	}
	h.mu.Unlock()
	if busy {
		fail(486, fmt.Errorf("sender SSRC already in use for this source"))
		return
	}
	defer func() { h.mu.Lock(); delete(h.senders, key); h.mu.Unlock() }()
	latest, err := h.store.FindPlatform(provisionCtx, platform.ID)
	if err != nil || !latest.Enabled || latest.ConfigRevision != platform.ConfigRevision || latest.ProjectionRevision != platform.ProjectionRevision {
		fail(403, fmt.Errorf("platform or sharing changed during INVITE"))
		return
	}
	now := time.Now()
	row := model.GbCascadeMediaSession{PlatformID: platform.ID, DialogKey: dialog.ID, CallID: req.CallID().Value(), CSeq: uint64(req.CSeq().SeqNo), SourceDeviceID: target.SourceDeviceID, SourceChannelID: target.SourceChannelID, PublishedChannelID: published, ZLMNodeID: lease.Source.NodeID, ZLMVHost: "__defaultVhost__", ZLMApp: lease.Source.App, ZLMStream: lease.Source.StreamID, SenderSSRC: offer.SSRC, Transport: string(offer.Transport), RemoteIP: offer.RemoteIP, RemotePort: offer.RemotePort, State: model.CascadeMediaSessionStateProvisioning, ReceivedAt: &now}
	if err = h.store.CreateMediaSession(provisionCtx, &row); err != nil {
		fail(500, err)
		return
	}
	state := row.State
	completed := false
	// Store ownership before starting: even an ambiguous HTTP timeout remains recoverable.
	sender, err := h.sender.Start(provisionCtx, media.SenderRequest{Source: lease.Source, SSRC: offer.SSRC, PayloadType: offer.PayloadType, RemoteIP: offer.RemoteIP, RemotePort: offer.RemotePort, Transport: offer.Transport})
	defer func() {
		for {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			stopErr := h.stopSender(cleanupCtx, row)
			cleanupCancel()
			if stopErr == nil {
				saved := false
				if completed || (session.acknowledged.Load() && sessionCtx.Err() != nil) {
					if state == model.CascadeMediaSessionStateActive || state == model.CascadeMediaSessionStateAnswered {
						h.transition(&row, &state, model.CascadeMediaSessionStateClosing)
					}
					saved = h.transition(&row, &state, model.CascadeMediaSessionStateClosed)
				} else {
					saved = h.transition(&row, &state, model.CascadeMediaSessionStateFailed)
				}
				if saved {
					return
				}
			} else {
				h.log(cascadeVideoEventRTPCleanupFailed, platform.ID, stopErr)
			}
			// Keep the dialog, source lease and sender claim until cleanup is certain.
			time.Sleep(5 * time.Second)
		}
	}()
	if err != nil {
		fail(503, err)
		return
	}
	mediaNode, ok := h.nodes.Get(lease.Source.NodeID)
	if !ok {
		fail(503, fmt.Errorf("media node disappeared"))
		return
	}
	mediaIP := strings.TrimSpace(platform.MediaAdvertiseIP)
	if mediaIP == "" {
		mediaIP = mediaNode.EffectiveReceiveHost()
	}
	answer, err := sdp.BuildCascadeVideoAnswer(offer, platform.LocalDeviceID, mediaIP, sender.LocalPort)
	if err != nil {
		fail(488, err)
		return
	}
	if provisionCtx.Err() != nil {
		return
	}
	if !h.transition(&row, &state, model.CascadeMediaSessionStateAnswered) {
		fail(500, fmt.Errorf("persist answer state failed"))
		return
	}
	if err = dialog.RespondSDP(answer); err != nil {
		h.log(cascadeVideoEventAnswerFailed, platform.ID, err)
		return
	}
	if !h.transition(&row, &state, model.CascadeMediaSessionStateActive) {
		return
	}
	h.log(cascadeVideoEventEstablished, platform.ID, nil)
	<-sessionCtx.Done()
	completed = true
}

// log 记录一条级联点播事件。extra 用于在固定字段之外附加这次调用特有的信息
// （例如观测到的 peer 地址），避免把结构化数据拼进消息文本里。
func (h *cascadeVideoRuntime) log(event string, platformID uint64, err error, extra ...zap.Field) {
	if app.ZapLog == nil {
		return
	}
	fields := make([]zap.Field, 0, len(extra)+2)
	fields = append(fields, extra...)
	fields = append(fields, zap.Uint64("platformId", platformID))
	if err != nil {
		fields = append(fields, zap.Error(err))
	}
	switch event {
	case cascadeVideoEventACKInvalid:
		app.ZapLog.Warn("级联点播 ACK 无效", append(fields, zap.String("event", "cascade.video.ack_invalid"))...)
	case cascadeVideoEventFailed:
		app.ZapLog.Warn("级联点播失败", append(fields, zap.String("event", "cascade.video.failed"))...)
	case cascadeVideoEventRTPCleanupFailed:
		app.ZapLog.Warn("级联 RTP 停止失败，保留占用并重试", append(fields, zap.String("event", "cascade.video.rtp_cleanup_failed"))...)
	case cascadeVideoEventAnswerFailed:
		app.ZapLog.Warn("级联点播应答或 ACK 失败", append(fields, zap.String("event", "cascade.video.answer_failed"))...)
	case cascadeVideoEventEstablished:
		app.ZapLog.Info("级联点播已建立", append(fields, zap.String("event", "cascade.video.established"))...)
	case cascadeVideoEventStateSaveFailed:
		app.ZapLog.Warn("级联点播状态保存失败", append(fields, zap.String("event", "cascade.video.state_save_failed"))...)
	case cascadeVideoEventReleaseTimeout:
		app.ZapLog.Warn("等待级联点播释放超时", append(fields, zap.String("event", "cascade.video.release_timeout"))...)
	case cascadeVideoEventByeFailed:
		app.ZapLog.Warn("级联配置更新时发送 BYE 失败", append(fields, zap.String("event", "cascade.video.bye_failed"))...)
	case cascadeVideoEventPeerMismatch:
		// 源 IP 与平台配置的 Host 不符但身份（From/端口/传输层）唯一命中：
		// 上游双网卡 / 走 VPN / 经 NAT 时时会这样，属正常放行，不是告警。
		app.ZapLog.Info("级联点播源地址与配置的 Host 不符，按 From/端口/传输层认定", append(fields, zap.String("event", "cascade.video.peer_mismatch"))...)
	}
}
func (h *cascadeVideoRuntime) transition(row *model.GbCascadeMediaSession, state *model.CascadeMediaSessionState, to model.CascadeMediaSessionState) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	changed, err := h.store.TransitionMediaSession(ctx, row.DialogKey, *state, to, time.Now())
	if err != nil || !changed {
		h.log(cascadeVideoEventStateSaveFailed, row.PlatformID, err)
		return false
	}
	*state = to
	return true
}
func (h *cascadeVideoRuntime) stopSender(ctx context.Context, row model.GbCascadeMediaSession) error {
	n, ok := h.nodes.Get(row.ZLMNodeID)
	if !ok {
		return fmt.Errorf("source media node %d unavailable", row.ZLMNodeID)
	}
	return zlm.NewClientForNode(n).StopSendRtp(ctx, row.ZLMVHost, row.ZLMApp, row.ZLMStream, row.SenderSSRC)
}
func (h *cascadeVideoRuntime) recoverSenders() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rows, err := h.store.ListNonterminalMediaSessions(ctx, 0)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.ZLMNodeID > 0 && row.ZLMStream != "" && row.SenderSSRC != "" {
			if err = h.stopSender(ctx, row); err != nil {
				return fmt.Errorf("清理历史级联发送器失败: %w", err)
			}
		}
		state := row.State
		if !h.transition(&row, &state, model.CascadeMediaSessionStateAborted) {
			return fmt.Errorf("历史级联会话状态保存失败")
		}
	}
	return nil
}
func stopCascadeVideoRuntime(ctx context.Context) {
	h := cascadeVideo.Load()
	if h == nil {
		return
	}
	h.mu.Lock()
	h.closed = true
	var sessions []*cascadeVideoSession
	for _, session := range h.sessions {
		sessions = append(sessions, session)
	}
	h.mu.Unlock()
	for _, session := range sessions {
		session.cancel()
	}
	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		h.sources.Close()
		gbroutes.SetCascadeSourceLeaseChecker(nil)
		cascadeVideo.CompareAndSwap(h, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		h.log(cascadeVideoEventReleaseTimeout, 0, ctx.Err())
	}
}

func (h *cascadeVideoRuntime) revalidate(ctx context.Context) error {
	platforms, err := h.store.ListPlatforms(ctx)
	if err != nil {
		return err
	}
	valid := map[uint64]model.GbCascadePlatform{}
	for _, p := range platforms {
		valid[p.ID] = p
	}
	h.mu.Lock()
	var obsolete []*cascadeVideoSession
	for _, session := range h.sessions {
		p, ok := valid[session.platform.ID]
		if !ok || !p.Enabled || p.ConfigRevision != session.platform.ConfigRevision || p.ProjectionRevision != session.platform.ProjectionRevision {
			obsolete = append(obsolete, session)
		}
	}
	h.mu.Unlock()
	for _, session := range obsolete {
		session.cancel()
		if session.dialog.LoadState() == sip.DialogStateConfirmed {
			byeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			err := session.dialog.Bye(byeCtx)
			cancel()
			if err != nil {
				h.log(cascadeVideoEventByeFailed, session.platform.ID, err)
			}
		}
	}
	return nil
}
