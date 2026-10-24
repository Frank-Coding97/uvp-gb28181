package gb28181

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"go.uber.org/zap"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/audio"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/sipclient"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/sdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/talk"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

const cascadeAudioDialogTimeout = 45 * time.Second
const cascadeAudioMediaInactivityTimeout = 30 * time.Second

type cascadeAudioSession struct {
	key           string
	platform      model.GbCascadePlatform
	pull          *cascadeBroadcastPull
	peer          cascadePeer
	uas           cascadeAudioServerDialog
	uac           sipclient.AudioDialog
	bridge        *audio.Bridge
	ctx           context.Context
	cancel        context.CancelFunc
	done          chan struct{}
	endedBy       string
	businessEnd   bool
	ssrc          string
	deviceACK     chan struct{}
	deviceACKOnce sync.Once
}

type cascadeAudioServerDialog interface {
	DialogID() string
	Context() context.Context
	Respond(int, string, []byte) error
	RespondSDP([]byte) error
	LoadState() sip.DialogState
	ReadAck(*sip.Request, sip.ServerTransaction) error
	ReadBye(*sip.Request, sip.ServerTransaction) error
	Bye(context.Context) error
	Close() error
}

type sipgoCascadeAudioServerDialog struct{ session *sipgo.DialogServerSession }

func (d sipgoCascadeAudioServerDialog) DialogID() string         { return d.session.ID }
func (d sipgoCascadeAudioServerDialog) Context() context.Context { return d.session.Context() }
func (d sipgoCascadeAudioServerDialog) Respond(status int, reason string, body []byte) error {
	return d.session.Respond(status, reason, body)
}
func (d sipgoCascadeAudioServerDialog) RespondSDP(body []byte) error {
	return d.session.RespondSDP(body)
}
func (d sipgoCascadeAudioServerDialog) LoadState() sip.DialogState { return d.session.LoadState() }
func (d sipgoCascadeAudioServerDialog) ReadAck(req *sip.Request, tx sip.ServerTransaction) error {
	return d.session.ReadAck(req, tx)
}
func (d sipgoCascadeAudioServerDialog) ReadBye(req *sip.Request, tx sip.ServerTransaction) error {
	return d.session.ReadBye(req, tx)
}
func (d sipgoCascadeAudioServerDialog) Bye(ctx context.Context) error { return d.session.Bye(ctx) }
func (d sipgoCascadeAudioServerDialog) Close() error                  { return d.session.Close() }

type cascadeAudioServerDialogFactory func(*sip.Request, sip.ServerTransaction, model.GbCascadePlatform) (cascadeAudioServerDialog, error)

var cascadeAudioSSRC atomic.Uint32

func nextCascadeAudioSSRC() string {
	n := cascadeAudioSSRC.Add(1)%999999999 + 1
	return fmt.Sprintf("0%09d", n)
}

func (m *cascadeBroadcastRuntime) HandleCascadeDialog(req *sip.Request, tx sip.ServerTransaction) bool {
	if req == nil || tx == nil || m == nil || (m.dialogClient == nil && m.audioDialogFactory == nil) {
		return false
	}
	if req.Method == sip.INVITE {
		return m.handleCascadeAudioInvite(req, tx)
	}
	if req.Method != sip.ACK && req.Method != sip.BYE {
		return false
	}
	return m.handleCascadeAudioInDialog(req, tx)
}

func (m *cascadeBroadcastRuntime) handleCascadeAudioInvite(req *sip.Request, tx sip.ServerTransaction) bool {
	if !strings.Contains(string(req.Body()), "m=audio") {
		return false
	}
	if m.store == nil || m.db == nil || m.clients == nil {
		_ = tx.Respond(sip.NewResponseFromRequest(req, 503, "Cascade Audio Unavailable", nil))
		return true
	}
	ctx, release, admitted := m.acquire(context.Background(), 0)
	if !admitted {
		_ = tx.Respond(sip.NewResponseFromRequest(req, 503, "Cascade Audio Unavailable", nil))
		return true
	}
	defer release()
	match, err := resolveCascadeBroadcastPull(ctx, m.db, m.store, req, time.Now())
	if match == nil && err == nil {
		return false
	}
	reject := func(code int, reason string) bool {
		_ = tx.Respond(sip.NewResponseFromRequest(req, code, reason, nil))
		return true
	}
	if err != nil {
		return reject(488, "Invalid Cascade Audio Offer")
	}
	if _, ok := m.clients.transport.(sipclient.AudioDialogTransport); !ok {
		return reject(503, "Cascade Audio Dialog Unavailable")
	}
	if err := match.Claim(ctx, m.db, req, time.Now()); err != nil {
		return reject(486, "Cascade Audio Reservation Changed")
	}
	if err := m.registerCascadeAudioSession(ctx, req, tx, match); err != nil {
		return reject(503, "Cascade Audio Dialog Unavailable")
	}
	return true
}

