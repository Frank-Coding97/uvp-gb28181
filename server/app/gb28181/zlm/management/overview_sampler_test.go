package management

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type overviewSamplerCacheFake struct {
	app.CacheInterf
	mu     sync.Mutex
	values map[string]string
	ttls   map[string]time.Duration
}

func newOverviewSamplerCacheFake() *overviewSamplerCacheFake {
	return &overviewSamplerCacheFake{values: make(map[string]string), ttls: make(map[string]time.Duration)}
}

func (cache *overviewSamplerCacheFake) Set(_ context.Context, key, value string, ttl time.Duration) error {
	cache.mu.Lock()
	cache.values[key] = value
	cache.ttls[key] = ttl
	cache.mu.Unlock()
	return nil
}

func (cache *overviewSamplerCacheFake) Get(_ context.Context, key string) (string, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	value, ok := cache.values[key]
	if !ok {
		return "", app.ErrKeyNotFound
	}
	return value, nil
}

type overviewSamplerSourceFake struct {
	mu     sync.Mutex
	result OverviewResult
	calls  int
}

func (source *overviewSamplerSourceFake) GetOverview(context.Context) (OverviewResult, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.calls++
	return source.result, nil
}

func (source *overviewSamplerSourceFake) GetNodeRuntime(_ context.Context, nodeID int64) (NodeRuntimeView, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	for _, runtime := range source.result.Nodes {
		if runtime.NodeID == nodeID {
			return runtime, nil
		}
	}
	return NodeRuntimeView{NodeID: nodeID}, nil
}

func (source *overviewSamplerSourceFake) ListStreams(context.Context, StreamFilter, PageRequest) (StreamDistribution, error) {
	return StreamDistribution{}, nil
}

func nodeRateOverview(at time.Time, nodes ...NodeRuntimeView) OverviewResult {
	result := exactRateOverview(at, 0, 0)
	result.Metrics.SampledNodeCount = int64(len(nodes))
	result.Metrics.MediaTrafficSampledNodes = 0
	result.Nodes = nodes
	for _, runtime := range nodes {
		if !runtime.Metrics.MediaTrafficAvailable {
			continue
		}
		result.Metrics.MediaTrafficSampledNodes++
		result.Metrics.UpstreamBytesPerSecond += runtime.Metrics.UpstreamBytesPerSecond
		result.Metrics.DownstreamBytesPerSecond += runtime.Metrics.DownstreamBytesPerSecond
	}
	return result
}

func nodeRateRuntime(nodeID int64, upstream, downstream uint64, available bool) NodeRuntimeView {
	return NodeRuntimeView{
		NodeID: nodeID,
		Metrics: NodeRuntimeMetrics{
			UpstreamBytesPerSecond:   upstream,
			DownstreamBytesPerSecond: downstream,
			MediaTrafficAvailable:    available,
		},
	}
}

func nodeTrendRuntime(nodeID int64, upstream, downstream uint64, streams, viewers int, throughput uint64, sessions int) NodeRuntimeView {
	runtime := nodeRateRuntime(nodeID, upstream, downstream, true)
	runtime.MediaFreshness = RuntimeFreshnessFresh
	runtime.MetricsComplete = true
	runtime.Metrics.NetworkSessionCount = sessions
	runtime.Metrics.ObjectStatistics = RuntimeObjectStatistics{MediaSource: uint64(streams), Socket: uint64(sessions)}
	runtime.Streams = make([]RuntimeMedia, streams)
	for index := range runtime.Streams {
		runtime.Streams[index].BytesSpeed = throughput / uint64(streams)
		runtime.Streams[index].ReaderCount = viewers / streams
	}
	return runtime
}

func (source *overviewSamplerSourceFake) set(result OverviewResult) {
	source.mu.Lock()
	source.result = result
	source.mu.Unlock()
}

func exactRateOverview(at time.Time, upstream, downstream uint64) OverviewResult {
	return OverviewResult{
		AsOf: at,
		Metrics: OverviewMetrics{
			SampledNodeCount:         2,
			MediaTrafficSampledNodes: 2,
			UpstreamBytesPerSecond:   upstream,
			DownstreamBytesPerSecond: downstream,
		},
	}
}

