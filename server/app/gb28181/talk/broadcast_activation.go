package talk

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync/atomic"

	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/sdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm"
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
	if !mediaNode.HasUsableSDPIP() {
		return s.failActivation(ctx, session, nil, "", false, fmt.Errorf("%w: 媒体节点未配置 SDP 地址", ErrTalkNodeUnavailable))
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
	if !hasReadyAudio(info.Tracks, talkAudioCodec(session)) {
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
		if current.SignalPhase == models.TalkSignalPhaseWaitingAck || current.SignalPhase == models.TalkSignalPhaseActive {
			return nil
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
	if len(candidates) == 0 && strings.TrimSpace(invite.PeerID) != "" {
		candidates, err = s.repo.FindPendingBroadcast(ctx, "", strings.TrimSpace(invite.PeerID))
		if err != nil {
			return PreparedBroadcastInvite{}, err
		}
	}
	// 部分设备的 Subject 只含平台 ID，From 为设备 ID，没有回显通道 ID。
	// 保留上面的通道格式兼容查询，再按设备匹配；多个候选仍由下方拒绝。
	if len(candidates) == 0 && strings.TrimSpace(invite.PeerID) != "" {
		candidates, err = s.repo.FindPendingBroadcast(ctx, strings.TrimSpace(invite.PeerID), "")
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
	// 平台只能发会话配置的那一种编码（ZLM 按 PT 打包、不转码），所以设备
	// offer 里必须真的包含它；否则回 488 并在 reason 里写清楚差在哪，
	// 现场照着改设备编码即可，不用再猜「为什么没声音」。
	codec := talkAudioCodec(session)
	if !media.Supports(codec) {
		return PreparedBroadcastInvite{}, &BroadcastSIPError{
			Status: 488,
			Reason: fmt.Sprintf("设备 offer 不含会话编码 %s（设备提供：%s）", codec.Label(), describeCodecs(media.Codecs)),
		}
	}
	claimed, err := s.repo.ClaimBroadcastDialog(ctx, session.SessionID, invite.CallID, invite.CSeq)
	if err != nil {
		return PreparedBroadcastInvite{}, err
	}
	if !claimed {
		current, findErr := s.repo.FindBySession(ctx, session.SessionID)
		if findErr != nil {
			return PreparedBroadcastInvite{}, findErr
		}
		if current == nil || current.CallID != strings.TrimSpace(invite.CallID) || current.LocalPort <= 0 {
			return PreparedBroadcastInvite{}, &BroadcastSIPError{Status: 486, Reason: "Broadcast dialog 已被其他 INVITE 认领"}
		}
		answer, answerErr := sdp.BuildBroadcastAnswer(sdp.BroadcastAnswerParams{ServerID: s.activation.deps.Platform.ServerID, LocalIP: s.broadcastLocalIP(current.NodeID), LocalPort: current.LocalPort, SSRC: current.SSRC, SenderMode: sdp.BroadcastSenderMode(current.SenderMode), Codec: talkAudioCodec(current)})
		return PreparedBroadcastInvite{SessionID: current.SessionID, AnswerSDP: answer}, answerErr
	}
	mediaNode, ok := s.nodes.Get(session.NodeID)
	if !ok || mediaNode == nil || !mediaNode.IsActive() {
		return PreparedBroadcastInvite{}, ErrTalkNodeUnavailable
	}
	client := s.activation.deps.ClientFor(mediaNode)
	broadcastClient, ok := client.(broadcastMediaClient)
	if !ok || broadcastClient == nil {
		return PreparedBroadcastInvite{}, s.failActivation(ctx, session, client, "", false, ErrTalkActivationUnavailable)
	}
	info, err := client.GetMediaInfo(ctx, "rtsp", defaultTalkVHost, session.App, session.SourceStream)
	if err != nil || info == nil || !info.Online || !hasReadyAudio(info.Tracks, codec) {
		return PreparedBroadcastInvite{}, s.failActivation(ctx, session, client, "", false, ErrTalkSourceUnavailable)
	}
	pt := codec.PayloadType()
	localPort := 0
	if media.SenderMode == sdp.BroadcastSenderTCPPassive {
		result, startErr := client.StartSendRtpPassive(ctx, zlm.TalkSendRtpRequest{VHost: defaultTalkVHost, App: session.App, SourceStream: session.SourceStream, RecvStreamID: session.RecvStream, SSRC: session.SSRC, CloseDelayMS: defaultTalkCloseDelay, PayloadType: &pt})
		if startErr != nil {
			return PreparedBroadcastInvite{}, s.failActivation(ctx, session, client, "", false, startErr)
		}
		localPort = result.LocalPort
	} else {
		result, startErr := broadcastClient.StartBroadcastSendRtp(ctx, zlm.BroadcastSendRtpRequest{VHost: defaultTalkVHost, App: session.App, SourceStream: session.SourceStream, SSRC: session.SSRC, RemoteIP: media.RemoteIP, RemotePort: media.RemotePort, IsUDP: media.SenderMode == sdp.BroadcastSenderUDP, PayloadType: &pt})
		if startErr != nil {
			return PreparedBroadcastInvite{}, s.failActivation(ctx, session, client, "", false, startErr)
		}
		localPort = result.LocalPort
	}
	if session.State == models.TalkSessionPublishing {
		// ⛔ 这里的 session.State 是本轮开头 FindPendingBroadcast 的**旧快照**，中间隔着
		// ParseBroadcastOffer / ClaimBroadcastDialog / GetMediaInfo / StartSendRtpPassive
		// 两次 ZLM HTTP 调用（实测 100ms+）。而 onBroadcastPublished（on_stream_changed
		// 触发）同样会做 publishing→inviting —— CAS 的 from 用旧值必然落空。
		// 落空**不等于失败**：状态已经前进到 inviting 正是我们要的结果，继续应答即可。
		changed, transitionErr := s.repo.Transition(ctx, session.SessionID, models.TalkSessionPublishing, models.TalkSessionInviting, TransitionPatch{})
		if transitionErr != nil {
			_ = client.StopSendRtp(ctx, defaultTalkVHost, session.App, session.SourceStream, session.SSRC)
			return PreparedBroadcastInvite{}, transitionErr
		}
		if !changed {
			latest, findErr := s.repo.FindBySession(ctx, session.SessionID)
			if findErr != nil {
				_ = client.StopSendRtp(ctx, defaultTalkVHost, session.App, session.SourceStream, session.SSRC)
				return PreparedBroadcastInvite{}, findErr
			}
			if latest == nil {
				_ = client.StopSendRtp(ctx, defaultTalkVHost, session.App, session.SourceStream, session.SSRC)
				return PreparedBroadcastInvite{}, ErrTalkSessionNotFound
			}
			if latest.State.IsTerminal() || latest.State == models.TalkSessionStopping {
				_ = client.StopSendRtp(ctx, defaultTalkVHost, session.App, session.SourceStream, session.SSRC)
				return PreparedBroadcastInvite{}, &BroadcastSIPError{Status: 480, Reason: "Broadcast 会话已结束"}
			}
			session = latest
		}
	}
	phase := models.TalkSignalPhaseWaitingAck
	transport := strings.ToLower(media.Transport)
	senderMode := string(media.SenderMode)
	changed, err := s.repo.UpdateBroadcastFacts(ctx, session.SessionID, BroadcastFactsPatch{SignalPhase: &phase, RemoteMediaIP: &media.RemoteIP, RemotePort: &media.RemotePort, Transport: &transport, SenderMode: &senderMode, LocalPort: &localPort})
	if err != nil {
		_ = client.StopSendRtp(ctx, defaultTalkVHost, session.App, session.SourceStream, session.SSRC)
		return PreparedBroadcastInvite{}, err
	}
	if !changed {
		// 媒体事实没落库 = 会话已离开可协商状态（被清理 / 租约回收）。
		// ⛔ 绝不能 `return PreparedBroadcastInvite{}, err`（此时 err 为 nil）：那会让 SIP 层
		// 回一个**没有 SDP 正文的 200 OK**，设备解析不出平台收流地址，只能 ACK 后立刻 BYE
		// （模拟器日志："语音广播编码不可接受(payloadTypes=null) → BYE"）。明确失败更好。
		_ = client.StopSendRtp(ctx, defaultTalkVHost, session.App, session.SourceStream, session.SSRC)
		return PreparedBroadcastInvite{}, &BroadcastSIPError{Status: 480, Reason: "Broadcast 会话状态已变化"}
	}
	answer, err := sdp.BuildBroadcastAnswer(sdp.BroadcastAnswerParams{ServerID: s.activation.deps.Platform.ServerID, LocalIP: mediaNode.EffectiveSDPIP(), LocalPort: localPort, SSRC: session.SSRC, SenderMode: media.SenderMode, Codec: codec})
	if err != nil {
		_ = client.StopSendRtp(ctx, defaultTalkVHost, session.App, session.SourceStream, session.SSRC)
		return PreparedBroadcastInvite{}, err
	}
	return PreparedBroadcastInvite{SessionID: session.SessionID, AnswerSDP: answer}, nil
}

func (s *Service) broadcastLocalIP(nodeID int64) string {
	if s != nil && s.nodes != nil {
		if mediaNode, ok := s.nodes.Get(nodeID); ok && mediaNode != nil {
			return mediaNode.EffectiveSDPIP()
		}
	}
	return ""
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
		return errors.New("broadcast ACK 状态已变化")
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