func (m *cascadeBroadcastRuntime) registerCascadeAudioSession(runtimeCtx context.Context, req *sip.Request, tx sip.ServerTransaction, match *cascadeBroadcastPull) error {
	platform := match.Authorization.Platform
	dialog, err := m.newCascadeAudioServerDialog(req, tx, platform)
	if err != nil {
		return err
	}
	sessionCtx, cancel := context.WithCancel(dialog.Context())
	stopRuntime := context.AfterFunc(runtimeCtx, cancel)
	session := &cascadeAudioSession{key: match.Row.DialogKey, platform: platform, pull: match, peer: match.Peer, uas: dialog, ctx: sessionCtx, cancel: cancel, done: make(chan struct{}), ssrc: match.Offer.SSRC, deviceACK: make(chan struct{})}
	if session.ssrc == "" {
		session.ssrc = nextCascadeAudioSSRC()
		match.Offer.SSRC = session.ssrc
	}
	m.audioMu.Lock()
	for _, current := range m.audioSessions {
		if current.key == session.key {
			m.audioMu.Unlock()
			stopRuntime()
			cancel()
			return fmt.Errorf("cascade broadcast already has an audio dialog")
		}
	}
	m.audioSessions[dialog.DialogID()] = session
	m.audioMu.Unlock()
	_ = dialog.Respond(100, "Trying", nil)
	defer func() {
		stopRuntime()
		cancel()
		m.audioMu.Lock()
		delete(m.audioSessions, dialog.DialogID())
		m.audioMu.Unlock()
		_ = dialog.Close()
		close(session.done)
	}()
	if err := m.establishCascadeAudio(session); err != nil {
		if dialog.LoadState() < sip.DialogStateEstablished {
			_ = dialog.Respond(488, "Cascade Audio Negotiation Failed", nil)
		}
		m.cleanupCascadeAudio(session, err.Error())
		return nil
	}
	m.waitCascadeAudio(session)
	m.cleanupCascadeAudio(session, "cascade audio dialog ended")
	return nil
}

func (m *cascadeBroadcastRuntime) newCascadeAudioServerDialog(req *sip.Request, tx sip.ServerTransaction, platform model.GbCascadePlatform) (cascadeAudioServerDialog, error) {
	if m.audioDialogFactory != nil {
		return m.audioDialogFactory(req, tx, platform)
	}
	if m.dialogClient == nil {
		return nil, fmt.Errorf("cascade audio server dialog client is unavailable")
	}
	contact := sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: platform.LocalDeviceID, Host: platform.LocalSIPIP, Port: platform.LocalSIPPort}}
	ua := sipgo.DialogUA{Client: m.dialogClient, ContactHDR: contact}
	dialog, err := ua.ReadInvite(req, tx)
	if err != nil {
		return nil, err
	}
	return sipgoCascadeAudioServerDialog{session: dialog}, nil
}

