package talk

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync/atomic"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/sdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
)

var broadcastSN atomic.Uint32

type BroadcastInvite struct {
	PeerID   string
	TargetID string
	CallID   string
	CSeq     uint
	SDP      string
}

type PreparedBroadcastInvite struct {
	SessionID string
	AnswerSDP string
}

type BroadcastSIPError struct {
	Status int
	Reason string
}

func (e *BroadcastSIPError) Error() string  { return e.Reason }
func (e *BroadcastSIPError) SIPStatus() int { return e.Status }

type broadcastMediaClient interface {
	TalkMediaClient
	StartBroadcastSendRtp(context.Context, zlm.BroadcastSendRtpRequest) (*zlm.StartBroadcastSendRtpResult, error)
}

func nextBroadcastSN() uint {
	return uint(broadcastSN.Add(1)%999999 + 1)
}

func (s *Service) onBroadcastPublished(ctx context.Context, session *models.GbTalkSession) error {
	if !s.activation.begin(session.SessionID) {
		return nil
	}
	defer s.activation.end(session.SessionID)
	mediaNode, ok := s.nodes.Get(session.NodeID)
	if !ok || mediaNode == nil || !mediaNode.IsActive() {
		return s.failActivation(ctx, session, nil, "", false, ErrTalkNodeUnavailable)
	}
	channel, device, err := s.activation.deps.Targets.LoadTalkTarget(ctx, session.ChannelID, session.DeviceID)
	if err != nil || channel == nil || device == nil || channel.Status != models.ChannelStatusOnline || device.Status != models.DeviceStatusOnline {
		if err == nil {
			err = ErrTalkTargetOffline
		}
		return s.failActivation(ctx, session, nil, "", false, err)
	}
	client := s.activation.deps.ClientFor(mediaNode)
	if client == nil {
		return s.failActivation(ctx, session, nil, "", false, ErrTalkActivationUnavailable)
	}
	info, err := client.GetMediaInfo(ctx, "rtsp", defaultTalkVHost, session.App, session.SourceStream)
	if err != nil || info == nil || !info.Online {
		if err == nil {
			err = ErrTalkSourceUnavailable
		}
		return s.failActivation(ctx, session, client, "", false, err)
	}
	if !hasReadyPCMA(info.Tracks) {
		return s.failActivation(ctx, session, client, "", false, ErrTalkCodecUnsupported)
	}
	sn := nextBroadcastSN()
	phase := models.TalkSignalPhaseNotifying
	_, _ = s.repo.UpdateBroadcastFacts(ctx, session.SessionID, BroadcastFactsPatch{SN: &sn, SignalPhase: &phase})
	body, err := manscdp.BuildBroadcastNotify(manscdp.BroadcastNotify{SN: int(sn), SourceID: s.activation.deps.Platform.ServerID, TargetID: channel.ChannelID})
	if err != nil {
		return s.failActivation(ctx, session, client, "", false, err)
	}
	destination := net.JoinHostPort(device.IP, strconv.Itoa(device.Port))
	if _, err = s.activation.deps.BroadcastSender.SendMessageTracked(ctx, device.DeviceID, destination, device.Transport, body); err != nil {
		return s.failActivation(ctx, session, client, "", false, err)
	}
	changed, err := s.repo.Transition(ctx, session.SessionID, models.TalkSessionPublishing, models.TalkSessionInviting, TransitionPatch{})
	if err != nil {
		return s.failActivation(ctx, session, client, "", false, err)
	}
	if !changed {
		current, findErr := s.repo.FindBySession(ctx, session.SessionID)
		if findErr != nil || current == nil || current.State != models.TalkSessionInviting {
			return findErr
		}
	}
	phase = models.TalkSignalPhaseWaitingInvite
	_, err = s.repo.UpdateBroadcastFacts(ctx, session.SessionID, BroadcastFactsPatch{SignalPhase: &phase})
	return err
}

func (s *Service) OnBroadcastMessage(ctx context.Context, senderDeviceCode string, body []byte) error {
	response, err := manscdp.ParseBroadcastResponse(body)
	if err != nil {
		return err
	}
	peer := strings.TrimSpace(senderDeviceCode)
	if peer == "" {
		peer = response.DeviceID
	}
	session, err := s.repo.FindByBroadcastSN(ctx, uint(response.SN), peer, response.TargetID)
	if err != nil || session == nil {
		return err
	}
	status := "failed"
	if response.Success() {
		status = "success"
	}
	_, err = s.repo.UpdateBroadcastFacts(ctx, session.SessionID, BroadcastFactsPatch{ReplyStatus: &status})
	return err
}