func TestOverviewSamplerPersistsAndReturnsSharedFiveMinuteHistory(t *testing.T) {
	ctx := context.Background()
	cache := newOverviewSamplerCacheFake()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	source := &overviewSamplerSourceFake{result: exactRateOverview(now, 1024, 2048)}
	sampler := NewOverviewSampler(source, cache, WithOverviewSamplerClock(func() time.Time { return now }))

	require.NoError(t, sampler.SampleOnce(ctx))
	now = now.Add(5 * time.Second)
	source.set(exactRateOverview(now, 4096, 8192))
	require.NoError(t, sampler.SampleOnce(ctx))

	result, err := sampler.GetOverview(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, source.calls, "读取已采样快照不应再次请求 ZLM")
	require.EqualValues(t, 4096, result.Metrics.UpstreamBytesPerSecond)
	require.Equal(t, []MediaRateSample{
		{SampledAt: now.Add(-5 * time.Second).UnixMilli(), Upstream: 1024, Downstream: 2048},
		{SampledAt: now.UnixMilli(), Upstream: 4096, Downstream: 8192},
	}, result.MediaRateSamples)

	// 新采样器模拟进程重启，历史应能从共享缓存恢复，而不是从浏览器重新积累。
	restarted := NewOverviewSampler(source, cache, WithOverviewSamplerClock(func() time.Time { return now }))
	require.NoError(t, restarted.loadHistory(ctx))
	require.Equal(t, result.MediaRateSamples, restarted.historySnapshot())
}

func TestOverviewSamplerSkipsInexactSamplesAndPrunesExpiredHistory(t *testing.T) {
	ctx := context.Background()
	cache := newOverviewSamplerCacheFake()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	source := &overviewSamplerSourceFake{result: exactRateOverview(now, 100, 200)}
	sampler := NewOverviewSampler(source, cache, WithOverviewSamplerClock(func() time.Time { return now }))
	require.NoError(t, sampler.SampleOnce(ctx))

	now = now.Add(4*time.Minute + 59*time.Second)
	source.set(OverviewResult{AsOf: now, Metrics: OverviewMetrics{SampledNodeCount: 2, MediaTrafficSampledNodes: 1, UpstreamBytesPerSecond: 999}})
	require.NoError(t, sampler.SampleOnce(ctx))
	require.Len(t, sampler.historySnapshot(), 1)

	now = now.Add(2 * time.Second)
	source.set(exactRateOverview(now, 300, 400))
	require.NoError(t, sampler.SampleOnce(ctx))
	require.Equal(t, []MediaRateSample{{SampledAt: now.UnixMilli(), Upstream: 300, Downstream: 400}}, sampler.historySnapshot())
}