func (m *cascadeBroadcastRuntime) establishCascadeAudio(session *cascadeAudioSession) error {
	setupCtx, setupCancel := context.WithTimeout(session.ctx, cascadeAudioDialogTimeout)
	defer setupCancel()
	if err := session.pull.Authorization.Validate(setupCtx, m.db, m.store); err != nil {
		return err
	}
	bindIP := strings.TrimSpace(session.platform.LocalSIPIP)
	advertiseIP := strings.TrimSpace(session.platform.MediaAdvertiseIP)
	if net.ParseIP(advertiseIP) == nil || net.ParseIP(advertiseIP).To4() == nil || net.ParseIP(advertiseIP).IsUnspecified() {
		advertiseIP = bindIP
	}
	localSetup := ""
	if session.pull.Offer.Transport == "TCP/RTP/AVP" {
		localSetup = "active"
	}
	leaseRepo := talk.NewGormRepo(m.db)
	now := time.Now()
	reserved, err := leaseRepo.ActivateCascadeLease(setupCtx, session.pull.Binding.AudioLeaseID, now, now.Add(cascadeAudioDialogTimeout+15*time.Second))
	if err != nil || !reserved {
		return fmt.Errorf("activate cascade audio lease: active=%t err=%w", reserved, err)
	}
	bridge, err := audio.NewBridge(session.ctx, bindIP, session.pull.Offer.Transport, localSetup, session.pull.Offer)
	if err != nil {
		return err
	}
	session.bridge = bridge
	sourceSSRC := nextCascadeAudioSSRC()
	sourceSDP, err := sdp.BuildCascadeAudioPullOffer(session.platform.LocalDeviceID, advertiseIP, bridge.SourcePort(), session.pull.Offer.Transport, localSetup, sourceSSRC, bridge.SourceRTCPPort())
	if err != nil {
		return err
	}
	client, err := m.clients.NewAudioClient(session.platform)
	if err != nil {
		return err
	}
	sourceOffer, err := sdp.ParseCascadeAudioOffer(sourceSDP)
	if err != nil {
		return err
	}
	dialog, err := client.WriteInvite(setupCtx, session.pull.Binding.UpstreamSourceID, session.pull.Authorization.PublishedID, sourceSDP)
	if err != nil {
		return err
	}
	session.uac = dialog
	if err := dialog.WaitAnswer(setupCtx); err != nil {
		return err
	}
	answer, err := sdp.ValidateCascadeAudioAnswer(sourceOffer, dialog.AnswerSDP())
	if err != nil {
		return err
	}
	if err := bridge.SetSource(answer); err != nil {
		return err
	}
	if err := session.pull.Authorization.Validate(setupCtx, m.db, m.store); err != nil {
		return err
	}
	if err := session.ctx.Err(); err != nil {
		return err
	}
	deviceAnswer, err := sdp.BuildCascadeAudioAnswer(session.pull.Offer, session.platform.LocalDeviceID, advertiseIP, bridge.ReceiverPort(), bridge.ReceiverRTCPPort())
	if err != nil {
		return err
	}
	if err := session.uas.RespondSDP(deviceAnswer); err != nil {
		return err
	}
	if err := waitCascadeAudioDeviceACK(setupCtx, session); err != nil {
		return err
	}
	if err := dialog.Ack(setupCtx); err != nil {
		return err
	}
	if err := session.pull.Authorization.Validate(setupCtx, m.db, m.store); err != nil {
		return err
	}
	if err := session.ctx.Err(); err != nil {
		return err
	}
	if err := bridge.Activate(); err != nil {
		return err
	}
	if renewed, err := leaseRepo.RenewCascadeLease(setupCtx, session.pull.Binding.AudioLeaseID, now, now.Add(cascadeBroadcastPullTimeout)); err != nil || !renewed {
		return fmt.Errorf("renew active cascade audio lease: active=%t err=%w", renewed, err)
	}
	return m.transitionBroadcastActive(setupCtx, session.key, now)
}

func (m *cascadeBroadcastRuntime) transitionBroadcastActive(ctx context.Context, key string, at time.Time) error {
	row, err := m.store.FindMediaSessionByDialog(ctx, key)
	if err != nil || row == nil {
		return fmt.Errorf("cascade audio media row unavailable: %w", err)
	}
	if row.State == model.CascadeMediaSessionStateProvisioning {
		if changed, err := m.store.TransitionMediaSession(ctx, key, row.State, model.CascadeMediaSessionStateAnswered, at); err != nil || !changed {
			return fmt.Errorf("save cascade audio answer state: changed=%t err=%w", changed, err)
		}
		row.State = model.CascadeMediaSessionStateAnswered
	}
	if row.State == model.CascadeMediaSessionStateAnswered {
		changed, err := m.store.TransitionMediaSession(ctx, key, row.State, model.CascadeMediaSessionStateActive, at)
		if err != nil || !changed {
			return fmt.Errorf("save cascade audio active state: changed=%t err=%w", changed, err)
		}
	}
	return nil
}

