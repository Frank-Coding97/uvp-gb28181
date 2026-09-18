package traffic

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type samplerNodes struct{ nodes []*node.Node }

func (n samplerNodes) ListActive() []*node.Node { return n.nodes }

type samplerClient struct {
	calls *atomic.Int32
	items []zlm.MediaInfo
	err   error
}

func (c samplerClient) GetMediaList(context.Context, string, string, string) ([]zlm.MediaInfo, error) {
	c.calls.Add(1)
	return c.items, c.err
}

func TestSamplerOpensGapAfterTwoFailuresAndClosesOnRecovery(t *testing.T) {
	repo := newTrafficTestRepo(t)
	resolver := NewAttributionResolver()
	resolver.RegisterLive(LiveBinding{NodeID: 7, StreamID: "stream-1", Generation: 1, DeviceCode: "device-1", ChannelCode: "channel-1"})
	var calls atomic.Int32
	client := &samplerClient{calls: &calls, err: errors.New("zlm unavailable")}
	now := time.Unix(3000, 0)
	sampler := NewSampler(samplerNodes{nodes: []*node.Node{{ID: 7, MediaServerUUID: "ms-1"}}}, func(*node.Node) SamplerMediaClient {
		return client
	}, repo, resolver, nil, func() time.Time { return now })

	require.Error(t, sampler.SampleOnce(context.Background()))
	var count int64
	require.NoError(t, repo.db.Model(&gbmodels.GbDeviceTrafficGap{}).Count(&count).Error)
	require.Zero(t, count)
	require.Error(t, sampler.SampleOnce(context.Background()))
	require.NoError(t, repo.db.Model(&gbmodels.GbDeviceTrafficGap{}).Where("state = ?", "open").Count(&count).Error)
	require.EqualValues(t, 1, count)

	client.err = nil
	client.items = []zlm.MediaInfo{{App: "rtp", Stream: "stream-1", Schema: "rtsp", TotalBytes: 1}}
	require.NoError(t, sampler.SampleOnce(context.Background()))
	require.NoError(t, repo.db.Model(&gbmodels.GbDeviceTrafficGap{}).Where("state = ?", "closed").Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestSamplerCallsEachNodeOnceAndDeduplicatesSchemas(t *testing.T) {
	repo := newTrafficTestRepo(t)
	resolver := NewAttributionResolver()
	resolver.RegisterLive(LiveBinding{NodeID: 7, StreamID: "stream-1", Generation: 1, DeviceCode: "device-1", ChannelCode: "channel-1"})
	resolver.RegisterLive(LiveBinding{NodeID: 8, StreamID: "stream-2", Generation: 1, DeviceCode: "device-2", ChannelCode: "channel-2"})
	var calls atomic.Int32
	items := map[int64][]zlm.MediaInfo{
		7: {
			{Online: true, Schema: "rtsp", VHost: "__defaultVhost__", App: "rtp", Stream: "stream-1", CreateStamp: 10, TotalBytes: 100, BytesSpeed: 4, ReaderCount: 1},
			{Online: true, Schema: "ws", VHost: "__defaultVhost__", App: "rtp", Stream: "stream-1", CreateStamp: 10, TotalBytes: 120, BytesSpeed: 5, ReaderCount: 2},
		},
		8: {{Online: true, Schema: "rtsp", VHost: "__defaultVhost__", App: "rtp", Stream: "stream-2", CreateStamp: 11, TotalBytes: 50}},
	}
	store := NewRealtimeStore(2 * time.Minute)
	sampler := NewSampler(samplerNodes{nodes: []*node.Node{{ID: 7, MediaServerUUID: "ms-7"}, {ID: 8, MediaServerUUID: "ms-8"}}}, func(n *node.Node) SamplerMediaClient {
		return samplerClient{calls: &calls, items: items[n.ID]}
	}, repo, resolver, store, func() time.Time { return time.Unix(3000, 0) })

	require.NoError(t, sampler.SampleOnce(context.Background()))
	require.EqualValues(t, 2, calls.Load())
	var rows []gbmodels.GbDeviceTrafficDaily
	require.NoError(t, repo.db.Order("device_code").Find(&rows).Error)
	require.Len(t, rows, 2)
	require.EqualValues(t, 120, rows[0].UpstreamBytes)
	snapshot, ok := store.Get("channel-1", time.Unix(3001, 0))
	require.True(t, ok)
	require.Equal(t, 3, snapshot.ReaderCount)
	require.EqualValues(t, 15, snapshot.EstimatedDownstreamBytesPerSec)
}
