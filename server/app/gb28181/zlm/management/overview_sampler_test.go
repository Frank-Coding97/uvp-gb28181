package management

import (
	"context"
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
}

func newOverviewSamplerCacheFake() *overviewSamplerCacheFake {
	return &overviewSamplerCacheFake{values: make(map[string]string)}
}

func (cache *overviewSamplerCacheFake) Set(_ context.Context, key, value string, _ time.Duration) error {
	cache.mu.Lock()
	cache.values[key] = value
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

func (source *overviewSamplerSourceFake) GetNodeRuntime(context.Context, int64) (NodeRuntimeView, error) {
	return NodeRuntimeView{}, nil
}

func (source *overviewSamplerSourceFake) ListStreams(context.Context, StreamFilter, PageRequest) (StreamDistribution, error) {
	return StreamDistribution{}, nil
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
