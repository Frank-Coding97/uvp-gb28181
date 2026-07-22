package talk

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

var (
	ErrTalkCapabilityUnknown    = errors.New("设备未上报对讲能力")
	ErrTalkUnsupported          = errors.New("设备明确不支持对讲")
	ErrTalkTargetOffline        = errors.New("设备或通道离线")
	ErrTalkNodeUnavailable      = errors.New("无可用对讲媒体节点")
	ErrSecurePublishUnavailable = errors.New("媒体节点未提供安全发布端口")
)

type TalkRepo interface {
	Create(context.Context, *models.GbTalkSession, string) error
	FindBySession(context.Context, string) (*models.GbTalkSession, error)
	FindBySource(context.Context, int64, string, string) (*models.GbTalkSession, error)
	ConsumeTokenForPublish(context.Context, string, string, string, time.Time) (bool, error)
	Transition(context.Context, string, models.TalkSessionState, models.TalkSessionState, TransitionPatch) (bool, error)
}

type TalkNodeRegistry interface {
	Get(int64) (*node.Node, bool)
}

type TalkLocationStore interface {
	Lookup(string) (int64, bool)
}

type TalkNodePicker interface {
	Pick(context.Context, play.PickContext) (*node.Node, error)
}

type TalkConfigProvider interface {
	Get(context.Context, int64) (node.ServerConfig, error)
}

type Service struct {
	repo      TalkRepo
	nodes     TalkNodeRegistry
	locations TalkLocationStore
	picker    TalkNodePicker
	configs   TalkConfigProvider
	now       func() time.Time
}

type CreateRequest struct {
	Channel     *models.GbChannel
	Device      *models.GbDevice
	ActorID     uint
	ActorDeptID uint
}

