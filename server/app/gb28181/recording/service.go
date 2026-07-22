package recording

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type Repository interface {
	GetChannel(context.Context, uint) (*models.GbChannel, error)
	FindChannelByStream(context.Context, string) (*models.GbChannel, error)
	SetDesired(context.Context, uint, bool) (*models.GbChannel, error)
	MarkState(context.Context, uint, bool, string, string) (bool, error)
	ListEnabledChannels(context.Context) ([]models.GbChannel, error)
	UpsertSession(context.Context, *models.GbRecordingSession) error
	FindSessionByMedia(context.Context, int64, string, string, string) (*models.GbRecordingSession, error)
	FindLatestSessionByChannel(context.Context, uint) (*models.GbRecordingSession, error)
	MarkSessionStopped(context.Context, uint64, string) error
	ListUnfinishedSessions(context.Context) ([]models.GbRecordingSession, error)
	InsertFile(context.Context, *models.GbRecordingFile) (bool, error)
}

type StreamStarter interface {
	Start(context.Context, string, string) (*play.Result, error)
}

type StreamStopper interface {
	Stop(context.Context, string) error
}

type LocationLookup interface {
	Lookup(string) (int64, bool)
}

type NodeLookup interface {
	Get(int64) (*node.Node, bool)
}

type RecorderClient interface {
	IsRecording(context.Context, string, string, string) (bool, error)
	StartRecord(context.Context, string, string, string, int) error
	StopRecord(context.Context, string, string, string) error
	GetMediaInfo(context.Context, string, string) (*zlm.MediaInfo, error)
}

type ClientFactory func(*node.Node) RecorderClient

type Service struct {
	repo     Repository
	starter  StreamStarter
	stopper  StreamStopper
	location LocationLookup
	registry NodeLookup
	client   ClientFactory

	mu sync.Mutex
}

func NewService(repo Repository, starter StreamStarter, stopper StreamStopper, location LocationLookup, registry NodeLookup, client ClientFactory) *Service {
	return &Service{
		repo: repo, starter: starter, stopper: stopper,
		location: location, registry: registry, client: client,
	}
}

func (s *Service) Enable(ctx context.Context, channelID uint) (*models.GbChannel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	channel, err := s.repo.SetDesired(ctx, channelID, true)
	if err != nil {
		return nil, err
	}
	return s.reconcileEnabledLocked(ctx, channel)
}

