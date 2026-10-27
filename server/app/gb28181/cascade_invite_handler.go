package gb28181

import (
	"context"
	"encoding/json"
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
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/catalog"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/control"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/media"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/mansrtsp"
	"uvplatform.com/uvp-gb28181/app/gb28181/play"
	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
	gbroutes "uvplatform.com/uvp-gb28181/app/gb28181/routes"
	"uvplatform.com/uvp-gb28181/app/gb28181/sdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

// cascadePeer binds the configured upstream identity to the observed SIP endpoint.
type cascadePeer struct {
	host      string
	port      int
	transport string
	fromUser  string
}

// cascadePeerDNS centralizes host resolution so every inbound cascade entry
// point applies the same source-address policy. Tests replace lookup with a
// deterministic resolver; production uses the system resolver and performs a
// fresh lookup for each new transaction, so an address change takes effect on
// the next request without weakening an established dialog's peer binding.
var cascadePeerDNS = struct {
	sync.RWMutex
	lookup func(string) ([]net.IP, error)
}{lookup: net.LookupIP}

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

// identify checks identity fields; match additionally requires a trusted source IP.
func (p cascadePeer) identify(platform model.GbCascadePlatform) bool {
	return p.fromUser == platform.UpstreamServerID &&
		p.port == platform.Port &&
		p.transport == strings.ToLower(strings.TrimSpace(platform.Transport))
}

// match 是严格口径：身份一致之外，源 IP 也要对上配置的 Host。
func (p cascadePeer) match(platform model.GbCascadePlatform) bool {
	return p.identify(platform) && p.trustedSource(platform)
}

// trustedSource 决定「源 IP 是否可信」。两条互补的通道，任一成立即可：
//
//	① 来源地址等于配置里的 Host（含域名 DNS 解析）——最严格的原始口径；
//	② 来源地址等于**该平台用自己的凭据成功注册时被记录下来的那个地址**。
//
// 为什么必须有②：GB28181 的 Host 是「人工填的接入地址」，而报文实际从
// 哪里来由对端的网络环境决定。下级平台部署在同机 docker 里（出站源 IP 是
// 172.x 容器网段）、或位于 NAT 之后时，配置的 Host 与真实源 IP 天然对不上，
// 于是上级发来的 SUBSCRIBE / MESSAGE 会被一律 403 —— 表现为「级联目录订阅
// 永远失败」，而注册、心跳、点播却全都正常，非常难反推。
//
// 为什么②依然安全：这条通道只认「同一份凭据已经通过 Digest 鉴权的那台机器」。
// 注册处理器在鉴权通过后调用 security.TrustEndpoint 把实测地址记进安全运行时
// （IP + 传输 + 有效期），由注册有效期约束、过期自动失效；一个陌生 IP 拿不到
// 这条记录，也就进不来。因此它不改变「陌生源地址必须被拒」这一契约，只是多认
// 一个「已验证过的地址」。
//
// ⚠️ 该记录在内存里，平台自身重启后会被清空：此时上级必须先重新 REGISTER 一次
// 才会恢复，期间它的控制类消息仍会被 403。实测 WVP 在我方重启后 1 分钟内就会
// 自动重注册，所以窗口很短；若将来遇到「重启后订阅一直失败」，先看最近一条
// event=cascade.subscription.rejected 的 from/peer，再确认上级是否重新注册过。
func (p cascadePeer) trustedSource(platform model.GbCascadePlatform) bool {
	if cascadePeerHostMatches(p.host, platform.Host) {
		return true
	}
	return cascadePeerTrustedMatches(p.fromUser, p.host)
}

// cascadePeerTrusted resolves the last address that authenticated with this
// platform's own credentials. Tests replace the lookup so the matcher stays
// hermetic (same pattern as cascadePeerDNS).
var cascadePeerTrusted = struct {
	sync.RWMutex
	lookup func(deviceID string) (string, bool)
}{lookup: registeredCascadePeerAddress}

func registeredCascadePeerAddress(deviceID string) (string, bool) {
	if strings.TrimSpace(deviceID) == "" {
		return "", false
	}
	runtime := SecurityRuntime()
	if runtime == nil {
		return "", false
	}
	endpoint, ok := runtime.TrustedEndpoint(deviceID)
	if !ok || strings.TrimSpace(endpoint.Address) == "" {
		return "", false
	}
	return endpoint.Address, true
}

func cascadePeerTrustedMatches(deviceID, observed string) bool {
	cascadePeerTrusted.RLock()
	lookup := cascadePeerTrusted.lookup
	cascadePeerTrusted.RUnlock()
	if lookup == nil {
		return false
	}
	trusted, ok := lookup(deviceID)
	if !ok {
		return false
	}
	return cascadePeerHostMatches(observed, trusted)
}

func cascadePeerHostMatches(observed, configured string) bool {
	observed = normalizeCascadeHost(observed)
	configured = normalizeCascadeHost(configured)
	if observed == "" || configured == "" {
		return false
	}
	observedIP := net.ParseIP(observed)
	if observedIP == nil {
		// A SIP source address is expected to be a numeric IP. Do not let a
		// hostname in a synthetic or malformed source bypass DNS verification.
		return false
	}
	if configuredIP := net.ParseIP(configured); configuredIP != nil {
		return configuredIP.Equal(observedIP)
	}
	cascadePeerDNS.RLock()
	lookup := cascadePeerDNS.lookup
	cascadePeerDNS.RUnlock()
	if lookup == nil {
		return false
	}
	addresses, err := lookup(configured)
	if err != nil || len(addresses) == 0 {
		return false
	}
	for _, address := range addresses {
		if address != nil && address.Equal(observedIP) {
			return true
		}
	}
	return false
}

func normalizeCascadeHost(host string) string {
	host = strings.TrimSpace(host)
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	if zone := strings.LastIndexByte(host, '%'); zone >= 0 {
		host = host[:zone]
	}
	return host
}

// sameSession 是事务级严格口径：来源地址、端口、传输层和 From 身份全部一致。
// 同一 INVITE 事务内的 CANCEL 必须用它（RFC 3261 要求 CANCEL 与 INVITE 同端点）。
func (p cascadePeer) sameSession(observed cascadePeer) bool {
	return p.fromUser == observed.fromUser &&
		strings.EqualFold(p.host, observed.host) &&
		p.port == observed.port &&
		p.transport == observed.transport
}

// sameEstablishedPeer 用于会话建立后的 ACK/BYE/INFO 比对。
//
// 身份维度：来源 IP + 传输层 + From 平台编码，再加 Call-ID/tags（调用方已用它
// 定位到本会话，别的 Call-ID 根本进不到这里）。
//
// UDP 上端口**不作为身份**：UDP 无连接，上级的报文随时可能换源端口 —— NAT
// 重绑定、多出口线路、上级重启后重新 bind，最常见的是上级把 dialog 内请求
// （INFO/BYE）交给另一个临时 socket 发出去。实测现场就是这样：62 的 INVITE 从
// 51064 到达，停播 BYE 和回放控制 INFO 却从 47972 发出，而且 Via sent-by 也跟着
// 写成 47972 —— 所以「拿 Via 兜底」也区分不出来。按端口比对的结果是下级把
// 停止和回放控制全部回 403，上级停不掉流、暂停和拖动全失效，只能等超时清理。
//
// TCP 仍要求端口一致：可靠连接本身就是会话的一部分，另一实例必然是另一条连接。
func (p cascadePeer) sameEstablishedPeer(observed cascadePeer) bool {
	if p.fromUser != observed.fromUser ||
		!strings.EqualFold(p.host, observed.host) ||
		p.transport != observed.transport {
		return false
	}
	if p.port == observed.port {
		return true
	}
	return observed.transport == "udp"
}

// The SIP flow identifies the upper platform. To/Request-URI identifies its
// published channel, which is resolved only inside that platform's projection.
func matchCascadeInvitePeer(req *sip.Request, p model.GbCascadePlatform) bool {
	peer, ok := cascadePeerFromRequest(req)
	return ok && peer.match(p)
}

type cascadeVideoSession struct {
	dialog   *sipgo.DialogServerSession
	platform model.GbCascadePlatform
	// peer 是 INVITE 到达时看到的实际网络身份，后续 ACK/BYE 拿它比对，
	// 而不是拿可能已经过期的平台配置里的 Host。
	peer cascadePeer
	// peerMu 保护 peer：会话建立后上级可能从新端口发 BYE/INFO，每收到一条
	// 都会把新端口写回，所以读侧（快照比对）必须与写侧互斥。
	peerMu            sync.Mutex
	source            cascadeMediaSourceIdentity
	inviteKey         cascadeInviteKey
	ssrc              string
	senderMu          sync.Mutex
	stopCurrentSender func(context.Context) error
	sender            *media.SenderLease
	done              chan struct{}
	context           context.Context
	transport         zlm.GBSendRTPTransport
	rtpInfo           *mansrtsp.RTPInfo
	cancel            context.CancelFunc
	acknowledged      atomic.Bool
	playback          *cascadePlaybackLease
	controlMu         sync.Mutex
}

// peerSnapshot 返回当前会话绑定的来源身份副本。
func (s *cascadeVideoSession) peerSnapshot() cascadePeer {
	if s == nil {
		return cascadePeer{}
	}
	s.peerMu.Lock()
	defer s.peerMu.Unlock()
	return s.peer
}

// adoptPeerPort 在比对通过后把上级的新源端口写回会话，返回旧端口用于记日志。
// 只允许改端口：来源 IP 与 From 身份已由 sameEstablishedPeer 卡住。
func (s *cascadeVideoSession) adoptPeerPort(peer cascadePeer) (previous cascadePeer) {
	s.peerMu.Lock()
	defer s.peerMu.Unlock()
	previous = s.peer
	if peer.port != 0 {
		s.peer.port = peer.port
	}
	return previous
}

type cascadeVideoRuntime struct {
	client             *sipgo.Client
	store              *repository.GormRepository
	sources            *media.Provider
	sender             *media.Sender
	nodes              media.SenderNodeLookup
	playback           cascadePlaybackService
	recordings         cascadeRecordingQuerier
	recordingLocation  *time.Location
	recordingLookback  time.Duration
	monitorInterval    time.Duration
	downloadByeTimeout time.Duration
	mu                 sync.Mutex
	sessions           map[string]*cascadeVideoSession
	sourcePending      map[cascadeSourceKey]cascadeInviteKey
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
	h := &cascadeVideoRuntime{client: client, store: repository.NewGormRepository(app.DB()), sources: media.NewProvider(media.NewPlayServiceAdapter(playSvc)), sender: media.NewSender(zlmRegistry, nil), nodes: zlmRegistry, playback: playbackService, sessions: map[string]*cascadeVideoSession{}, senders: map[string]bool{}}
	if recordQueryService != nil {
		h.recordings = recordQueryService
		h.recordingLocation = recordQueryService.Location()
	}
	// Persisted sender identities allow cleanup after a process crash. Never
	// reconstruct a SIP dialog from stale rows or stop the shared source stream.
	if err := h.recoverSenders(); err != nil {
		return err
	}
	cascadeVideo.Store(h)
	gbroutes.SetCascadeSourceLeaseChecker(h.sources)
	receiver.SetCascadeDialogHandler(h.Handle)
	if cancelReceiver, ok := server.(interface {
		SetCascadeCancelValidator(func(*sip.Request, sip.ServerTransaction) bool)
	}); ok {
		cancelReceiver.SetCascadeCancelValidator(h.ValidateCancel)
	}
	return nil
}

func (h *cascadeVideoRuntime) ValidateCancel(req *sip.Request, tx sip.ServerTransaction) bool {
	originProvider, ok := tx.(interface{ Origin() *sip.Request })
	if h == nil || req == nil || tx == nil || !ok || originProvider.Origin() == nil || !cascadeRequestHasVideo(originProvider.Origin()) {
		return true
	}
	originPeer, ok := cascadePeerFromRequest(originProvider.Origin())
	if !ok {
		return false
	}
	// A pending video transaction stays bound to its original transport peer,
	// independently of later DNS, database or platform configuration changes.
	cancelPeer, ok := cascadePeerFromRequest(req)
	return ok && cancelPeer.sameSession(originPeer)
}

func cascadeRequestHasVideo(req *sip.Request) bool {
	if req == nil {
		return false
	}
	for _, line := range strings.Split(string(req.Body()), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "m=video" {
			return true
		}
	}
	return false
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
	peer, peerOK := cascadePeerFromRequest(req)
	if !peerOK || !peer.sameEstablishedPeer(session.peerSnapshot()) {
		if req.Method != sip.ACK {
			// 上级换了源 IP 或 From 身份（上平台重启后重建、配置被改）时，后续 BYE/INFO 会落到这里。
			// 不记日志的话，现象是「上级停不掉/控不了」而我方日志一片安静。
			if app.ZapLog != nil {
				app.ZapLog.Warn("级联点播会话对端不匹配,拒绝请求",
					zap.String("event", "cascade.video.rejected"),
					zap.String("method", req.Method.String()),
					zap.Uint64("platform_id", session.platform.ID),
					zap.String("call_id", cascadeCallID(req)),
					zap.String("peer", cascadePeerLabel(req)),
					zap.String("session_peer", session.peerSnapshot().String()))
			}
			_ = tx.Respond(sip.NewResponseFromRequest(req, 403, "Forbidden", nil))
		}
		return true
	}
	if previous := session.adoptPeerPort(peer); previous.port != peer.port {
		// 端口漂移是正常现象（NAT 重绑定、多出口、上级重启后重新 bind），放行但留痕：
		// 以后若再出现「上级停不掉流」，这条 INFO 就是第一现场。
		if app.ZapLog != nil {
			app.ZapLog.Info("级联点播会话源端口漂移,已跟随更新",
				zap.String("event", "cascade.video.peer_port_drift"),
				zap.String("method", req.Method.String()),
				zap.Uint64("platform_id", session.platform.ID),
				zap.String("call_id", cascadeCallID(req)),
				zap.String("peer", cascadePeerLabel(req)),
				zap.Int("previous_port", previous.port),
				zap.Int("current_port", peer.port))
		}
	}
	switch req.Method {
	case sip.ACK:
		if err := session.dialog.ReadAck(req, tx); err != nil {
			h.logACKInvalid(session.platform.ID, session.dialog.ID, err)
		} else {
			session.acknowledged.Store(true)
		}
	case sip.BYE:
		if err := session.dialog.ReadBye(req, tx); err != nil {
			_ = tx.Respond(sip.NewResponseFromRequest(req, 481, "Dialog Does Not Exist", nil))
		} else {
			session.cancel()
		}
	case sip.INFO:
		h.playbackInfo(req, tx, session)
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
		h.logFailed(platformID, cascadeCallID(req), err, peer.String())
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
	for i := range platforms {
		if peer.match(platforms[i]) {
			strictCount++
			if platform == nil {
				platform = &platforms[i]
			}
		}
	}
	if strictCount > 1 {
		fail(403, fmt.Errorf("ambiguous upstream: %d platforms match %s", strictCount, peer))
		return
	}
	if platform == nil || !platform.Enabled {
		fail(403, fmt.Errorf("unknown or disabled upstream (platforms=%d, peer=%s)", len(platforms), peer))
		return
	}
	platformID = platform.ID
	if (offer.Playback || offer.Download) && !platform.RecordingEnabled {
		fail(403, fmt.Errorf("recording sharing is disabled"))
		return
	}
	subject := req.GetHeader("Subject")
	if subject == nil {
		fail(400, fmt.Errorf("missing Subject"))
		return
	}
	identity, subjectErr := parseCascadeMediaSourceIdentity(subject.Value(), offer.Playback || offer.Download)
	if subjectErr != nil {
		fail(400, subjectErr)
		return
	}
	if identity.SenderID != req.Recipient.User || identity.ReceiverID != platform.UpstreamServerID {
		fail(403, fmt.Errorf("subject identity mismatch"))
		return
	}
	if offer.RecordingURI != "" && (offer.Playback || offer.Download) {
		uri, err := sdp.ParseHistoricalURI(offer.RecordingURI)
		if err != nil || uri.DeviceID != identity.SenderID {
			fail(403, fmt.Errorf("recording URI target differs from published channel"))
			return
		}
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
	var deviceEpoch int64
	if !offer.Playback && !offer.Download {
		security, err := playauth.NewDeviceSecurityStore(app.DB()).Load(ctx, source.DeviceCode)
		if err != nil || security.CleanupCompletedEpoch != security.AccessEpoch {
			fail(503, play.ErrPlayAuthorizationUnavailable)
			return
		}
		deviceEpoch = security.AccessEpoch
	}
	releaseReservation, duplicate, err := h.reserveSource(ctx, platform.ID, identity, offer.SSRC, req)
	if err != nil {
		fail(503, err)
		return
	}
	if duplicate {
		// The owning SIP transaction retransmits its cached final response.
		_ = tx.Respond(sip.NewResponseFromRequest(req, 100, "Trying", nil))
		return
	}
	defer func() {
		if releaseReservation != nil {
			releaseReservation()
		}
	}()
	ua := sipgo.DialogUA{Client: h.client, RewriteContact: true, ContactHDR: sip.ContactHeader{Address: sip.Uri{User: platform.LocalDeviceID, Host: platform.LocalSIPIP, Port: platform.LocalSIPPort}}}
	dialog, err := ua.ReadInvite(req, tx)
	if err != nil {
		fail(400, err)
		return
	}
	responseRequest = dialog.InviteRequest
	sessionCtx, sessionCancel := context.WithCancel(dialog.Context())
	session := &cascadeVideoSession{dialog: dialog, platform: *platform, peer: peer, source: identity, inviteKey: cascadeInviteTransactionKey(req), ssrc: offer.SSRC, done: make(chan struct{}), context: sessionCtx, transport: offer.Transport, cancel: func() { sessionCancel(); tx.Terminate() }}
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
	defer func() {
		sessionCancel()
		h.mu.Lock()
		delete(h.sessions, dialog.ID)
		h.mu.Unlock()
		_ = dialog.Close()
		close(session.done)
	}()
	_ = dialog.Respond(100, "Trying", nil)
	provisionCtx, provisionCancel := context.WithTimeout(sessionCtx, 30*time.Second)
	defer provisionCancel()
	var sourceMedia media.StartResult
	var releaseSource func()
	if offer.Playback || offer.Download {
		sourceMedia, releaseSource, err = h.acquirePlayback(provisionCtx, session, target, &offer)
	} else {
		var lease *media.Lease
		lease, err = h.sources.Acquire(provisionCtx, media.AcquireRequest{ConsumerKey: dialog.ID, DeviceID: source.DeviceCode, ChannelID: source.ChannelCode, DeviceEpoch: deviceEpoch})
		if err == nil {
			sourceMedia = lease.Source
			releaseSource = func() {
				if releaseErr := lease.Release(context.Background()); releaseErr != nil && app.ZapLog != nil {
					app.ZapLog.Warn("级联媒体 source 释放失败", zap.String("event", "cascade.source_release_failed"), zap.String("dialog_id", dialog.ID), zap.Error(releaseErr))
				}
			}
		}
	}
	if err != nil {
		fail(503, err)
		return
	}
	defer releaseSource()
	latestProjection, err := h.store.ProjectionSnapshot(provisionCtx, platform.ID)
	if err != nil {
		fail(500, err)
		return
	}
	latestSnapshot, err := catalog.Build(*latestProjection, catalog.SourceFacts{})
	if err != nil {
		fail(500, err)
		return
	}
	latestTarget, ok := latestSnapshot.LookupPublishedChannel(published)
	if !ok || latestTarget.SourceDeviceID != target.SourceDeviceID || latestTarget.SourceChannelID != target.SourceChannelID {
		fail(403, fmt.Errorf("channel sharing changed during INVITE"))
		return
	}
	mediaNode, ok := h.nodes.Get(sourceMedia.NodeID)
	if !ok {
		fail(503, fmt.Errorf("media node disappeared"))
		return
	}
	mediaIP := strings.TrimSpace(platform.MediaAdvertiseIP)
	if mediaIP == "" {
		mediaIP = mediaNode.EffectiveSDPIP()
	}
	if mediaIP == "" {
		// 无可用 SDP 地址时宁可明确失败,也不下发 c=IN IP4 127.0.0.1 让上级
		// 把 RTP 推向平台自己(表征是级联通了但没有画面)。
		fail(503, fmt.Errorf("no usable SDP address for media node %d", mediaNode.ID))
		return
	}
	key := fmt.Sprintf("%d/%s/%s/%s", sourceMedia.NodeID, sourceMedia.App, sourceMedia.StreamID, offer.SSRC)
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
	if err != nil || !latest.Enabled || ((offer.Playback || offer.Download) && !latest.RecordingEnabled) || latest.ConfigRevision != platform.ConfigRevision || latest.ProjectionRevision != platform.ProjectionRevision {
		fail(403, fmt.Errorf("platform or sharing changed during INVITE"))
		return
	}
	now := time.Now()
	identitySummary, _ := json.Marshal(identity)
	row := model.GbCascadeMediaSession{PlatformID: platform.ID, DialogKey: dialog.ID, CallID: req.CallID().Value(), CSeq: uint64(req.CSeq().SeqNo), SourceDeviceID: target.SourceDeviceID, SourceChannelID: target.SourceChannelID, PublishedChannelID: published, ZLMNodeID: sourceMedia.NodeID, ZLMVHost: "__defaultVhost__", ZLMApp: sourceMedia.App, ZLMStream: sourceMedia.StreamID, SenderSSRC: offer.SSRC, SDPSummary: string(identitySummary), Transport: string(offer.Transport), RemoteIP: offer.RemoteIP, RemotePort: offer.RemotePort, State: model.CascadeMediaSessionStateProvisioning, ReceivedAt: &now}
	if err = h.store.CreateMediaSession(provisionCtx, &row); err != nil {
		fail(500, err)
		return
	}
	state := row.State
	completed := false
	byeSent := false
	// Store ownership before starting: even an ambiguous HTTP timeout remains recoverable.
	request := media.SenderRequest{Source: sourceMedia, SSRC: offer.SSRC, RTPInfoURL: cascadeRTPInfoURL(mediaIP, sourceMedia.App, sourceMedia.StreamID), PayloadType: offer.PayloadType, RemoteIP: offer.RemoteIP, RemotePort: offer.RemotePort, Transport: offer.Transport, RTCPKeepalive: platform.RTCPKeepalive}
	var sender *media.SenderLease
	senderStopped := false
	stopRow := row
	session.senderMu.Lock()
	h.mu.Lock()
	session.stopCurrentSender = func(stopCtx context.Context) error {
		session.senderMu.Lock()
		defer session.senderMu.Unlock()
		if senderStopped {
			return nil
		}
		var stopErr error
		if session.playback != nil {
			stopErr = session.playback.stopSender(stopCtx)
		} else {
			stopErr = h.stopSender(stopCtx, stopRow)
		}
		if stopErr == nil {
			senderStopped = true
		}
		return stopErr
	}
	h.mu.Unlock()
	if session.playback != nil {
		sender, err = session.playback.startSender(provisionCtx, row, request)
	} else {
		sender, err = h.sender.Start(provisionCtx, request)
	}
	session.senderMu.Unlock()
	defer func() {
		for {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			stopErr := session.stopCurrentSender(cleanupCtx)
			cleanupCancel()
			if stopErr == nil {
				if !byeSent && session.dialog.LoadState() == sip.DialogStateConfirmed {
					byeCtx, byeCancel := context.WithTimeout(context.Background(), 3*time.Second)
					byeErr := session.dialog.Bye(byeCtx)
					byeCancel()
					byeSent = true
					if byeErr != nil {
						h.logByeFailed(platform.ID, session.dialog.ID, byeErr)
					}
				}
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
				h.logRTPCleanupFailed(platform.ID, row.DialogKey, stopErr)
			}
			// Keep the dialog, source lease and sender claim until cleanup is certain.
			time.Sleep(5 * time.Second)
		}
	}()
	if err != nil {
		fail(503, err)
		return
	}
	session.sender = sender
	if sender.RTPInfo != nil && strings.TrimSpace(sender.RTPInfo.URL) != "" {
		session.rtpInfo = &mansrtsp.RTPInfo{URL: sender.RTPInfo.URL, Seq: sender.RTPInfo.Seq, RTPTime: sender.RTPInfo.RTPTime}
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
	releaseReservation()
	releaseReservation = nil
	if err = dialog.RespondSDP(answer); err != nil {
		h.logAnswerFailed(platform.ID, dialog.ID, err)
		return
	}
	if !session.acknowledged.Load() || sessionCtx.Err() != nil {
		return
	}
	if !h.transition(&row, &state, model.CascadeMediaSessionStateActive) {
		return
	}
	h.logEstablished(platform.ID, dialog.ID)
	h.waitCascadeVideo(sessionCtx, session, sender)
	completed = true
}

// 级联点播的日志出口：**一个事件一个方法，字段在方法体里写全**。
//
// ⚠️ 不要改回「攒一个 fields []zap.Field、再 append(..., zap.String("event", ...))... 展开」
// 的写法。那种写法运行时输出是对的，但**字段静态不可见**——门禁
// （internal/loggingcontract）报 unresolved_logger，scan-logging.py 把它算作
// "经 zap.Field 变量展开"。于是「这条日志带了定位字段」这个结论**无法被验证**，
// 而一个红着的门禁等于没有门禁。拆成具名方法后，每条日志带什么是一眼可读的。
//
// 字段约定（与 play 链路一致，全部 snake_case）：
//
//	platform_id —— 配置里的上级平台主键；**认定平台之前没有值**，此时字段缺席。
//	call_id     —— 本次 SIP 事务的 Call-ID。级联是纯 SIP 链路，没有 HTTP
//	               request id 可继承，能把它和 `gb_sip_trace_message` 对上的只有它。
//	peer        —— 实际观测到的对端身份（源地址:端口/传输层 + From 用户）。
func (h *cascadeVideoRuntime) logACKInvalid(platformID uint64, callID string, err error) {
	if app.ZapLog == nil {
		return
	}
	// INFO 而非 WARN：ACK 无效是**上级平台的错**（按 RFC3261 必须回带同一 Call-ID 的 ACK）。
	// 本函数只记录，调用方继续跑（不 return）—— 没有我方对象可修，"去看/去联系上级平台"
	// 也不是一条能落到具体对象的动作。真出问题时下游会自己报：
	// `cascade.video.release_timeout` / `cascade.video.answer_failed` 仍是 WARN。
	app.ZapLog.Info("级联点播 ACK 无效",
		zap.String("event", cascadeVideoEventACKInvalid),
		zap.Uint64("platform_id", platformID),
		zap.String("call_id", callID),
		zap.Error(err))
}

// logFailed 记录 INVITE 在处理过程中被拒。
//
// 失败分两段：**认定平台之前**（报文非法 / SDP 解析失败 / 认不出上游）与**之后**
// （通道没共享、设备离线、并发超限）。前一段 platformID 还没值，此时
// `platform_id` **字段缺席**而不是打 0 —— 打 0 会被扫描脚本和人都当成"已带定位字段"，
// 正是那种"指标绿着、排障瞎着"的假阳性。这种时候身份由 peer 承担。
func (h *cascadeVideoRuntime) logFailed(platformID uint64, callID string, err error, peer string) {
	if app.ZapLog == nil {
		return
	}
	if platformID == 0 {
		app.ZapLog.Warn("级联点播失败",
			zap.String("event", cascadeVideoEventFailed),
			zap.String("call_id", callID),
			zap.String("peer", peer),
			zap.Error(err))
		return
	}
	app.ZapLog.Warn("级联点播失败",
		zap.String("event", cascadeVideoEventFailed),
		zap.Uint64("platform_id", platformID),
		zap.String("call_id", callID),
		zap.String("peer", peer),
		zap.Error(err))
}

func (h *cascadeVideoRuntime) logRTPCleanupFailed(platformID uint64, callID string, err error) {
	if app.ZapLog == nil {
		return
	}
	app.ZapLog.Warn("级联 RTP 停止失败，保留占用并重试",
		zap.String("event", cascadeVideoEventRTPCleanupFailed),
		zap.Uint64("platform_id", platformID),
		zap.String("call_id", callID),
		zap.Error(err))
}

func (h *cascadeVideoRuntime) logAnswerFailed(platformID uint64, callID string, err error) {
	if app.ZapLog == nil {
		return
	}
	app.ZapLog.Warn("级联点播应答或 ACK 失败",
		zap.String("event", cascadeVideoEventAnswerFailed),
		zap.Uint64("platform_id", platformID),
		zap.String("call_id", callID),
		zap.Error(err))
}

func (h *cascadeVideoRuntime) logEstablished(platformID uint64, callID string) {
	if app.ZapLog == nil {
		return
	}
	app.ZapLog.Info("级联点播已建立",
		zap.String("event", cascadeVideoEventEstablished),
		zap.Uint64("platform_id", platformID),
		zap.String("call_id", callID))
}

func (h *cascadeVideoRuntime) logStateSaveFailed(platformID uint64, callID string, err error) {
	if app.ZapLog == nil {
		return
	}
	app.ZapLog.Warn("级联点播状态保存失败",
		zap.String("event", cascadeVideoEventStateSaveFailed),
		zap.Uint64("platform_id", platformID),
		zap.String("call_id", callID),
		zap.Error(err))
}

// logReleaseTimeout 是**进程级**事件：关闭时等不到所有级联会话释放，
// 定位对象是"本次进程关闭"而不是某台上级平台或某次会话，因此不带 platform_id / call_id。
func (h *cascadeVideoRuntime) logReleaseTimeout(err error) {
	if app.ZapLog == nil {
		return
	}
	app.ZapLog.Warn("等待级联点播释放超时",
		zap.String("event", cascadeVideoEventReleaseTimeout),
		zap.Error(err))
}

func (h *cascadeVideoRuntime) logByeFailed(platformID uint64, callID string, err error) {
	if app.ZapLog == nil {
		return
	}
	app.ZapLog.Warn("级联配置更新时发送 BYE 失败",
		zap.String("event", cascadeVideoEventByeFailed),
		zap.Uint64("platform_id", platformID),
		zap.String("call_id", callID),
		zap.Error(err))
}

// logPeerMismatch 记录「源 IP 与平台配置的 Host 不符但身份（From/端口/传输层）唯一命中」。
// 上游双网卡 / 走 VPN / 经 NAT 时会有这一半报文，属正常放行，不是告警 ——
// 但它解释了「为什么按源 IP 查不到这台平台」，所以两个地址都要留下。
func (h *cascadeVideoRuntime) logPeerMismatch(platformID uint64, callID, peer, configuredHost string) {
	if app.ZapLog == nil {
		return
	}
	app.ZapLog.Info("级联点播源地址与配置的 Host 不符，按 From/端口/传输层认定",
		zap.String("event", cascadeVideoEventPeerMismatch),
		zap.Uint64("platform_id", platformID),
		zap.String("call_id", callID),
		zap.String("peer", peer),
		zap.String("configured_host", configuredHost))
}

func (h *cascadeVideoRuntime) transition(row *model.GbCascadeMediaSession, state *model.CascadeMediaSessionState, to model.CascadeMediaSessionState) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	changed, err := h.store.TransitionMediaSession(ctx, row.DialogKey, *state, to, time.Now())
	if err != nil || !changed {
		h.logStateSaveFailed(row.PlatformID, row.DialogKey, err)
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
	rows, err := h.store.ListNonterminalVideoSessions(ctx)
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
		h.logReleaseTimeout(ctx.Err())
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

	}
	return nil
}
