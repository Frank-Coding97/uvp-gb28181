package talk

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/sdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

const (
	defaultTalkVHost      = "__defaultVhost__"
	defaultTalkCloseDelay = 12000
	defaultTalkLease      = 30 * time.Second
)

var (
	ErrTalkActivationUnavailable = errors.New("对讲激活服务未装配")
	ErrTalkSessionNotFound       = errors.New("对讲会话不存在")
	ErrTalkSourceUnavailable     = errors.New("对讲发布源不可用")
	ErrTalkCodecUnsupported      = errors.New("对讲发布源不是 PCMA/8000")
)

type TalkMediaClient interface {
	GetMediaInfo(context.Context, string, string, string, string) (*zlm.MediaInfo, error)
	StartSendRtpPassive(context.Context, zlm.TalkSendRtpRequest) (*zlm.StartSendRtpPassiveResult, error)
	StopSendRtp(context.Context, string, string, string, string) error
	CloseTalkSource(context.Context, string, string, string) error
}

type TalkInviter interface {
	InviteTalk(context.Context, uac.TalkInviteRequest) (uac.TalkDialogMetadata, error)
	ByeTalk(context.Context, string) error
}

type TalkTargetLoader interface {
	LoadTalkTarget(context.Context, uint, string) (*models.GbChannel, *models.GbDevice, error)
}

type ActivationPlatform struct {
	ServerID string
}

type ActivationDependencies struct {
	ClientFor func(*node.Node) TalkMediaClient
	Inviter   TalkInviter
	Targets   TalkTargetLoader
	Platform  ActivationPlatform
}

type activationRuntime struct {
	deps ActivationDependencies
	mu   sync.Mutex
	busy map[string]struct{}
}

func (s *Service) ConfigureActivation(deps ActivationDependencies) {
	if s == nil {
		return
	}
	if deps.ClientFor == nil {
		deps.ClientFor = func(mediaNode *node.Node) TalkMediaClient { return zlm.NewClientForNode(mediaNode) }
	}
	s.activation = &activationRuntime{deps: deps, busy: make(map[string]struct{})}
}

func (s *Service) OnPublished(ctx context.Context, nodeID int64, appName, sourceStream string) error {
	if s == nil || s.repo == nil || s.nodes == nil || s.activation == nil || s.activation.deps.Inviter == nil || s.activation.deps.Targets == nil {
		return ErrTalkActivationUnavailable
	}
	session, err := s.repo.FindBySource(ctx, nodeID, appName, sourceStream)
	if err != nil {
		return err
	}
	if session == nil {
		return ErrTalkSessionNotFound
	}
	if session.State != models.TalkSessionPublishing {
		return nil
	}
	if !s.activation.begin(session.SessionID) {
		return nil
	}
	defer s.activation.end(session.SessionID)

	mediaNode, ok := s.nodes.Get(nodeID)
	if !ok || mediaNode == nil || !mediaNode.IsActive() {
		return s.failActivation(ctx, session, nil, "", false, ErrTalkNodeUnavailable)
	}
	channel, device, err := s.activation.deps.Targets.LoadTalkTarget(ctx, session.ChannelID, session.DeviceID)
	if err != nil {
		return s.failActivation(ctx, session, nil, "", false, err)
	}
	if channel == nil || device == nil || channel.Status != models.ChannelStatusOnline || device.Status != models.DeviceStatusOnline {
		return s.failActivation(ctx, session, nil, "", false, ErrTalkTargetOffline)
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
		return s.failActivation(ctx, session, client, "", false, fmt.Errorf("%w: %v", ErrTalkSourceUnavailable, err))
	}
	if !hasReadyPCMA(info.Tracks) {
		return s.failActivation(ctx, session, client, "", false, ErrTalkCodecUnsupported)
	}
	if _, changed, err := s.activationState(ctx, session.SessionID, models.TalkSessionPublishing); err != nil || changed {
		return err
	}

	sender, err := client.StartSendRtpPassive(ctx, zlm.TalkSendRtpRequest{
		VHost: defaultTalkVHost, App: session.App, SourceStream: session.SourceStream,
		RecvStreamID: session.RecvStream, SSRC: session.SSRC, CloseDelayMS: defaultTalkCloseDelay,
	})
	if err != nil {
		return s.failActivation(ctx, session, client, "", true, err)
	}
	if _, changed, stateErr := s.activationState(ctx, session.SessionID, models.TalkSessionPublishing); stateErr != nil || changed {
		_ = client.StopSendRtp(ctx, defaultTalkVHost, session.App, session.SourceStream, session.SSRC)
		_ = client.CloseTalkSource(ctx, defaultTalkVHost, session.App, session.SourceStream)
		return stateErr
	}
	changed, err := s.repo.Transition(ctx, session.SessionID, models.TalkSessionPublishing, models.TalkSessionInviting,
		TransitionPatch{LocalPort: &sender.LocalPort})
	if err != nil || !changed {
		_ = client.StopSendRtp(ctx, defaultTalkVHost, session.App, session.SourceStream, session.SSRC)
		_ = client.CloseTalkSource(ctx, defaultTalkVHost, session.App, session.SourceStream)
		return err
	}

	talkSDP := sdp.BuildTalkSDP(sdp.TalkParams{
		ServerID: s.activation.deps.Platform.ServerID, RecvIP: mediaNode.Host,
		RecvPort: sender.LocalPort, SSRC: session.SSRC,
	})
	metadata, err := s.activation.deps.Inviter.InviteTalk(ctx, uac.TalkInviteRequest{
		DeviceID: session.DeviceID, ChannelID: channel.ChannelID,
		Destination: net.JoinHostPort(device.IP, strconv.Itoa(device.Port)), Transport: device.Transport,
		SSRC: session.SSRC, SDP: talkSDP,
	})
	if err != nil {
		return s.failActivation(ctx, session, client, metadata.CallID, true, err)
	}
	if _, changed, stateErr := s.activationState(ctx, session.SessionID, models.TalkSessionInviting); stateErr != nil || changed {
		_ = s.activation.deps.Inviter.ByeTalk(ctx, metadata.CallID)
		_ = client.StopSendRtp(ctx, defaultTalkVHost, session.App, session.SourceStream, session.SSRC)
		_ = client.CloseTalkSource(ctx, defaultTalkVHost, session.App, session.SourceStream)
		return stateErr
	}
	now := s.now().UTC()
	cseq := uint(metadata.CSeq)
	expiresAt := now.Add(defaultTalkLease)
	changed, err = s.repo.Transition(ctx, session.SessionID, models.TalkSessionInviting, models.TalkSessionActive, TransitionPatch{
		CallID: &metadata.CallID, CSeq: &cseq, StartedAt: &now, ExpiresAt: &expiresAt,
	})
	if err != nil || !changed {
		_ = s.activation.deps.Inviter.ByeTalk(ctx, metadata.CallID)
		_ = client.StopSendRtp(ctx, defaultTalkVHost, session.App, session.SourceStream, session.SSRC)
		_ = client.CloseTalkSource(ctx, defaultTalkVHost, session.App, session.SourceStream)
		return err
	}
	return nil
}