func (s *Service) Disable(ctx context.Context, channelID uint) (*models.GbChannel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	channel, err := s.repo.SetDesired(ctx, channelID, false)
	if err != nil {
		return nil, err
	}
	session, err := s.repo.FindLatestSessionByChannel(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if session == nil || session.State == models.RecordingSessionStateStopped {
		if _, err := s.repo.MarkState(ctx, channelID, false, models.CloudRecordingStateDisabled, ""); err != nil {
			return nil, err
		}
		return s.repo.GetChannel(ctx, channelID)
	}

	mediaNode, ok := s.registry.Get(session.NodeID)
	if !ok {
		return s.markDisableFailure(ctx, channelID, session, fmt.Sprintf("ZLM 节点 %d 不可用", session.NodeID))
	}
	client := s.client(mediaNode)
	if err := client.StopRecord(ctx, session.VHost, session.App, session.Stream); err != nil {
		return s.markDisableFailure(ctx, channelID, session, "停止录像失败: "+err.Error())
	}

	mediaInfo, mediaErr := client.GetMediaInfo(ctx, session.App, session.Stream)
	if err := s.repo.MarkSessionStopped(ctx, session.ID, ""); err != nil {
		return nil, err
	}
	if mediaErr == nil && mediaInfo.ReaderCount == 0 && channel.OnDemandLive && s.stopper != nil {
		if err := s.stopper.Stop(ctx, session.Stream); err != nil {
			if _, markErr := s.repo.MarkState(ctx, channelID, false, models.CloudRecordingStateFailed, "停止空闲流失败: "+err.Error()); markErr != nil {
				return nil, markErr
			}
			return s.repo.GetChannel(ctx, channelID)
		}
	}
	if _, err := s.repo.MarkState(ctx, channelID, false, models.CloudRecordingStateDisabled, ""); err != nil {
		return nil, err
	}
	return s.repo.GetChannel(ctx, channelID)
}

func (s *Service) reconcileEnabledLocked(ctx context.Context, channel *models.GbChannel) (*models.GbChannel, error) {
	result, err := s.starter.Start(ctx, channel.DeviceID, channel.ChannelID)
	if err != nil {
		state := models.CloudRecordingStateFailed
		message := "拉流失败: " + err.Error()
		if errors.Is(err, play.ErrDeviceOffline) {
			state = models.CloudRecordingStateWaiting
			message = "设备离线,等待恢复"
		}
		if _, markErr := s.repo.MarkState(ctx, channel.ID, true, state, message); markErr != nil {
			return nil, markErr
		}
		return s.repo.GetChannel(ctx, channel.ID)
	}

	nodeID, ok := s.location.Lookup(result.StreamID)
	if !ok {
		_, err = s.repo.MarkState(ctx, channel.ID, true, models.CloudRecordingStateWaiting, "媒体流尚未绑定 ZLM 节点")
		if err != nil {
			return nil, err
		}
		return s.repo.GetChannel(ctx, channel.ID)
	}
	mediaNode, ok := s.registry.Get(nodeID)
	if !ok {
		_, err = s.repo.MarkState(ctx, channel.ID, true, models.CloudRecordingStateWaiting, fmt.Sprintf("ZLM 节点 %d 不可用", nodeID))
		if err != nil {
			return nil, err
		}
		return s.repo.GetChannel(ctx, channel.ID)
	}

	now := time.Now()
	session := &models.GbRecordingSession{
		ChannelID: channel.ID, DeviceID: channel.DeviceID, NodeID: nodeID,
		VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp,
		Stream: result.StreamID, State: models.RecordingSessionStateStarting,
		StartedAt: &now, LastCheckedAt: &now,
	}
	if err := s.repo.UpsertSession(ctx, session); err != nil {
		return nil, err
	}

	client := s.client(mediaNode)
	recording, err := client.IsRecording(ctx, session.VHost, session.App, session.Stream)
	if err != nil {
		return s.markEnableFailure(ctx, channel.ID, session, "查询录像状态失败: "+err.Error())
	}
	if !recording {
		if err := client.StartRecord(ctx, session.VHost, session.App, session.Stream, 0); err != nil {
			return s.markEnableFailure(ctx, channel.ID, session, "启动录像失败: "+err.Error())
		}
	}

	session.State = models.RecordingSessionStateRecording
	session.LastError = ""
	session.LastCheckedAt = &now
	if err := s.repo.UpsertSession(ctx, session); err != nil {
		return nil, err
	}
	if _, err := s.repo.MarkState(ctx, channel.ID, true, models.CloudRecordingStateRecording, ""); err != nil {
		return nil, err
	}
	return s.repo.GetChannel(ctx, channel.ID)
}

func (s *Service) markEnableFailure(ctx context.Context, channelID uint, session *models.GbRecordingSession, message string) (*models.GbChannel, error) {
	session.State = models.RecordingSessionStateFailed
	session.LastError = message
	_ = s.repo.UpsertSession(ctx, session)
	if _, err := s.repo.MarkState(ctx, channelID, true, models.CloudRecordingStateFailed, message); err != nil {
		return nil, err
	}
	return s.repo.GetChannel(ctx, channelID)
}

func (s *Service) markDisableFailure(ctx context.Context, channelID uint, session *models.GbRecordingSession, message string) (*models.GbChannel, error) {
	session.State = models.RecordingSessionStateFailed
	session.LastError = message
	_ = s.repo.UpsertSession(ctx, session)
	if _, err := s.repo.MarkState(ctx, channelID, false, models.CloudRecordingStateFailed, message); err != nil {
		return nil, err
	}
	return s.repo.GetChannel(ctx, channelID)
}
