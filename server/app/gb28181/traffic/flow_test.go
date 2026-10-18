package traffic

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/handler"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

type flowNodeResolver struct{ id int64 }

func (r flowNodeResolver) IDForUUID(uuid string) (int64, bool) { return r.id, uuid == "ms-1" }

func TestFlowServiceFinalUpstreamReusesActiveSampleAndAddsTail(t *testing.T) {
	repo := newTrafficTestRepo(t)
	resolver := NewAttributionResolver()
	resolver.RegisterLive(LiveBinding{NodeID: 7, StreamID: "stream-1", Generation: 1, DeviceCode: "device-1", ChannelCode: "channel-1"})
	baseAt := time.Unix(1000, 0)
	_, err := repo.Apply(context.Background(), ApplyRequest{
		BusinessKey: "sample:7:stream-1:1", NodeID: 7, Direction: DirectionUpstream,
		DeviceCode: "device-1", ChannelCode: "channel-1", App: "rtp", Stream: "stream-1", AbsoluteBytes: 130, At: baseAt,
	})
	require.NoError(t, err)

	service := NewFlowService(repo, resolver, flowNodeResolver{id: 7}, func() time.Time { return baseAt.Add(time.Minute) })
	require.NoError(t, service.CollectFlow(context.Background(), handler.FlowReport{
		ID: "zlm-1", MediaServerID: "ms-1", Schema: "rtsp", VHost: "__defaultVhost__", App: "rtp", Stream: "stream-1", TotalBytes: 134, Duration: 60,
	}))

	var daily gbmodels.GbDeviceTrafficDaily
	require.NoError(t, repo.db.First(&daily).Error)
	require.EqualValues(t, 134, daily.UpstreamBytes)
	require.EqualValues(t, 1, daily.UpstreamSessions)
}

func TestFlowServiceSettlesEachPlayerAsDownstream(t *testing.T) {
	repo := newTrafficTestRepo(t)
	resolver := NewAttributionResolver()
	resolver.RegisterLive(LiveBinding{NodeID: 7, StreamID: "stream-1", Generation: 1, DeviceCode: "device-1", ChannelCode: "channel-1"})
	service := NewFlowService(repo, resolver, flowNodeResolver{id: 7}, func() time.Time { return time.Unix(2000, 0) })

	for _, report := range []handler.FlowReport{
		{ID: "player-a", MediaServerID: "ms-1", App: "rtp", Stream: "stream-1", Player: true, TotalBytes: 10, Duration: 1},
		{ID: "player-b", MediaServerID: "ms-1", App: "rtp", Stream: "stream-1", Player: true, TotalBytes: 20, Duration: 2},
	} {
		require.NoError(t, service.CollectFlow(context.Background(), report))
	}
	var daily gbmodels.GbDeviceTrafficDaily
	require.NoError(t, repo.db.First(&daily).Error)
	require.EqualValues(t, 30, daily.DownstreamBytes)
	require.EqualValues(t, 2, daily.DownstreamSessions)
}

func TestFlowServiceSettleReusesSampledDownstreamSession(t *testing.T) {
	// 观看连接存活期间采样器已按 downstreamBusinessKey 增量入账 200;
	// 断开时 flow_report 报最终值 260,必须复用同一条会话只补尾差 60,
	// 落账总量 = 260 而不是 460
	repo := newTrafficTestRepo(t)
	resolver := NewAttributionResolver()
	resolver.RegisterLive(LiveBinding{NodeID: 7, StreamID: "stream-1", Generation: 1, DeviceCode: "device-1", ChannelCode: "channel-1"})
	baseAt := time.Unix(1000, 0)
	_, err := repo.Apply(context.Background(), ApplyRequest{
		BusinessKey: downstreamBusinessKey(7, "sid-1"), NodeID: 7, ZLMSessionID: "sid-1", Direction: DirectionDownstream,
		DeviceCode: "device-1", ChannelCode: "channel-1", App: "rtp", Stream: "stream-1", AbsoluteBytes: 200, At: baseAt,
	})
	require.NoError(t, err)

	service := NewFlowService(repo, resolver, flowNodeResolver{id: 7}, func() time.Time { return baseAt.Add(time.Minute) })
	report := handler.FlowReport{ID: "sid-1", MediaServerID: "ms-1", App: "rtp", Stream: "stream-1", Player: true, TotalBytes: 260, Duration: 30}
	require.NoError(t, service.CollectFlow(context.Background(), report))

	var daily gbmodels.GbDeviceTrafficDaily
	require.NoError(t, repo.db.First(&daily).Error)
	require.EqualValues(t, 260, daily.DownstreamBytes, "采样累计 + 尾差结算必须等于最终值,不允许重复计数")
	require.EqualValues(t, 1, daily.DownstreamSessions)

	var session gbmodels.GbDeviceTrafficSession
	require.NoError(t, repo.db.Where("business_key = ?", downstreamBusinessKey(7, "sid-1")).First(&session).Error)
	require.Equal(t, string(SessionSettled), session.State)
	require.EqualValues(t, 260, session.SettledTotalBytes)
	require.EqualValues(t, 30, session.DurationSeconds)

	// 同一报告重复投递(hook 重试)不得重复计数
	require.NoError(t, service.CollectFlow(context.Background(), report))
	require.NoError(t, repo.db.First(&daily).Error)
	require.EqualValues(t, 260, daily.DownstreamBytes)
	require.EqualValues(t, 1, daily.DownstreamSessions)
}