func (s *Service) PrepareBroadcastInvite(ctx context.Context, invite BroadcastInvite) (PreparedBroadcastInvite, error) {
	if s == nil || s.activation == nil || s.activation.deps.ClientFor == nil {
		return PreparedBroadcastInvite{}, ErrTalkActivationUnavailable
	}
	candidates, err := s.repo.FindPendingBroadcast(ctx, strings.TrimSpace(invite.PeerID), strings.TrimSpace(invite.TargetID))
	if err != nil {
		return PreparedBroadcastInvite{}, err
	}
	if len(candidates) == 0 && strings.TrimSpace(invite.TargetID) != "" {
		candidates, err = s.repo.FindPendingBroadcast(ctx, "", strings.TrimSpace(invite.TargetID))
		if err != nil {
			return PreparedBroadcastInvite{}, err
		}
	}
	if len(candidates) == 0 {
		return PreparedBroadcastInvite{}, &BroadcastSIPError{Status: 403, Reason: "没有匹配的 Broadcast 会话"}
	}
	if len(candidates) != 1 {
		return PreparedBroadcastInvite{}, &BroadcastSIPError{Status: 486, Reason: "存在多个 Broadcast 候选会话"}
	}
	session := &candidates[0]
	media, err := sdp.ParseBroadcastOffer(invite.SDP)
	if err != nil {
		return PreparedBroadcastInvite{}, &BroadcastSIPError{Status: 488, Reason: err.Error()}
	}
	mediaNode, ok := s.nodes.Get(session.NodeID)
	if !ok || mediaNode == nil || !mediaNode.IsActive() {
		return PreparedBroadcastInvite{}, ErrTalkNodeUnavailable
	}
	client := s.activation.deps.ClientFor(mediaNode)
	broadcastClient, ok := client.(broadcastMediaClient)
	if !ok || broadcastClient == nil {
		return PreparedBroadcastInvite{}, ErrTalkActivationUnavailable
	}
	info, err := client.GetMediaInfo(ctx, "rtsp", defaultTalkVHost, session.App, session.SourceStream)
	if err != nil || info == nil || !info.Online || !hasReadyPCMA(info.Tracks) {
		return PreparedBroadcastInvite{}, ErrTalkSourceUnavailable
	}
	localPort := 0
	if media.SenderMode == sdp.BroadcastSenderTCPPassive {
		result, startErr := client.StartSendRtpPassive(ctx, zlm.TalkSendRtpRequest{VHost: defaultTalkVHost, App: session.App, SourceStream: session.SourceStream, RecvStreamID: session.RecvStream, SSRC: session.SSRC, CloseDelayMS: defaultTalkCloseDelay})
		if startErr != nil {
			return PreparedBroadcastInvite{}, startErr
		}
		localPort = result.LocalPort
	} else {
		result, startErr := broadcastClient.StartBroadcastSendRtp(ctx, zlm.BroadcastSendRtpRequest{VHost: defaultTalkVHost, App: session.App, SourceStream: session.SourceStream, SSRC: session.SSRC, RemoteIP: media.RemoteIP, RemotePort: media.RemotePort, IsUDP: media.SenderMode == sdp.BroadcastSenderUDP})
		if startErr != nil {
			return PreparedBroadcastInvite{}, startErr
		}
		localPort = result.LocalPort
	}
	if session.State == models.TalkSessionPublishing {
		changed, transitionErr := s.repo.Transition(ctx, session.SessionID, models.TalkSessionPublishing, models.TalkSessionInviting, TransitionPatch{})
		if transitionErr != nil || !changed {
			_ = client.StopSendRtp(ctx, defaultTalkVHost, session.App, session.SourceStream, session.SSRC)
			return PreparedBroadcastInvite{}, transitionErr
		}
	}
	phase := models.TalkSignalPhaseWaitingAck
	transport := strings.ToLower(media.Transport)
	senderMode := string(media.SenderMode)
	callID := strings.TrimSpace(invite.CallID)
	changed, err := s.repo.UpdateBroadcastFacts(ctx, session.SessionID, BroadcastFactsPatch{SignalPhase: &phase, RemoteMediaIP: &media.RemoteIP, RemotePort: &media.RemotePort, Transport: &transport, SenderMode: &senderMode, CallID: &callID, CSeq: &invite.CSeq})
	if err != nil || !changed {
		_ = client.StopSendRtp(ctx, defaultTalkVHost, session.App, session.SourceStream, session.SSRC)
		return PreparedBroadcastInvite{}, err
	}
	answer, err := sdp.BuildBroadcastAnswer(sdp.BroadcastAnswerParams{ServerID: s.activation.deps.Platform.ServerID, LocalIP: mediaNode.Host, LocalPort: localPort, SSRC: session.SSRC, SenderMode: media.SenderMode})
	if err != nil {
		_ = client.StopSendRtp(ctx, defaultTalkVHost, session.App, session.SourceStream, session.SSRC)
		return PreparedBroadcastInvite{}, err
	}
	return PreparedBroadcastInvite{SessionID: session.SessionID, AnswerSDP: answer}, nil
}

func (s *Service) OnBroadcastAck(ctx context.Context, callID string) error {
	session, err := s.repo.FindByCallID(ctx, callID)
	if err != nil || session == nil || session.Mode != models.TalkSessionModeBroadcast || session.State.IsTerminal() {
		return err
	}
	if session.State == models.TalkSessionActive {
		return nil
	}
	now := s.now().UTC()
	expiresAt := now.Add(defaultTalkLease)
	changed, err := s.repo.Transition(ctx, session.SessionID, models.TalkSessionInviting, models.TalkSessionActive, TransitionPatch{StartedAt: &now, ExpiresAt: &expiresAt})
	if err != nil {
		return err
	}
	if !changed {
		return errors.New("Broadcast ACK 状态已变化")
	}
	phase := models.TalkSignalPhaseActive
	_, err = s.repo.UpdateBroadcastFacts(ctx, session.SessionID, BroadcastFactsPatch{SignalPhase: &phase})
	return err
}

func (s *Service) OnBroadcastBye(ctx context.Context, callID string) error {
	session, err := s.repo.FindByCallID(ctx, callID)
	if err != nil || session == nil || session.Mode != models.TalkSessionModeBroadcast || session.State.IsTerminal() {
		return err
	}
	return s.Cleanup(ctx, session.SessionID, models.TalkSessionEnded, "device sent Broadcast BYE")
}

func statusFromError(err error, fallback int) int {
	var sipErr interface{ SIPStatus() int }
	if errors.As(err, &sipErr) {
		return sipErr.SIPStatus()
	}
	return fallback
}
