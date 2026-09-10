package recording

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
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
	FindSessionByID(context.Context, uint64) (*models.GbRecordingSession, error)
	FindLatestSessionByChannel(context.Context, uint) (*models.GbRecordingSession, error)
	MarkSessionStopped(context.Context, uint64, string) error
	ListUnfinishedSessions(context.Context) ([]models.GbRecordingSession, error)
	InsertFile(context.Context, *models.GbRecordingFile) (bool, error)
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
}

type ClientFactory func(*node.Node) RecorderClient

type Service struct {
	repo     Repository
	location LocationLookup
	registry NodeLookup
	client   ClientFactory

	locks          *keyedLocker
	operationGuard func(context.Context, uint, string, func(context.Context) error) error

	lifecycleMu      sync.Mutex
	shutdownStarted  bool
	activeStartCount int
	activeStartsDone chan struct{}
	shutdownDone     chan struct{}
	shutdownErr      error
}

var (
	ErrServiceShuttingDown  = errors.New("云端录像服务正在关闭")
	ErrRecordingStillActive = errors.New("媒体录像停止后仍处于录制状态")
)

func NewService(repo Repository, location LocationLookup, registry NodeLookup, client ClientFactory) *Service {
	activeStartsDone := make(chan struct{})
	close(activeStartsDone)
	return &Service{
		repo: repo, location: location, registry: registry, client: client,
		locks: newKeyedLocker(), activeStartsDone: activeStartsDone, shutdownDone: make(chan struct{}),
	}
}

func (s *Service) acceptStart() error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.shutdownStarted {
		return ErrServiceShuttingDown
	}
	if s.activeStartCount == 0 {
		s.activeStartsDone = make(chan struct{})
	}
	s.activeStartCount++
	return nil
}

func (s *Service) releaseStart() {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.activeStartCount == 0 {
		return
	}
	s.activeStartCount--
	if s.activeStartCount == 0 {
		close(s.activeStartsDone)
	}
}

// Shutdown drains accepted recording starts and then stops every unfinished
// media session. It leaves the channel's desired recording flag unchanged.
func (s *Service) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	s.lifecycleMu.Lock()
	if s.shutdownStarted {
		done := s.shutdownDone
		s.lifecycleMu.Unlock()
		return s.waitShutdown(ctx, done)
	}
	s.shutdownStarted = true
	startsDone := s.activeStartsDone
	s.lifecycleMu.Unlock()

	if err := waitForShutdown(ctx, startsDone); err != nil {
		s.completeShutdown(err)
		return err
	}
	sessions, err := s.repo.ListUnfinishedSessions(ctx)
	if err != nil {
		s.completeShutdown(err)
		return err
	}

	var shutdownErr error
	for index := range sessions {
		if err := ctx.Err(); err != nil {
			shutdownErr = errors.Join(shutdownErr, err)
			break
		}
		if err := s.stopUnfinishedSession(ctx, &sessions[index]); err != nil {
			shutdownErr = errors.Join(shutdownErr, err)
		}
	}
	s.completeShutdown(shutdownErr)
	return shutdownErr
}

func (s *Service) waitShutdown(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return s.shutdownResult()
	default:
	}
	select {
	case <-done:
		return s.shutdownResult()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) shutdownResult() error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	return s.shutdownErr
}

func (s *Service) completeShutdown(err error) {
	s.lifecycleMu.Lock()
	s.shutdownErr = err
	close(s.shutdownDone)
	s.lifecycleMu.Unlock()
}