func TestOverviewSamplerPersistsNodeMediaRateHistoryInRedis(t *testing.T) {
	ctx := context.Background()
	cache := newOverviewSamplerCacheFake()
	now := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	source := &overviewSamplerSourceFake{result: nodeRateOverview(now,
		nodeTrendRuntime(11, 1024, 2048, 2, 4, 6000, 7),
		nodeRateRuntime(22, 4096, 8192, true),
	)}
	sampler := NewOverviewSampler(source, cache, WithOverviewSamplerClock(func() time.Time { return now }))

	require.NoError(t, sampler.SampleOnce(ctx))
	now = now.Add(DefaultMediaRateSampleInterval)
	source.set(nodeRateOverview(now,
		nodeTrendRuntime(11, 3072, 6144, 3, 6, 9000, 8),
		nodeRateRuntime(22, 5120, 10240, true),
	))
	require.NoError(t, sampler.SampleOnce(ctx))

	node11, err := sampler.GetNodeRuntime(ctx, 11)
	require.NoError(t, err)
	require.Equal(t, []MediaRateSample{
		{SampledAt: now.Add(-DefaultMediaRateSampleInterval).UnixMilli(), Upstream: 1024, Downstream: 2048},
		{SampledAt: now.UnixMilli(), Upstream: 3072, Downstream: 6144},
	}, node11.MediaRateSamples)
	require.Len(t, node11.TrendSamples, 2)
	require.EqualValues(t, 3, *node11.TrendSamples[1].StreamCount)
	require.EqualValues(t, 6, *node11.TrendSamples[1].ViewerCount)
	require.EqualValues(t, 9000, *node11.TrendSamples[1].Throughput)
	require.EqualValues(t, 8, *node11.TrendSamples[1].SessionCount)
	require.EqualValues(t, 3, node11.TrendSamples[1].ObjectStatistics.MediaSource)
	node22, err := sampler.GetNodeRuntime(ctx, 22)
	require.NoError(t, err)
	require.Equal(t, []MediaRateSample{
		{SampledAt: now.Add(-DefaultMediaRateSampleInterval).UnixMilli(), Upstream: 4096, Downstream: 8192},
		{SampledAt: now.UnixMilli(), Upstream: 5120, Downstream: 10240},
	}, node22.MediaRateSamples)

	cache.mu.Lock()
	var persisted []MediaRateSample
	require.NoError(t, json.Unmarshal([]byte(cache.values[nodeMediaRateHistoryCacheKey(11)]), &persisted))
	require.Equal(t, node11.MediaRateSamples, persisted)
	require.Equal(t, defaultMediaRateHistoryTTL, cache.ttls[nodeMediaRateHistoryCacheKey(11)])
	var persistedTrend []RuntimeTrendSample
	require.NoError(t, json.Unmarshal([]byte(cache.values[nodeRuntimeTrendHistoryCacheKey(11)]), &persistedTrend))
	require.Len(t, persistedTrend, 2)
	require.Equal(t, defaultMediaRateHistoryTTL, cache.ttls[nodeRuntimeTrendHistoryCacheKey(11)])
	cache.mu.Unlock()

	restarted := NewOverviewSampler(source, cache, WithOverviewSamplerClock(func() time.Time { return now }))
	restoredNode, err := restarted.GetNodeRuntime(ctx, 11)
	require.NoError(t, err)
	require.Equal(t, node11.MediaRateSamples, restoredNode.MediaRateSamples)
	require.Equal(t, node11.TrendSamples, restoredNode.TrendSamples)
}

func TestOverviewSamplerRestoresAndIsolatesNodeMediaRateHistory(t *testing.T) {
	ctx := context.Background()
	cache := newOverviewSamplerCacheFake()
	now := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	source := &overviewSamplerSourceFake{result: nodeRateOverview(now,
		nodeRateRuntime(11, 100, 200, true),
		nodeRateRuntime(22, 900, 1000, true),
	)}
	first := NewOverviewSampler(source, cache, WithOverviewSamplerClock(func() time.Time { return now }))
	require.NoError(t, first.SampleOnce(ctx))

	now = now.Add(DefaultMediaRateSampleInterval)
	source.set(nodeRateOverview(now,
		nodeRateRuntime(11, 300, 400, true),
		nodeRateRuntime(22, 0, 0, false),
	))
	restarted := NewOverviewSampler(source, cache, WithOverviewSamplerClock(func() time.Time { return now }))
	require.NoError(t, restarted.SampleOnce(ctx))

	node11, err := restarted.GetNodeRuntime(ctx, 11)
	require.NoError(t, err)
	require.Equal(t, []MediaRateSample{
		{SampledAt: now.Add(-DefaultMediaRateSampleInterval).UnixMilli(), Upstream: 100, Downstream: 200},
		{SampledAt: now.UnixMilli(), Upstream: 300, Downstream: 400},
	}, node11.MediaRateSamples)
	node22, err := restarted.GetNodeRuntime(ctx, 22)
	require.NoError(t, err)
	require.Equal(t, []MediaRateSample{
		{SampledAt: now.Add(-DefaultMediaRateSampleInterval).UnixMilli(), Upstream: 900, Downstream: 1000},
	}, node22.MediaRateSamples, "不可用采样不能伪造成 0，也不能混入其他节点")

	now = now.Add(DefaultMediaRateHistoryWindow)
	source.set(nodeRateOverview(now, nodeRateRuntime(11, 500, 600, true)))
	require.NoError(t, restarted.SampleOnce(ctx))
	node11, err = restarted.GetNodeRuntime(ctx, 11)
	require.NoError(t, err)
	require.Equal(t, []MediaRateSample{
		{SampledAt: now.Add(-DefaultMediaRateHistoryWindow).UnixMilli(), Upstream: 300, Downstream: 400},
		{SampledAt: now.UnixMilli(), Upstream: 500, Downstream: 600},
	}, node11.MediaRateSamples)
}
