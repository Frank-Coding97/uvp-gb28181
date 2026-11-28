package management

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
)

type audiencePlayerFake struct {
	mu      sync.Mutex
	players map[string][]zlm.MediaPlayer
	errors  map[string]error
	calls   []string
}

func (f *audiencePlayerFake) GetMediaPlayerList(_ context.Context, nodeID int64, target zlm.StreamTarget) ([]zlm.MediaPlayer, error) {
	key := audienceFakeKey(nodeID, target)
	f.mu.Lock()
	f.calls = append(f.calls, key)
	err := f.errors[key]
	players := append([]zlm.MediaPlayer(nil), f.players[key]...)
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return players, nil
}

func (f *audiencePlayerFake) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func audienceFakeKey(nodeID int64, target zlm.StreamTarget) string {
	return fmt.Sprintf("%d|%s|%s", nodeID, target.Schema, target.Stream)
}

func TestOverviewAudienceCountsOnePersonPerPeerIP(t *testing.T) {
	now := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	registry := overviewRegistryFake{nodes: []*node.Node{overviewNode(1, node.StateActive, now)}}
	runtime := &overviewRuntimeFake{statistics: map[int64]zlm.Statistic{1: {MediaSource: 4}}}
	media := &overviewMediaFake{media: map[int64][]zlm.MediaInfo{1: {
		{Online: true, Schema: "rtsp", VHost: "__defaultVhost__", App: "rtp", Stream: "camera-1", ReaderCount: 1},
		{Online: true, Schema: "rtsp", VHost: "__defaultVhost__", App: "rtp", Stream: "camera-2", ReaderCount: 1},
		{Online: true, Schema: "rtsp", VHost: "__defaultVhost__", App: "rtp", Stream: "camera-3", ReaderCount: 1},
		{Online: true, Schema: "rtsp", VHost: "__defaultVhost__", App: "rtp", Stream: "camera-4", ReaderCount: 1},
	}}}
	players := &audiencePlayerFake{players: map[string][]zlm.MediaPlayer{
		audienceFakeKey(1, zlm.StreamTarget{Schema: "rtsp", Stream: "camera-1"}): {{PeerIP: "10.1.1.7", PeerPort: 51001, Identifier: "conn-1", TypeID: "http-flv"}},
		audienceFakeKey(1, zlm.StreamTarget{Schema: "rtsp", Stream: "camera-2"}): {{PeerIP: "10.1.1.7", PeerPort: 51002, Identifier: "conn-2", TypeID: "http-flv"}},
		audienceFakeKey(1, zlm.StreamTarget{Schema: "rtsp", Stream: "camera-3"}): {{PeerIP: "10.1.1.7", PeerPort: 51003, Identifier: "conn-3", TypeID: "http-flv"}},
		audienceFakeKey(1, zlm.StreamTarget{Schema: "rtsp", Stream: "camera-4"}): {{PeerIP: "10.1.1.7", PeerPort: 51004, Identifier: "conn-4", TypeID: "http-flv"}},
	}}
	service := NewOverviewService(OverviewDependencies{Registry: registry, Runtime: runtime, Media: media, Players: players},
		WithOverviewClock(func() time.Time { return now }))

	result, err := service.GetOverview(context.Background())
	require.NoError(t, err)
	require.True(t, result.Audience.Available)
	require.Equal(t, 1, result.Audience.UniqueIPs, "一个人看四路只能算一位观众")
	require.Equal(t, 4, result.Audience.Connections)
	require.Len(t, result.Audience.Viewers, 1)
	require.Len(t, result.Audience.Viewers[0].Streams, 4, "明细里要能看出他看了四路")
	require.False(t, result.Audience.Partial)
}

func TestOverviewAudienceKeepsDifferentPeerIPsApart(t *testing.T) {
	now := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	registry := overviewRegistryFake{nodes: []*node.Node{overviewNode(1, node.StateActive, now)}}
	runtime := &overviewRuntimeFake{statistics: map[int64]zlm.Statistic{1: {MediaSource: 1}}}
	media := &overviewMediaFake{media: map[int64][]zlm.MediaInfo{1: {
		{Online: true, Schema: "rtsp", VHost: "__defaultVhost__", App: "rtp", Stream: "camera-1", ReaderCount: 2},
	}}}
	players := &audiencePlayerFake{players: map[string][]zlm.MediaPlayer{
		audienceFakeKey(1, zlm.StreamTarget{Schema: "rtsp", Stream: "camera-1"}): {
			{PeerIP: "10.1.1.7", PeerPort: 51001, Identifier: "conn-1"},
			{PeerIP: "10.1.1.8", PeerPort: 51001, Identifier: "conn-2"},
		},
	}}
	service := NewOverviewService(OverviewDependencies{Registry: registry, Runtime: runtime, Media: media, Players: players},
		WithOverviewClock(func() time.Time { return now }))

	result, err := service.GetOverview(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, result.Audience.UniqueIPs)
	require.Equal(t, 2, result.Audience.Connections)
}