func waitForShutdown(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	default:
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) stopUnfinishedSession(ctx context.Context, session *models.GbRecordingSession) error {
	if session == nil || session.State == models.RecordingSessionStateStopped {
		return nil
	}
	unlock, err := s.locks.LockContext(ctx, session.ChannelID)
	if err != nil {
		return err
	}
	defer unlock()

	channel, err := s.repo.GetChannel(ctx, session.ChannelID)
	if err != nil {
		return err
	}
	if channel == nil {
		return ErrChannelNotFound
	}
	if s.registry == nil {
		return s.recordShutdownFailure(ctx, channel, session, errors.New("录像节点注册表不可用"), "录像节点注册表不可用")
	}
	mediaNode, ok := s.registry.Get(session.NodeID)
	if !ok || mediaNode == nil {
		cause := fmt.Errorf("ZLM 节点 %d 不可用", session.NodeID)
		return s.recordShutdownFailure(ctx, channel, session, cause, cause.Error())
	}
	if s.client == nil {
		return s.recordShutdownFailure(ctx, channel, session, errors.New("录像客户端不可用"), "录像客户端不可用")
	}
	recorder := s.client(mediaNode)
	if recorder == nil {
		return s.recordShutdownFailure(ctx, channel, session, errors.New("录像客户端不可用"), "录像客户端不可用")
	}
	if err := recorder.StopRecord(ctx, session.VHost, session.App, session.Stream); err != nil {
		return s.recordShutdownFailure(ctx, channel, session, err, "停止录像失败: "+err.Error())
	}
	recording, err := recorder.IsRecording(ctx, session.VHost, session.App, session.Stream)
	if err != nil {
		return s.recordShutdownFailure(ctx, channel, session, err, "确认录像停止失败: "+err.Error())
	}
	if recording {
		return s.recordShutdownFailure(ctx, channel, session, ErrRecordingStillActive, ErrRecordingStillActive.Error())
	}
	if err := s.repo.MarkSessionStopped(ctx, session.ID, ""); err != nil {
		return err
	}
	state := models.CloudRecordingStateDisabled
	if channel.CloudRecordingEnabled {
		state = models.CloudRecordingStateWaiting
	}
	updated, err := s.repo.MarkState(ctx, channel.ID, channel.CloudRecordingEnabled, state, "")
	if err != nil {
		return err
	}
	if !updated {
		return errors.New("录像通道状态未更新")
	}
	return nil
}

func (s *Service) recordShutdownFailure(ctx context.Context, channel *models.GbChannel, session *models.GbRecordingSession, cause error, message string) error {
	_, markErr := s.markStopFailure(ctx, channel, session, message)
	if markErr != nil {
		return errors.Join(cause, markErr)
	}
	return cause
}

func (s *Service) ObserveStream(ctx context.Context, streamID string, registered bool) error {
	if registered {
		return nil
	}
	channel, err := s.repo.FindChannelByStream(ctx, streamID)
	if err != nil || channel == nil {
		return err
	}
	unlock := s.locks.Lock(channel.ID)
	defer unlock()

	current, err := s.repo.GetChannel(ctx, channel.ID)
	if err != nil {
		return err
	}
	session, err := s.repo.FindLatestSessionByChannel(ctx, current.ID)
	if err != nil {
		return err
	}
	if session != nil && session.Stream == streamID && session.State != models.RecordingSessionStateStopped {
		if err := s.repo.MarkSessionStopped(ctx, session.ID, "媒体流已注销"); err != nil {
			return err
		}
	}
	state := models.CloudRecordingStateDisabled
	message := ""
	if current.CloudRecordingEnabled {
		state = models.CloudRecordingStateWaiting
		message = "媒体流已中断,等待下次点播"
	}
	_, err = s.repo.MarkState(ctx, current.ID, current.CloudRecordingEnabled, state, message)
	return err
}

