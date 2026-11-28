package recording

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// 「节点不在 registry」是媒体面暂时够不着，不是录像失败。
// 判 failed 会把通道钉死到必须人工重开云录像 —— 实测级联起流场景就踩在这里。
func TestBeginPlaybackWaitsWhenNodeMissingFromRegistry(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	channel.StreamID = "stream-1"
	require.NoError(t, repo.db.Model(channel).Update("stream_id", channel.StreamID).Error)
	client := &fakeRecorderClient{}
	// 绑定说在 2 号节点，但 registry 里没有它（节点刚下线/重启）。
	service := newRecordingService(repo, client, fakeLocation{nodeID: 2, ok: true}, fakeRegistry{})

	_, err := service.Enable(context.Background(), channel.ID)
	require.NoError(t, err)
	require.NoError(t, service.BeginPlayback(context.Background(), channel.StreamID))

	got, err := repo.GetChannel(context.Background(), channel.ID)
	require.NoError(t, err)
	require.Equal(t, models.CloudRecordingStateWaiting, got.CloudRecordingState)
	require.Contains(t, got.CloudRecordingError, "ZLM 节点 2 当前不可用")
	require.Zero(t, client.startCalls.Load())
}

// 回归：node_id=0 的绑定曾被当成有效绑定，于是写出一条 node_id=0 的空壳
// 会话（channel_id=0 且 stream 为空），下一次停录报「ZLM 节点 0 不可用」。
func TestBeginPlaybackNeverPersistsZeroNodeSession(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	channel.StreamID = "stream-1"
	require.NoError(t, repo.db.Model(channel).Update("stream_id", channel.StreamID).Error)
	service := newRecordingService(repo, &fakeRecorderClient{}, fakeLocation{nodeID: 0, ok: true}, fakeRegistry{})

	_, err := service.Enable(context.Background(), channel.ID)
	require.NoError(t, err)
	require.NoError(t, service.BeginPlayback(context.Background(), channel.StreamID))

	got, err := repo.GetChannel(context.Background(), channel.ID)
	require.NoError(t, err)
	require.Equal(t, models.CloudRecordingStateWaiting, got.CloudRecordingState)
	require.Equal(t, "媒体流尚未绑定 ZLM 节点", got.CloudRecordingError)

	var sessions []models.GbRecordingSession
	require.NoError(t, repo.db.Find(&sessions).Error)
	require.Empty(t, sessions, "不能为未知节点写会话，否则每轮对账都会捞到空壳记录")
}

// 流结束时会话确实终止了，但通道要停在 waiting 等下一次点播。
func TestEndPlaybackOnMissingNodeKeepsChannelWaiting(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	channel.StreamID = "stream-1"
	require.NoError(t, repo.db.Model(channel).Update("stream_id", channel.StreamID).Error)
	_, err := repo.SetDesired(context.Background(), channel.ID, true)
	require.NoError(t, err)
	session := &models.GbRecordingSession{
		ChannelID: channel.ID, DeviceID: channel.DeviceID, NodeID: 2,
		VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp,
		Stream: "stream-1", State: models.RecordingSessionStateRecording,
	}
	require.NoError(t, repo.UpsertSession(context.Background(), session))
	service := newRecordingService(repo, &fakeRecorderClient{}, fakeLocation{}, fakeRegistry{})

	require.NoError(t, service.EndPlayback(context.Background(), "stream-1"))

	got, err := repo.GetChannel(context.Background(), channel.ID)
	require.NoError(t, err)
	require.True(t, got.CloudRecordingEnabled)
	require.Equal(t, models.CloudRecordingStateWaiting, got.CloudRecordingState)
	require.Contains(t, got.CloudRecordingError, "ZLM 节点 2 当前不可用")

	stored, err := repo.FindSessionByID(context.Background(), session.ID)
	require.NoError(t, err)
	require.Equal(t, models.RecordingSessionStateStopped, stored.State)
	require.Contains(t, stored.LastError, "ZLM 节点 2 当前不可用")
}

// 用户主动关云录像时节点刚好不可用：期望值是「关掉了」，不是「失败」。
func TestDisableOnMissingNodeFallsBackToDisabled(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	_, err := repo.SetDesired(context.Background(), channel.ID, true)
	require.NoError(t, err)
	require.NoError(t, repo.UpsertSession(context.Background(), &models.GbRecordingSession{
		ChannelID: channel.ID, DeviceID: channel.DeviceID, NodeID: 2,
		VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp,
		Stream: "stream-1", State: models.RecordingSessionStateRecording,
	}))
	service := newRecordingService(repo, &fakeRecorderClient{}, fakeLocation{}, fakeRegistry{})

	got, err := service.Disable(context.Background(), channel.ID)
	require.NoError(t, err)
	require.False(t, got.CloudRecordingEnabled)
	require.Equal(t, models.CloudRecordingStateDisabled, got.CloudRecordingState)
	require.Contains(t, got.CloudRecordingError, "ZLM 节点 2 当前不可用")
}

// failed 是终态，不能当「未完成」每轮对账重复捞回来。
func TestListUnfinishedSessionsSkipsFailed(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	active := &models.GbRecordingSession{
		ChannelID: channel.ID, DeviceID: channel.DeviceID, NodeID: 2,
		VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp,
		Stream: "active", State: models.RecordingSessionStateRecording,
	}
	require.NoError(t, repo.UpsertSession(context.Background(), active))
	failed := &models.GbRecordingSession{
		ChannelID: channel.ID, DeviceID: channel.DeviceID, NodeID: 2,
		VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp,
		Stream: "failed", State: models.RecordingSessionStateFailed,
	}
	require.NoError(t, repo.UpsertSession(context.Background(), failed))
	stopped := &models.GbRecordingSession{
		ChannelID: channel.ID, DeviceID: channel.DeviceID, NodeID: 2,
		VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp,
		Stream: "stopped", State: models.RecordingSessionStateStopped,
	}
	require.NoError(t, repo.UpsertSession(context.Background(), stopped))

	unfinished, err := repo.ListUnfinishedSessions(context.Background())
	require.NoError(t, err)
	require.Len(t, unfinished, 1)
	require.Equal(t, "active", unfinished[0].Stream)
}