func TestOverviewAudienceDeduplicatesSameSessionAcrossSchemas(t *testing.T) {
	now := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	registry := overviewRegistryFake{nodes: []*node.Node{overviewNode(1, node.StateActive, now)}}
	runtime := &overviewRuntimeFake{statistics: map[int64]zlm.Statistic{1: {MediaSource: 2}}}
	// 同一路流同时挂在 rtsp 与 rtp 两棵树上，播放器列表会重复返回同一条连接。
	media := &overviewMediaFake{media: map[int64][]zlm.MediaInfo{1: {
		{Online: true, Schema: "rtsp", VHost: "__defaultVhost__", App: "rtp", Stream: "camera-1", ReaderCount: 1},
		{Online: true, Schema: "rtp", VHost: "__defaultVhost__", App: "rtp", Stream: "camera-1", ReaderCount: 1},
	}}}
	players := &audiencePlayerFake{players: map[string][]zlm.MediaPlayer{
		audienceFakeKey(1, zlm.StreamTarget{Schema: "rtsp", Stream: "camera-1"}): {{PeerIP: "10.1.1.7", PeerPort: 51001, Identifier: "conn-1"}},
		audienceFakeKey(1, zlm.StreamTarget{Schema: "rtp", Stream: "camera-1"}):  {{PeerIP: "10.1.1.7", PeerPort: 51001, Identifier: "conn-1"}},
	}}
	service := NewOverviewService(OverviewDependencies{Registry: registry, Runtime: runtime, Media: media, Players: players},
		WithOverviewClock(func() time.Time { return now }))

	result, err := service.GetOverview(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, result.Audience.Connections, "同一会话不能因为挂在两个 schema 上被算两次")
	require.Len(t, result.Audience.Viewers[0].Streams, 1)
}

func TestOverviewAudienceSkipsStreamsWithoutReaders(t *testing.T) {
	now := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	registry := overviewRegistryFake{nodes: []*node.Node{overviewNode(1, node.StateActive, now)}}
	runtime := &overviewRuntimeFake{statistics: map[int64]zlm.Statistic{1: {MediaSource: 1}}}
	media := &overviewMediaFake{media: map[int64][]zlm.MediaInfo{1: {
		{Online: true, Schema: "rtsp", VHost: "__defaultVhost__", App: "rtp", Stream: "camera-1", ReaderCount: 0},
	}}}
	players := &audiencePlayerFake{}
	service := NewOverviewService(OverviewDependencies{Registry: registry, Runtime: runtime, Media: media, Players: players},
		WithOverviewClock(func() time.Time { return now }))

	result, err := service.GetOverview(context.Background())
	require.NoError(t, err)
	require.True(t, result.Audience.Available)
	require.Zero(t, result.Audience.UniqueIPs)
	require.Empty(t, players.called(), "没有读者的流不该发起播放器查询")
}

func TestOverviewAudienceReportsUnavailableWhenReaderIsMissing(t *testing.T) {
	now := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	registry := overviewRegistryFake{nodes: []*node.Node{overviewNode(1, node.StateActive, now)}}
	runtime := &overviewRuntimeFake{statistics: map[int64]zlm.Statistic{1: {MediaSource: 1}}}
	media := &overviewMediaFake{media: map[int64][]zlm.MediaInfo{1: {
		{Online: true, Schema: "rtsp", VHost: "__defaultVhost__", App: "rtp", Stream: "camera-1", ReaderCount: 1},
	}}}
	service := NewOverviewService(OverviewDependencies{Registry: registry, Runtime: runtime, Media: media},
		WithOverviewClock(func() time.Time { return now }))

	result, err := service.GetOverview(context.Background())
	require.NoError(t, err)
	require.False(t, result.Audience.Available, "未装配播放器读取能力时必须让前端回退，而不是报 0 人")
	require.Zero(t, result.Audience.UniqueIPs)
}