func (s *Service) enable(ctx context.Context, channelID uint) (*models.GbChannel, error) {
	unlock := s.locks.Lock(channelID)
	defer unlock()

	current, err := s.repo.GetChannel(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if current.CloudRecordingEnabled && current.CloudRecordingState == models.CloudRecordingStateRecording {
		return current, nil
	}

	return s.repo.SetDesired(ctx, channelID, true)
}

func (s *Service) beginPlayback(ctx context.Context, streamID string) error {
	channel, err := s.repo.FindChannelByStream(ctx, streamID)
	if err != nil || channel == nil {
		return err
	}
	unlock := s.locks.Lock(channel.ID)
	defer unlock()

	current, err := s.repo.GetChannel(ctx, channel.ID)
	if err != nil || !current.CloudRecordingEnabled {
		return err
	}
	_, err = s.startRecordingLocked(ctx, current, streamID)
	return err
}

func (s *Service) endPlayback(ctx context.Context, streamID string) error {
	channel, err := s.repo.FindChannelByStream(ctx, streamID)
	if err != nil || channel == nil {
		return err
	}
	unlock := s.locks.Lock(channel.ID)
	defer unlock()

	current, err := s.repo.GetChannel(ctx, channel.ID)
	if err != nil {
		return err
	}
	_, err = s.finishRecordingLocked(ctx, current, streamID)
	return err
}

func (s *Service) disable(ctx context.Context, channelID uint) (*models.GbChannel, error) {
	unlock := s.locks.Lock(channelID)
	defer unlock()

	current, err := s.repo.GetChannel(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if !current.CloudRecordingEnabled && current.CloudRecordingState == models.CloudRecordingStateDisabled {
		return current, nil
	}

	channel, err := s.repo.SetDesired(ctx, channelID, false)
	if err != nil {
		return nil, err
	}
	return s.disableLocked(ctx, channel)
}

func (s *Service) stopSession(ctx context.Context, channelID uint, sessionID uint64) (*models.GbChannel, error) {
	unlock := s.locks.Lock(channelID)
	defer unlock()

	current, err := s.repo.GetChannel(ctx, channelID)
	if err != nil {
		return nil, err
	}
	session, err := s.repo.FindSessionByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil || session.ChannelID != channelID {
		return nil, ErrRecordingSessionNotFound
	}
	channel, err := s.repo.SetDesired(ctx, current.ID, false)
	if err != nil {
		return nil, err
	}
	return s.stopSessionLocked(ctx, channel, session)
}

func (s *Service) reconcileChannel(ctx context.Context, channelID uint) error {
	unlock := s.locks.Lock(channelID)
	defer unlock()

	channel, err := s.repo.GetChannel(ctx, channelID)
	if err != nil {
		return err
	}
	if channel.CloudRecordingEnabled {
		return nil
	}
	_, err = s.disableLocked(ctx, channel)
	return err
}

func (s *Service) disableLocked(ctx context.Context, channel *models.GbChannel) (*models.GbChannel, error) {
	channelID := channel.ID
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
	return s.stopSessionLocked(ctx, channel, session)
}

func (s *Service) stopSessionLocked(ctx context.Context, channel *models.GbChannel, session *models.GbRecordingSession) (*models.GbChannel, error) {
	channelID := channel.ID
	if session.State == models.RecordingSessionStateStopped {
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

	if err := s.repo.MarkSessionStopped(ctx, session.ID, ""); err != nil {
		return nil, err
	}
	if _, err := s.repo.MarkState(ctx, channelID, false, models.CloudRecordingStateDisabled, ""); err != nil {
		return nil, err
	}
	return s.repo.GetChannel(ctx, channelID)
}

func (s *Service) startRecordingLocked(ctx context.Context, channel *models.GbChannel, streamID string) (*models.GbChannel, error) {
	if _, err := s.repo.MarkState(ctx, channel.ID, true, models.CloudRecordingStateStarting, ""); err != nil {
		return nil, err
	}
	nodeID, ok := s.location.Lookup(streamID)
	if !ok {
		_, err := s.repo.MarkState(ctx, channel.ID, true, models.CloudRecordingStateWaiting, "媒体流尚未绑定 ZLM 节点")
		if err != nil {
			return nil, err
		}
		return s.repo.GetChannel(ctx, channel.ID)
	}
	mediaNode, ok := s.registry.Get(nodeID)
	if !ok {
		_, err := s.repo.MarkState(ctx, channel.ID, true, models.CloudRecordingStateWaiting, fmt.Sprintf("ZLM 节点 %d 不可用", nodeID))
		if err != nil {
			return nil, err
		}
		return s.repo.GetChannel(ctx, channel.ID)
	}

	now := time.Now()
	session := &models.GbRecordingSession{
		ChannelID: channel.ID, DeviceID: channel.DeviceID, NodeID: nodeID,
		VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp,
		Stream: streamID, State: models.RecordingSessionStateStarting,
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

func (s *Service) finishRecordingLocked(ctx context.Context, channel *models.GbChannel, streamID string) (*models.GbChannel, error) {
	session, err := s.repo.FindLatestSessionByChannel(ctx, channel.ID)
	if err != nil {
		return nil, err
	}
	finalState := models.CloudRecordingStateDisabled
	if channel.CloudRecordingEnabled {
		finalState = models.CloudRecordingStateWaiting
	}
	if session == nil || session.Stream != streamID || session.State == models.RecordingSessionStateStopped {
		if _, err := s.repo.MarkState(ctx, channel.ID, channel.CloudRecordingEnabled, finalState, ""); err != nil {
			return nil, err
		}
		return s.repo.GetChannel(ctx, channel.ID)
	}
	mediaNode, ok := s.registry.Get(session.NodeID)
	if !ok {
		return s.markStopFailure(ctx, channel, session, fmt.Sprintf("ZLM 节点 %d 不可用", session.NodeID))
	}
	if err := s.client(mediaNode).StopRecord(ctx, session.VHost, session.App, session.Stream); err != nil {
		return s.markStopFailure(ctx, channel, session, "停止录像失败: "+err.Error())
	}
	if err := s.repo.MarkSessionStopped(ctx, session.ID, ""); err != nil {
		return nil, err
	}
	if _, err := s.repo.MarkState(ctx, channel.ID, channel.CloudRecordingEnabled, finalState, ""); err != nil {
		return nil, err
	}
	return s.repo.GetChannel(ctx, channel.ID)
}

func (s *Service) markStopFailure(ctx context.Context, channel *models.GbChannel, session *models.GbRecordingSession, message string) (*models.GbChannel, error) {
	session.State = models.RecordingSessionStateFailed
	session.LastError = message
	if err := s.repo.UpsertSession(ctx, session); err != nil {
		return nil, err
	}
	if _, err := s.repo.MarkState(ctx, channel.ID, channel.CloudRecordingEnabled, models.CloudRecordingStateFailed, message); err != nil {
		return nil, err
	}
	return s.repo.GetChannel(ctx, channel.ID)
}

func (s *Service) markEnableFailure(ctx context.Context, channelID uint, session *models.GbRecordingSession, message string) (*models.GbChannel, error) {
	session.State = models.RecordingSessionStateFailed
	session.LastError = message
	// 会话状态持久化失败不能无声吞掉:否则通道显示失败、会话停留在旧状态
	var sessionErr error
	if err := s.repo.UpsertSession(ctx, session); err != nil {
		sessionErr = fmt.Errorf("会话状态持久化失败: %w", err)
	}
	if _, markErr := s.repo.MarkState(ctx, channelID, true, models.CloudRecordingStateFailed, message); markErr != nil {
		return nil, errors.Join(markErr, sessionErr)
	}
	if sessionErr != nil {
		return nil, sessionErr
	}
	return s.repo.GetChannel(ctx, channelID)
}

func (s *Service) markDisableFailure(ctx context.Context, channelID uint, session *models.GbRecordingSession, message string) (*models.GbChannel, error) {
	session.State = models.RecordingSessionStateFailed
	session.LastError = message
	var sessionErr error
	if err := s.repo.UpsertSession(ctx, session); err != nil {
		sessionErr = fmt.Errorf("会话状态持久化失败: %w", err)
	}
	if _, markErr := s.repo.MarkState(ctx, channelID, false, models.CloudRecordingStateFailed, message); markErr != nil {
		return nil, errors.Join(markErr, sessionErr)
	}
	if sessionErr != nil {
		return nil, sessionErr
	}
	return s.repo.GetChannel(ctx, channelID)
}