func (s *Service) activationState(ctx context.Context, sessionID string, expected models.TalkSessionState) (*models.GbTalkSession, bool, error) {
	current, err := s.repo.FindBySession(ctx, sessionID)
	if err != nil {
		return nil, false, err
	}
	if current == nil {
		return nil, false, ErrTalkSessionNotFound
	}
	return current, current.State != expected, nil
}

func (s *Service) failActivation(ctx context.Context, session *models.GbTalkSession, client TalkMediaClient, callID string, senderStarted bool, cause error) error {
	if callID != "" && s.activation != nil && s.activation.deps.Inviter != nil {
		_ = s.activation.deps.Inviter.ByeTalk(ctx, callID)
	}
	if client != nil && senderStarted {
		_ = client.StopSendRtp(ctx, defaultTalkVHost, session.App, session.SourceStream, session.SSRC)
	}
	if client != nil {
		_ = client.CloseTalkSource(ctx, defaultTalkVHost, session.App, session.SourceStream)
	}
	current, findErr := s.repo.FindBySession(ctx, session.SessionID)
	if findErr == nil && current != nil && current.State != models.TalkSessionStopping && !current.State.IsTerminal() {
		message := cause.Error()
		if len(message) > 500 {
			message = message[:500]
		}
		_, _ = s.repo.FinishAndReleaseLease(ctx, session.SessionID, models.TalkSessionFailed, message, s.now().UTC())
	}
	return cause
}

func hasReadyPCMA(tracks []zlm.MediaTrack) bool {
	for _, track := range tracks {
		if track.CodecType == 1 && track.Ready && strings.EqualFold(strings.TrimSpace(track.CodecIDName), "PCMA") &&
			(track.SampleRate == 0 || track.SampleRate == 8000) {
			return true
		}
	}
	return false
}

func (r *activationRuntime) begin(sessionID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.busy[sessionID]; exists {
		return false
	}
	r.busy[sessionID] = struct{}{}
	return true
}

func (r *activationRuntime) end(sessionID string) {
	r.mu.Lock()
	delete(r.busy, sessionID)
	r.mu.Unlock()
}