func (m *cascadeBroadcastRuntime) handleCascadeAudioInDialog(req *sip.Request, tx sip.ServerTransaction) bool {
	if req.CallID() == nil {
		return false
	}
	session, side := m.findCascadeAudioDialog(req)
	if session == nil {
		return false
	}
	peer, ok := cascadePeerFromRequest(req)
	peerValid := ok && session.peer.sameSession(peer)
	if side == "upstream" && ok {
		platforms, err := m.store.ListPlatforms(context.Background())
		platform, ambiguous := findCascadeControlPeer(req, platforms)
		peerValid = err == nil && !ambiguous && platform != nil && platform.ID == session.platform.ID && platform.Enabled
	}
	if !peerValid {
		if req.Method != sip.ACK {
			_ = tx.Respond(sip.NewResponseFromRequest(req, 403, "Forbidden", nil))
		}
		return true
	}
	if side == "device" {
		switch req.Method {
		case sip.ACK:
			if err := session.uas.ReadAck(req, tx); err != nil {
				app.Log(context.Background()).Warn("级联广播设备 ACK 无效", zap.String("event", "cascade.broadcast.audio_ack_invalid"), zap.Error(err))
			} else if session.deviceACK != nil {
				session.deviceACKOnce.Do(func() { close(session.deviceACK) })
			}
		case sip.BYE:
			if err := session.uas.ReadBye(req, tx); err != nil {
				_ = tx.Respond(sip.NewResponseFromRequest(req, 481, "Dialog Does Not Exist", nil))
			} else {
				session.endedBy = "device"
				session.cancel()
			}
		}
	} else if req.Method == sip.BYE {
		if err := session.uac.ReadBye(req, tx); err != nil {
			_ = tx.Respond(sip.NewResponseFromRequest(req, 481, "Dialog Does Not Exist", nil))
		} else {
			session.endedBy = "upstream"
			session.cancel()
		}
	}
	return true
}

func waitCascadeAudioDeviceACK(ctx context.Context, session *cascadeAudioSession) error {
	if session == nil || session.deviceACK == nil {
		return fmt.Errorf("cascade audio device ACK signal is unavailable")
	}
	select {
	case <-session.deviceACK:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *cascadeBroadcastRuntime) findCascadeAudioDialog(req *sip.Request) (*cascadeAudioSession, string) {
	m.audioMu.Lock()
	defer m.audioMu.Unlock()
	for id, session := range m.audioSessions {
		if id == "" || session.uas == nil {
			continue
		}
		if dialogID, err := sip.DialogIDFromRequestUAS(req); err == nil && dialogID == id {
			return session, "device"
		}
		if session.uac != nil && session.uac.MatchesRequest(req) {
			return session, "upstream"
		}
	}
	return nil, ""
}

func (m *cascadeBroadcastRuntime) stopCascadeAudioForBusinessEnd(key string) error {
	m.audioMu.Lock()
	var session *cascadeAudioSession
	for _, candidate := range m.audioSessions {
		if candidate.key == key {
			session = candidate
			candidate.businessEnd = true
			candidate.cancel()
			break
		}
	}
	m.audioMu.Unlock()
	if session == nil {
		return nil
	}
	timer := time.NewTimer(6 * time.Second)
	defer timer.Stop()
	select {
	case <-session.done:
		return nil
	case <-timer.C:
		return fmt.Errorf("cascade audio cleanup timed out before broadcast lease release")
	}
}

func (m *cascadeBroadcastRuntime) ValidateCascadeAudioCancel(req *sip.Request, tx sip.ServerTransaction) bool {
	origin, ok := tx.(interface{ Origin() *sip.Request })
	if !ok || origin.Origin() == nil || !strings.Contains(string(origin.Origin().Body()), "m=audio") {
		return true
	}
	match, err := resolveCascadeBroadcastPull(context.Background(), m.db, m.store, origin.Origin(), time.Now())
	if err != nil || match == nil {
		return err == nil
	}
	originPeer, originOK := cascadePeerFromRequest(origin.Origin())
	cancelPeer, cancelOK := cascadePeerFromRequest(req)
	return originOK && cancelOK && originPeer.sameSession(cancelPeer)
}

func (m *cascadeBroadcastRuntime) waitCascadeAudio(session *cascadeAudioSession) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-session.ctx.Done():
			return
		case <-session.bridge.Done():
			return
		case now := <-ticker.C:
			lastRTPAt := session.bridge.LastRTPAt()
			if cascadeAudioMediaInactive(lastRTPAt, now) {
				app.Log(context.Background()).Warn("级联广播音频媒体流中断", zap.String("event", "cascade.broadcast.audio_media_inactive"), zap.Duration("inactive_for", now.Sub(lastRTPAt)))
				session.cancel()
				return
			}
			ctx, cancel := context.WithTimeout(session.ctx, 4*time.Second)
			err := session.pull.Authorization.Validate(ctx, m.db, m.store)
			if err == nil {
				var renewed bool
				renewed, err = talk.NewGormRepo(m.db).RenewCascadeLease(ctx, session.pull.Binding.AudioLeaseID, now, now.Add(cascadeBroadcastPullTimeout))
				if err == nil && !renewed {
					err = fmt.Errorf("cascade audio lease was lost")
				}
			}
			cancel()
			if err != nil {
				app.Log(context.Background()).Warn("级联广播音频授权复验失败", zap.String("event", "cascade.broadcast.audio_authorization_lost"), zap.Error(err))
				session.cancel()
				return
			}
		}
	}
}

