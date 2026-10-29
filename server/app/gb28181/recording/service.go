package recording

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.com/uvp-gb28181/app/global/app"
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

	locks *keyedLocker
}

func NewService(repo Repository, location LocationLookup, registry NodeLookup, client ClientFactory) *Service {
	return &Service{
		repo: repo, location: location, registry: registry, client: client,
		locks: newKeyedLocker(),
	}
}

// ObserveStream 是 ZLM on_stream_changed 的落点：流上线时按开关起录，流注销时收尾。
//
// ⛔⛔ 「自动开始录」必须挂在流上线事件上，不能挂在浏览器点播的 REST 接口里：
// 级联拉流（上级平台发 INVITE）、ZLM 主动推流都不经过那个接口。挂在那里就会
// 变成「本地点播能录、上级来拉不录」，而开关的语义是「这条通道每次有流上线就录」。
func (s *Service) ObserveStream(ctx context.Context, streamID string, registered bool) error {
	if streamID == "" {
		return nil
	}
	channel, err := s.repo.FindChannelByStream(ctx, streamID)
	if err != nil {
		return err
	}
	// 查不到就说明这条流不归任何 GB 通道：回放流（pb-*）、下载流、对讲流都走这里。
	// 另外 GORM 的 not-found 回调能把结果掩成零值，所以 ID 为 0 一律按「没有」处理。
	if channel == nil || channel.ID == 0 {
		return nil
	}
	unlock := s.locks.Lock(channel.ID)
	defer unlock()

	current, err := s.repo.GetChannel(ctx, channel.ID)
	if err != nil || current == nil {
		return err
	}
	if registered {
		return s.beginStreamRecording(ctx, current, streamID)
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

// beginStreamRecording 在流上线时按云端录像开关决定是否起录。
//
// 幂等性靠「同一路流已有未停止的会话」短路：浏览器点播那条路径仍会同步调用
// BeginPlayback（用户点开就录，不等 hook），随后 ZLM 的 on_stream_changed 还会
// 再来一次，没有这个短路就会对同一路流重复 StartRecord。会话状态为 failed 时
// 不短路，让上一次真正失败的能重试。
func (s *Service) beginStreamRecording(ctx context.Context, channel *models.GbChannel, streamID string) error {
	if !channel.CloudRecordingEnabled {
		return nil
	}
	session, err := s.repo.FindLatestSessionByChannel(ctx, channel.ID)
	if err != nil {
		return err
	}
	if session != nil && session.Stream == streamID &&
		session.State != models.RecordingSessionStateStopped &&
		session.State != models.RecordingSessionStateFailed {
		return nil
	}
	_, err = s.startRecordingLocked(ctx, channel, streamID)
	return err
}

// Enable 只把开关置为「期望录」并落到 waiting，**不补录已经在推的流**。
//
// ⛔⛔ 这是产品口径，不是缺陷：开关打开时若流已经在推，本次不回看现有流，
// 等这条通道下一次有流上线（下一次点播/上级拉流/设备重新推流）才起录。
// 因此这里刻意不调 ZLM getMediaList 做补录探测 —— 别把它当「缺口」修掉，
// TestEnableOnlyWaitsForNextPlayback 就是对这条契约的反向断言。
func (s *Service) Enable(ctx context.Context, channelID uint) (*models.GbChannel, error) {
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

func (s *Service) BeginPlayback(ctx context.Context, streamID string) error {
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

func (s *Service) EndPlayback(ctx context.Context, streamID string) error {
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

func (s *Service) Disable(ctx context.Context, channelID uint) (*models.GbChannel, error) {
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

func (s *Service) StopSession(ctx context.Context, channelID uint, sessionID uint64) (*models.GbChannel, error) {
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

func (s *Service) ReconcileChannel(ctx context.Context, channelID uint) error {
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
	if !ok || mediaNode == nil {
		// 节点下线/重启只是「媒体面暂时够不着」，不是录像失败。判 failed 会
		// 把通道钉死到必须人工重开云录像；这里按 enabled 落 waiting/disabled。
		return s.markNodeUnavailable(ctx, channel, session, "disable")
	}
	client := s.client(mediaNode)
	if err := client.StopRecord(ctx, session.VHost, session.App, session.Stream); err != nil {
		return s.markDisableFailure(ctx, channelID, session, err)
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
	if !ok || nodeID <= 0 {
		return s.markWaiting(ctx, channel.ID, "stream_not_bound", nodeUnavailableMessage(nodeID), nodeID, streamID)
	}
	mediaNode, ok := s.registry.Get(nodeID)
	if !ok || mediaNode == nil {
		return s.markWaiting(ctx, channel.ID, "node_unavailable", nodeUnavailableMessage(nodeID), nodeID, streamID)
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
	logCloudRecordingState(ctx, "recording.cloud.recording", "started", channel.ID, nodeID, streamID, models.CloudRecordingStateRecording, "")
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
	if !ok || mediaNode == nil {
		return s.markNodeUnavailable(ctx, channel, session, "stop")
	}
	if err := s.client(mediaNode).StopRecord(ctx, session.VHost, session.App, session.Stream); err != nil {
		return s.markStopFailure(ctx, channel, session, err)
	}
	if err := s.repo.MarkSessionStopped(ctx, session.ID, ""); err != nil {
		return nil, err
	}
	if _, err := s.repo.MarkState(ctx, channel.ID, channel.CloudRecordingEnabled, finalState, ""); err != nil {
		return nil, err
	}
	return s.repo.GetChannel(ctx, channel.ID)
}

// nodeUnavailableMessage 说明「媒体节点这条路暂时走不通」。
//
// ⛔ 不能把 node_id=0 拼成「ZLM 节点 0 不可用」：那不是故障描述，而是
// 「这条流压根没绑定过节点」（级联起流/绑定丢失/历史脏会话），照原样打出来
// 只会把人引向「查节点 0」这条死路。
func nodeUnavailableMessage(nodeID int64) string {
	if nodeID <= 0 {
		return "媒体流尚未绑定 ZLM 节点"
	}
	return fmt.Sprintf("ZLM 节点 %d 当前不可用", nodeID)
}

// logCloudRecordingState 让每次云录像状态变更都能在日志里对账。
// 这次排查失败案例时只能靠库里的孤儿会话倒推，就是因为这些分支全是静默写库。
func logCloudRecordingState(ctx context.Context, event, reason string, channelID uint, nodeID int64, streamID, state, message string) {
	if app.ZapLog == nil {
		return
	}
	fields := []zap.Field{
		zap.String("event", event),
		zap.String("reason", reason),
		zap.Uint("channel_id", channelID),
		zap.Int64("node_id", nodeID),
		zap.String("stream", streamID),
		zap.String("cloud_state", state),
	}
	if message != "" {
		fields = append(fields, zap.String("cloud_message", message))
	}
	logger := app.Log(ctx).Named("recording.cloud")
	if state == models.CloudRecordingStateFailed || state == models.CloudRecordingStateWaiting {
		logger.Warn("云录像状态: "+state, fields...)
		return
	}
	logger.Info("云录像状态: "+state, fields...)
}

// markWaiting 把「还没起录」的失败统一落到 waiting：等下一次点播/节点回归。
func (s *Service) markWaiting(ctx context.Context, channelID uint, reason, message string, nodeID int64, streamID string) (*models.GbChannel, error) {
	logCloudRecordingState(ctx, "recording.cloud.waiting", reason, channelID, nodeID, streamID, models.CloudRecordingStateWaiting, message)
	if _, err := s.repo.MarkState(ctx, channelID, true, models.CloudRecordingStateWaiting, message); err != nil {
		return nil, err
	}
	return s.repo.GetChannel(ctx, channelID)
}

// markNodeUnavailable 处理「录制中途节点没了」：会话确实结束了，直接标
// stopped（否则它会一直挂在未完成列表里被反复对账），通道按期望值落
// waiting/disabled。真正的 StopRecord 报错仍走 markStopFailure/markDisableFailure。
func (s *Service) markNodeUnavailable(ctx context.Context, channel *models.GbChannel, session *models.GbRecordingSession, reason string) (*models.GbChannel, error) {
	message := nodeUnavailableMessage(session.NodeID)
	session.State = models.RecordingSessionStateStopped
	session.LastError = message
	now := time.Now()
	session.StoppedAt = &now
	var sessionErr error
	if err := s.repo.UpsertSession(ctx, session); err != nil {
		sessionErr = fmt.Errorf("会话状态持久化失败: %w", err)
	}
	state := models.CloudRecordingStateDisabled
	if channel.CloudRecordingEnabled {
		state = models.CloudRecordingStateWaiting
	}
	logCloudRecordingState(ctx, "recording.cloud.node_unavailable", reason, channel.ID, session.NodeID, session.Stream, state, message)
	if _, markErr := s.repo.MarkState(ctx, channel.ID, channel.CloudRecordingEnabled, state, message); markErr != nil {
		return nil, errors.Join(markErr, sessionErr)
	}
	if sessionErr != nil {
		return nil, sessionErr
	}
	return s.repo.GetChannel(ctx, channel.ID)
}

func (s *Service) markStopFailure(ctx context.Context, channel *models.GbChannel, session *models.GbRecordingSession, cause error) (*models.GbChannel, error) {
	message := "停止录像失败: " + cause.Error()
	session.State = models.RecordingSessionStateFailed
	session.LastError = message
	var persistErr error
	if err := s.repo.UpsertSession(ctx, session); err != nil {
		persistErr = errors.Join(persistErr, err)
	}
	if _, err := s.repo.MarkState(ctx, channel.ID, channel.CloudRecordingEnabled, models.CloudRecordingStateFailed, message); err != nil {
		persistErr = errors.Join(persistErr, err)
	}
	logCloudRecordingState(ctx, "recording.cloud.failed", "stop_record_failed", channel.ID, session.NodeID, session.Stream, models.CloudRecordingStateFailed, message)
	channel, getErr := s.repo.GetChannel(ctx, channel.ID)
	return channel, errors.Join(fmt.Errorf("停止录像失败: %w", cause), persistErr, getErr)
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
	logCloudRecordingState(ctx, "recording.cloud.failed", "start_record_failed", channelID, session.NodeID, session.Stream, models.CloudRecordingStateFailed, message)
	return s.repo.GetChannel(ctx, channelID)
}

func (s *Service) markDisableFailure(ctx context.Context, channelID uint, session *models.GbRecordingSession, cause error) (*models.GbChannel, error) {
	message := "停止录像失败: " + cause.Error()
	session.State = models.RecordingSessionStateFailed
	session.LastError = message
	var persistErr error
	if err := s.repo.UpsertSession(ctx, session); err != nil {
		persistErr = errors.Join(persistErr, fmt.Errorf("会话状态持久化失败: %w", err))
	}
	if _, markErr := s.repo.MarkState(ctx, channelID, false, models.CloudRecordingStateFailed, message); markErr != nil {
		persistErr = errors.Join(persistErr, markErr)
	}
	logCloudRecordingState(ctx, "recording.cloud.failed", "stop_record_failed_on_disable", channelID, session.NodeID, session.Stream, models.CloudRecordingStateFailed, message)
	channel, getErr := s.repo.GetChannel(ctx, channelID)
	return channel, errors.Join(fmt.Errorf("停止录像失败: %w", cause), persistErr, getErr)
}