func TestOverviewAudienceMarksPartialWhenOneStreamFails(t *testing.T) {
	now := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	registry := overviewRegistryFake{nodes: []*node.Node{overviewNode(1, node.StateActive, now)}}
	runtime := &overviewRuntimeFake{statistics: map[int64]zlm.Statistic{1: {MediaSource: 2}}}
	media := &overviewMediaFake{media: map[int64][]zlm.MediaInfo{1: {
		{Online: true, Schema: "rtsp", VHost: "__defaultVhost__", App: "rtp", Stream: "camera-1", ReaderCount: 1},
		{Online: true, Schema: "rtsp", VHost: "__defaultVhost__", App: "rtp", Stream: "camera-2", ReaderCount: 1},
	}}}
	players := &audiencePlayerFake{
		players: map[string][]zlm.MediaPlayer{
			audienceFakeKey(1, zlm.StreamTarget{Schema: "rtsp", Stream: "camera-1"}): {{PeerIP: "10.1.1.7", PeerPort: 51001, Identifier: "conn-1"}},
		},
		errors: map[string]error{
			audienceFakeKey(1, zlm.StreamTarget{Schema: "rtsp", Stream: "camera-2"}): errors.New("zlm timeout"),
		},
	}
	service := NewOverviewService(OverviewDependencies{Registry: registry, Runtime: runtime, Media: media, Players: players},
		WithOverviewClock(func() time.Time { return now }))

	result, err := service.GetOverview(context.Background())
	require.NoError(t, err)
	require.True(t, result.Audience.Available)
	require.True(t, result.Audience.Partial)
	require.Equal(t, 1, result.Audience.UniqueIPs)
	require.Len(t, result.Audience.Errors, 1)
	require.EqualValues(t, 1, result.Audience.Errors[0].NodeID)
}

func TestOverviewAudienceReusesShortLivedSnapshot(t *testing.T) {
	now := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	registry := overviewRegistryFake{nodes: []*node.Node{overviewNode(1, node.StateActive, now)}}
	runtime := &overviewRuntimeFake{statistics: map[int64]zlm.Statistic{1: {MediaSource: 1}}}
	media := &overviewMediaFake{media: map[int64][]zlm.MediaInfo{1: {
		{Online: true, Schema: "rtsp", VHost: "__defaultVhost__", App: "rtp", Stream: "camera-1", ReaderCount: 1},
	}}}
	players := &audiencePlayerFake{players: map[string][]zlm.MediaPlayer{
		audienceFakeKey(1, zlm.StreamTarget{Schema: "rtsp", Stream: "camera-1"}): {{PeerIP: "10.1.1.7", PeerPort: 51001, Identifier: "conn-1"}},
	}}
	service := NewOverviewService(OverviewDependencies{Registry: registry, Runtime: runtime, Media: media, Players: players},
		WithOverviewClock(func() time.Time { return now }))

	first, err := service.GetOverview(context.Background())
	require.NoError(t, err)
	callsAfterFirst := len(players.called())
	second, err := service.GetOverview(context.Background())
	require.NoError(t, err)

	require.Equal(t, callsAfterFirst, len(players.called()), "3 秒短缓存内不应重复采集播放器列表")
	require.Equal(t, first.Audience.UniqueIPs, second.Audience.UniqueIPs)
}

func TestOverviewAudienceTreatsVanishedMediaAsEmpty(t *testing.T) {
	now := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	registry := overviewRegistryFake{nodes: []*node.Node{overviewNode(1, node.StateActive, now)}}
	runtime := &overviewRuntimeFake{statistics: map[int64]zlm.Statistic{1: {MediaSource: 1}}}
	media := &overviewMediaFake{media: map[int64][]zlm.MediaInfo{1: {
		{Online: true, Schema: "rtsp", VHost: "__defaultVhost__", App: "rtp", Stream: "camera-1", ReaderCount: 1},
	}}}
	players := &audiencePlayerFake{errors: map[string]error{
		audienceFakeKey(1, zlm.StreamTarget{Schema: "rtsp", Stream: "camera-1"}): zlm.ErrMediaNotFound,
	}}
	service := NewOverviewService(OverviewDependencies{Registry: registry, Runtime: runtime, Media: media, Players: players},
		WithOverviewClock(func() time.Time { return now }))

	result, err := service.GetOverview(context.Background())
	require.NoError(t, err)
	require.True(t, result.Audience.Available)
	require.False(t, result.Audience.Partial, "流在采样间隙结束不算失败")
	require.Zero(t, result.Audience.UniqueIPs)
}