func cascadeAudioMediaInactive(lastRTPAt, now time.Time) bool {
	return !lastRTPAt.IsZero() && now.Sub(lastRTPAt) >= cascadeAudioMediaInactivityTimeout
}

func (m *cascadeBroadcastRuntime) cleanupCascadeAudio(session *cascadeAudioSession, reason string) {
	if session.bridge != nil {
		session.bridge.Close()
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if shouldCascadeAudioSendBye(session.endedBy, "upstream") && session.uac != nil && session.uac.AnswerSDP() != nil {
		if err := session.uac.Bye(cleanupCtx); err != nil {
			app.Log(cleanupCtx).Warn("级联广播上级 BYE 失败", zap.String("event", "cascade.broadcast.audio_upstream_bye_failed"), zap.Error(err))
		}
	}
	if shouldCascadeAudioSendBye(session.endedBy, "device") && session.uas != nil && session.uas.LoadState() == sip.DialogStateConfirmed {
		if err := session.uas.Bye(cleanupCtx); err != nil {
			app.Log(cleanupCtx).Warn("级联广播设备 BYE 失败", zap.String("event", "cascade.broadcast.audio_device_bye_failed"), zap.Error(err))
		}
	}
	if session.uac != nil {
		if err := session.uac.Close(); err != nil {
			app.Log(cleanupCtx).Warn("级联广播上级 Dialog 关闭失败", zap.String("event", "cascade.broadcast.audio_upstream_dialog_close_failed"), zap.Error(err))
		}
	}
	if session.uas != nil {
		_ = session.uas.Close()
	}
	m.audioMu.Lock()
	businessEnd := session.businessEnd
	m.audioMu.Unlock()
	if businessEnd {
		return
	}
	if err := m.updateBroadcast(cleanupCtx, session.key, 0, model.CascadeMediaSessionStateClosed, reason, time.Now()); err != nil {
		app.Log(cleanupCtx).Error("级联广播音频释放失败", zap.String("event", "cascade.broadcast.audio_cleanup_failed"), zap.Error(err))
	}
	_, _ = talk.NewGormRepo(m.db).ReleaseCascadeLease(cleanupCtx, session.pull.Binding.AudioLeaseID, gbmodels.TalkSessionEnded, reason, time.Now())
}

func shouldCascadeAudioSendBye(endedBy, side string) bool {
	return endedBy == "" || endedBy != side
}