type CreateResult struct {
	SessionID    string    `json:"sessionId"`
	State        string    `json:"state"`
	NodeID       int64     `json:"nodeId"`
	NodeName     string    `json:"nodeName"`
	SourceStream string    `json:"sourceStream"`
	RecvStream   string    `json:"recvStream"`
	SSRC         string    `json:"ssrc"`
	PublishURL   string    `json:"publishUrl"`
	PublishToken string    `json:"publishToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

type PublishAuthorization struct {
	NodeID       int64
	App          string
	SourceStream string
	PublishToken string
	PublishID    string
}

func NewService(repo TalkRepo, nodes TalkNodeRegistry, locations TalkLocationStore, picker TalkNodePicker, configs TalkConfigProvider, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{repo: repo, nodes: nodes, locations: locations, picker: picker, configs: configs, now: now}
}

func (s *Service) Create(ctx context.Context, request CreateRequest) (*CreateResult, error) {
	if request.Channel == nil || request.Device == nil || request.Channel.Status != models.ChannelStatusOnline || request.Device.Status != models.DeviceStatusOnline {
		return nil, ErrTalkTargetOffline
	}
	if err := requireTalkCapability(request.Channel.Capabilities); err != nil {
		return nil, err
	}
	sessionID := uuid.NewString()
	mediaNode, err := s.selectNode(ctx, request.Channel, sessionID)
	if err != nil {
		return nil, err
	}
	config, err := s.configs.Get(ctx, mediaNode.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTalkNodeUnavailable, err)
	}
	if config.HTTPSPort <= 0 {
		return nil, ErrSecurePublishUnavailable
	}
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	compactID := strings.ReplaceAll(sessionID, "-", "")
	sourceStream := "talk_" + compactID
	recvStream := "talk_recv_" + compactID
	ssrc, err := randomSSRC()
	if err != nil {
		return nil, err
	}
	expiresAt := s.now().UTC().Add(30 * time.Second)
	session := &models.GbTalkSession{
		SessionID: sessionID, ChannelID: request.Channel.ID, DeviceID: request.Device.DeviceID,
		ActorID: request.ActorID, ActorDeptID: request.ActorDeptID,
		NodeID: mediaNode.ID, App: "talk", SourceStream: sourceStream, RecvStream: recvStream, SSRC: ssrc,
		State: models.TalkSessionReserved, ExpiresAt: expiresAt,
	}
	if err := s.repo.Create(ctx, session, token); err != nil {
		return nil, err
	}
	query := url.Values{"app": {"talk"}, "stream": {sourceStream}, "token": {token}}
	publishURL := fmt.Sprintf("https://%s:%d/index/api/whip?%s", mediaNode.Host, config.HTTPSPort, query.Encode())
	return &CreateResult{
		SessionID: sessionID, State: string(session.State), NodeID: mediaNode.ID, NodeName: mediaNode.Name,
		SourceStream: sourceStream, RecvStream: recvStream, SSRC: ssrc,
		PublishURL: publishURL, PublishToken: token, ExpiresAt: expiresAt,
	}, nil
}

func (s *Service) AuthorizePublish(ctx context.Context, request PublishAuthorization) (bool, error) {
	if request.NodeID <= 0 || request.App != "talk" || request.SourceStream == "" || request.PublishID == "" {
		return false, nil
	}
	session, err := s.repo.FindBySource(ctx, request.NodeID, request.App, request.SourceStream)
	if err != nil || session == nil {
		return false, err
	}
	allowed, err := s.repo.ConsumeTokenForPublish(ctx, session.SessionID, request.PublishToken, request.PublishID, s.now().UTC())
	if err != nil || !allowed {
		return allowed, err
	}
	current, err := s.repo.FindBySession(ctx, session.SessionID)
	if err != nil || current == nil {
		return false, err
	}
	if current.State == models.TalkSessionPublishing && current.PublishID == request.PublishID {
		return true, nil
	}
	if current.State != models.TalkSessionReserved {
		return false, nil
	}
	changed, err := s.repo.Transition(ctx, current.SessionID, models.TalkSessionReserved, models.TalkSessionPublishing, TransitionPatch{})
	if err != nil {
		return false, err
	}
	if changed {
		return true, nil
	}
	current, err = s.repo.FindBySession(ctx, session.SessionID)
	return err == nil && current != nil && current.State == models.TalkSessionPublishing && current.PublishID == request.PublishID, err
}

func (s *Service) selectNode(ctx context.Context, channel *models.GbChannel, sessionID string) (*node.Node, error) {
	if channel.StreamID != "" && s.locations != nil && s.nodes != nil {
		if nodeID, ok := s.locations.Lookup(channel.StreamID); ok {
			if mediaNode, exists := s.nodes.Get(nodeID); exists && mediaNode.IsActive() {
				return mediaNode, nil
			}
		}
	}
	if s.picker == nil {
		return nil, ErrTalkNodeUnavailable
	}
	mediaNode, err := s.picker.Pick(ctx, play.PickContext{DeviceID: channel.DeviceID, ChannelID: channel.ChannelID, StreamID: sessionID})
	if err != nil || mediaNode == nil {
		return nil, fmt.Errorf("%w: %v", ErrTalkNodeUnavailable, err)
	}
	return mediaNode, nil
}

func requireTalkCapability(raw *string) error {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return ErrTalkCapabilityUnknown
	}
	var values map[string]json.RawMessage
	if json.Unmarshal([]byte(*raw), &values) != nil {
		return ErrTalkCapabilityUnknown
	}
	for _, key := range []string{"talk", "voice_talk", "audio_talk"} {
		value, exists := values[key]
		if !exists {
			continue
		}
		var supported bool
		if json.Unmarshal(value, &supported) != nil {
			return ErrTalkCapabilityUnknown
		}
		if supported {
			return nil
		}
		return ErrTalkUnsupported
	}
	return ErrTalkCapabilityUnknown
}

func randomToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func randomSSRC() (string, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(9000000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%010d", value.Int64()+1000000000), nil
}
