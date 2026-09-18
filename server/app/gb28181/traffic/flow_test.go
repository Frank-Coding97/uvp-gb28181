package traffic

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/handler"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
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
