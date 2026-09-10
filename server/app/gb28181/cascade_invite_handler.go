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

// The SIP flow identifies the upper platform. To/Request-URI identifies its
// published channel, which is resolved only inside that platform's projection.
func matchCascadeInvitePeer(req *sip.Request, p model.GbCascadePlatform) bool {
	host, port, err := net.SplitHostPort(req.Source())
	return err == nil && req.From() != nil && req.From().Address.User == p.UpstreamServerID &&
		strings.EqualFold(host, p.Host) && port == strconv.Itoa(p.Port) && strings.EqualFold(req.Transport(), p.Transport)
}

type cascadeVideoSession struct {
	dialog       *sipgo.DialogServerSession
	platform     model.GbCascadePlatform
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
	if !matchCascadeInvitePeer(req, session.platform) {
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
	fail := func(code int, err error) {
		h.log(cascadeVideoEventFailed, 0, err)
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	platforms, err := h.store.ListPlatforms(ctx)
	if err != nil {
		fail(500, err)
		return
	}
	var platform *model.GbCascadePlatform
	for i := range platforms {
		if matchCascadeInvitePeer(req, platforms[i]) {
			if platform != nil {
				fail(403, fmt.Errorf("ambiguous upstream"))
				return
			}
			platform = &platforms[i]
		}
	}
	if platform == nil || !platform.Enabled {
		fail(403, fmt.Errorf("unknown or disabled upstream"))
		return
	}
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
	session := &cascadeVideoSession{dialog: dialog, platform: *platform, cancel: func() { sessionCancel(); tx.Terminate() }}
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

func (h *cascadeVideoRuntime) log(event string, platformID uint64, err error) {
	if app.ZapLog == nil {
		return
	}
	fields := []zap.Field{zap.Uint64("platformId", platformID)}
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
