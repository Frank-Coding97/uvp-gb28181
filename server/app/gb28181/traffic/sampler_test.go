package traffic

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
)

type samplerNodes struct{ nodes []*node.Node }

func (n samplerNodes) ListActive() []*node.Node { return n.nodes }

type samplerClient struct {
	calls   *atomic.Int32
	items   []zlm.MediaInfo
	err     error
	players map[string][]zlm.MediaPlayer
}

func (c samplerClient) GetMediaList(context.Context, string, string, string) ([]zlm.MediaInfo, error) {
	c.calls.Add(1)
	return c.items, c.err
}

func (c samplerClient) GetMediaPlayerList(_ context.Context, schema, _, _, _ string) ([]zlm.MediaPlayer, error) {
	return c.players[schema], nil
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

// 回归守卫(2026-10-18):noteSuccess 曾经只在「内存里累计过 2 次失败」时才关缺口,
// 而 failures 只活在进程内存 ⇒ 后端重启后,重启前留下的 open 缺口永远关不上
// (实测 gap#46 从 14:34 一直挂到收盘,首页流量图因此永久显示「部分数据」)。
// 现在改成每次成功都无条件 CloseGap(带 state='open' 守卫,空更新也安全)。
func TestSamplerClosesResidualGapFromPreviousProcessOnFirstSuccess(t *testing.T) {
	repo := newTrafficTestRepo(t)
	resolver := NewAttributionResolver()
	resolver.RegisterLive(LiveBinding{NodeID: 7, StreamID: "stream-1", Generation: 1, DeviceCode: "device-1", ChannelCode: "channel-1"})
	// 模拟「上一个进程」留下的缺口:本进程的 failures 计数是空的
	require.NoError(t, repo.db.Create(&gbmodels.GbDeviceTrafficGap{
		NodeID: 7, Reason: "api_failed", State: "open", StartedAt: time.Unix(2900, 0).UTC(),
	}).Error)
	var calls atomic.Int32
	client := &samplerClient{
		calls: &calls,
		items: []zlm.MediaInfo{{App: "rtp", Stream: "stream-1", Schema: "rtsp", TotalBytes: 1}},
	}
	sampler := NewSampler(samplerNodes{nodes: []*node.Node{{ID: 7, MediaServerUUID: "ms-1"}}}, func(*node.Node) SamplerMediaClient {
		return client
	}, repo, resolver, nil, func() time.Time { return time.Unix(3000, 0) })

	require.NoError(t, sampler.SampleOnce(context.Background()))
	var gap gbmodels.GbDeviceTrafficGap
	require.NoError(t, repo.db.Where("node_id = ? AND reason = ?", 7, "api_failed").First(&gap).Error)
	require.Equal(t, "closed", gap.State, "重启前的残留缺口必须在首次成功后关闭")
	require.NotNil(t, gap.EndedAt)
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

func TestSamplerSamplesDownstreamPlayersIncrementally(t *testing.T) {
	repo := newTrafficTestRepo(t)
	resolver := NewAttributionResolver()
	resolver.RegisterLive(LiveBinding{NodeID: 7, StreamID: "stream-1", Generation: 1, DeviceCode: "device-1", ChannelCode: "channel-1"})
	bytesOf := func(v uint64) *uint64 { return &v }
	client := &samplerClient{
		calls: &atomic.Int32{},
		items: []zlm.MediaInfo{{Online: true, Schema: "ws", VHost: "__defaultVhost__", App: "rtp", Stream: "stream-1", CreateStamp: 10, TotalBytes: 1000, BytesSpeed: 8, ReaderCount: 1}},
		players: map[string][]zlm.MediaPlayer{
			"ws": {{Identifier: "sid-1", TotalBytes: bytesOf(100)}},
		},
	}
	store := NewRealtimeStore(2 * time.Minute)
	now := time.Unix(3000, 0)
	sampler := NewSampler(samplerNodes{nodes: []*node.Node{{ID: 7, MediaServerUUID: "ms-1"}}}, func(*node.Node) SamplerMediaClient {
		return client
	}, repo, resolver, store, func() time.Time { return now })

	require.NoError(t, sampler.SampleOnce(context.Background()))
	// 第二轮:同一连接累计涨到 260,只应入账增量 160
	client.players["ws"][0].TotalBytes = bytesOf(260)
	now = now.Add(time.Minute)
	require.NoError(t, sampler.SampleOnce(context.Background()))

	var session gbmodels.GbDeviceTrafficSession
	require.NoError(t, repo.db.Where("direction = ?", DirectionDownstream).First(&session).Error)
	require.Equal(t, downstreamBusinessKey(7, "sid-1"), session.BusinessKey, "下行采样必须与 flow_report 收尾共用同一个会话键")
	require.Equal(t, string(SessionActive), session.State)
	require.Equal(t, "sid-1", session.ZLMSessionID)
	require.EqualValues(t, 260, session.LastTotalBytes)

	var daily gbmodels.GbDeviceTrafficDaily
	require.NoError(t, repo.db.First(&daily).Error)
	require.EqualValues(t, 260, daily.DownstreamBytes, "下行应边跑边记,累计值=260 而不是断开时才一次性入账")
	require.Zero(t, daily.DownstreamSessions, "未结算的连接不记会话数")
	require.EqualValues(t, 1000, daily.UpstreamBytes)

	snapshot, ok := store.Get("channel-1", now)
	require.True(t, ok)
	require.True(t, snapshot.DownstreamMeasured, "节点支持实时下行计数时应标记为实测")
}

func TestSamplerSkipsPlayersWithoutAliveCounter(t *testing.T) {
	// 旧版 ZLM / HLS 切片会话不返回 totalBytes:必须完全不产生下行入账,
	// 实时快照回退为估算口径 —— 否则 HLS 场景会与 HlsMediaSource 按 cookie
	// 聚合上报的 flow_report 重复计数
	repo := newTrafficTestRepo(t)
	resolver := NewAttributionResolver()
	resolver.RegisterLive(LiveBinding{NodeID: 7, StreamID: "stream-1", Generation: 1, DeviceCode: "device-1", ChannelCode: "channel-1"})
	client := &samplerClient{
		calls: &atomic.Int32{},
		items: []zlm.MediaInfo{{Online: true, Schema: "hls", VHost: "__defaultVhost__", App: "rtp", Stream: "stream-1", CreateStamp: 10, TotalBytes: 1000, BytesSpeed: 8, ReaderCount: 2}},
		players: map[string][]zlm.MediaPlayer{
			"hls": {{Identifier: "cookie-1"}, {Identifier: ""}},
		},
	}
	store := NewRealtimeStore(2 * time.Minute)
	now := time.Unix(3000, 0)
	sampler := NewSampler(samplerNodes{nodes: []*node.Node{{ID: 7, MediaServerUUID: "ms-1"}}}, func(*node.Node) SamplerMediaClient {
		return client
	}, repo, resolver, store, func() time.Time { return now })

	require.NoError(t, sampler.SampleOnce(context.Background()))
	var count int64
	require.NoError(t, repo.db.Model(&gbmodels.GbDeviceTrafficSession{}).Where("direction = ?", DirectionDownstream).Count(&count).Error)
	require.Zero(t, count, "不返回 totalBytes 的会话类型不允许产生下行采样入账")
	snapshot, ok := store.Get("channel-1", now)
	require.True(t, ok)
	require.False(t, snapshot.DownstreamMeasured)
	require.EqualValues(t, 16, snapshot.EstimatedDownstreamBytesPerSec, "不支持实测时必须保留估算回退")
}
