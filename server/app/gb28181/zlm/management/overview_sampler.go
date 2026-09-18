package management

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"uvplatform.cn/uvp-gb28181/app/global/app"
)

const (
	DefaultMediaRateSampleInterval  = 5 * time.Second
	DefaultMediaRateHistoryWindow   = 5 * time.Minute
	defaultMediaRateHistoryTTL      = 15 * time.Minute
	mediaRateHistoryCacheKey        = "uvp:gb28181:dashboard:media-rate:5m:v1"
	nodeMediaRateHistoryCachePrefix = "uvp:gb28181:runtime:node:"
)

type MediaRateSample struct {
	SampledAt  int64  `json:"sampledAt"`
	Upstream   uint64 `json:"upstream"`
	Downstream uint64 `json:"downstream"`
}

type overviewSamplerSource interface {
	GetOverview(context.Context) (OverviewResult, error)
	GetNodeRuntime(context.Context, int64) (NodeRuntimeView, error)
	ListStreams(context.Context, StreamFilter, PageRequest) (StreamDistribution, error)
}

type OverviewSamplerOption func(*OverviewSampler)

func WithOverviewSamplerClock(clock func() time.Time) OverviewSamplerOption {
	return func(sampler *OverviewSampler) {
		if clock != nil {
			sampler.now = clock
		}
	}
}

func WithOverviewSamplerInterval(interval time.Duration) OverviewSamplerOption {
	return func(sampler *OverviewSampler) {
		if interval > 0 {
			sampler.interval = interval
		}
	}
}

type OverviewSampler struct {
	source   overviewSamplerSource
	cache    app.CacheInterf
	now      func() time.Time
	interval time.Duration
	window   time.Duration

	sampleMu               sync.Mutex
	stateMu                sync.RWMutex
	latest                 *OverviewResult
	history                []MediaRateSample
	loaded                 bool
	nodeHistories          map[int64][]MediaRateSample
	nodeHistoryLoaded      map[int64]bool
	nodeTrendHistories     map[int64][]RuntimeTrendSample
	nodeTrendHistoryLoaded map[int64]bool

	startOnce sync.Once
	stopOnce  sync.Once
	stop      chan struct{}
	done      chan struct{}
}

func NewOverviewSampler(source overviewSamplerSource, cache app.CacheInterf, options ...OverviewSamplerOption) *OverviewSampler {
	sampler := &OverviewSampler{
		source: source, cache: cache, now: time.Now,
		interval:               DefaultMediaRateSampleInterval,
		window:                 DefaultMediaRateHistoryWindow,
		nodeHistories:          make(map[int64][]MediaRateSample),
		nodeHistoryLoaded:      make(map[int64]bool),
		nodeTrendHistories:     make(map[int64][]RuntimeTrendSample),
		nodeTrendHistoryLoaded: make(map[int64]bool),
		stop:                   make(chan struct{}),
		done:                   make(chan struct{}),
	}
	for _, option := range options {
		if option != nil {
			option(sampler)
		}
	}
	return sampler
}

// Start owns the single process-wide sampling loop. The callback receives
// transient source/cache errors without turning a previously sampled overview
// into zero-valued data.
func (sampler *OverviewSampler) Start(onError func(error)) {
	if sampler == nil || sampler.source == nil {
		return
	}
	sampler.startOnce.Do(func() {
		go func() {
			defer close(sampler.done)
			recordError := func(err error) {
				if err != nil && onError != nil {
					onError(err)
				}
			}
			recordError(sampler.SampleOnce(context.Background()))
			ticker := time.NewTicker(sampler.interval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					recordError(sampler.SampleOnce(context.Background()))
				case <-sampler.stop:
					return
				}
			}
		}()
	})
}

func (sampler *OverviewSampler) Close() {
	if sampler == nil {
		return
	}
	started := false
	sampler.startOnce.Do(func() { close(sampler.done) })
	select {
	case <-sampler.done:
		return
	default:
		started = true
	}
	if started {
		sampler.stopOnce.Do(func() { close(sampler.stop) })
		<-sampler.done
	}
}

func (sampler *OverviewSampler) SampleOnce(ctx context.Context) error {
	if sampler == nil || sampler.source == nil {
		return errors.New("overview sampler is not configured")
	}
	sampler.sampleMu.Lock()
	defer sampler.sampleMu.Unlock()
	return sampler.sampleOnceLocked(ctx)
}

func (sampler *OverviewSampler) sampleOnceLocked(ctx context.Context) error {
	var cacheErr error
	if !sampler.loaded {
		cacheErr = sampler.loadHistoryLocked(ctx)
	}
	result, err := sampler.source.GetOverview(ctx)
	if err != nil {
		return errors.Join(cacheErr, err)
	}
	for _, runtime := range result.Nodes {
		if runtime.NodeID <= 0 {
			continue
		}
		if !sampler.nodeHistoryLoaded[runtime.NodeID] {
			cacheErr = errors.Join(cacheErr, sampler.loadNodeHistoryLocked(ctx, runtime.NodeID))
		}
		if !sampler.nodeTrendHistoryLoaded[runtime.NodeID] {
			cacheErr = errors.Join(cacheErr, sampler.loadNodeTrendHistoryLocked(ctx, runtime.NodeID))
		}
	}
	now := sampler.now()
	sampler.stateMu.Lock()
	sampler.pruneHistoryLocked(now)
	if exactMediaRate(result.Metrics) {
		sample := MediaRateSample{
			SampledAt:  now.UnixMilli(),
			Upstream:   result.Metrics.UpstreamBytesPerSecond,
			Downstream: result.Metrics.DownstreamBytesPerSecond,
		}
		if length := len(sampler.history); length > 0 && sampler.history[length-1].SampledAt == sample.SampledAt {
			sampler.history[length-1] = sample
		} else {
			sampler.history = append(sampler.history, sample)
		}
	}
	nodeHistories := make(map[int64][]MediaRateSample, len(result.Nodes))
	nodeTrendHistories := make(map[int64][]RuntimeTrendSample, len(result.Nodes))
	for _, runtime := range result.Nodes {
		if runtime.NodeID <= 0 {
			continue
		}
		history := pruneMediaRateHistory(sampler.nodeHistories[runtime.NodeID], now, sampler.window)
		if runtime.Metrics.MediaTrafficAvailable {
			history = appendMediaRateSample(history, MediaRateSample{
				SampledAt:  now.UnixMilli(),
				Upstream:   runtime.Metrics.UpstreamBytesPerSecond,
				Downstream: runtime.Metrics.DownstreamBytesPerSecond,
			})
		}
		sampler.nodeHistories[runtime.NodeID] = history
		trendHistory := pruneRuntimeTrendHistory(sampler.nodeTrendHistories[runtime.NodeID], now, sampler.window)
		trendHistory = appendRuntimeTrendSample(trendHistory, runtimeTrendSample(runtime, now))
		sampler.nodeTrendHistories[runtime.NodeID] = trendHistory
		if len(history) > 0 {
			nodeHistories[runtime.NodeID] = cloneMediaRateSamples(history)
		}
		if len(trendHistory) > 0 {
			nodeTrendHistories[runtime.NodeID] = cloneRuntimeTrendSamples(trendHistory)
		}
	}
	result.MediaRateSamples = cloneMediaRateSamples(sampler.history)
	sampler.latest = &result
	history := cloneMediaRateSamples(sampler.history)
	sampler.stateMu.Unlock()

	persistErr := sampler.persistHistory(ctx, history)
	for nodeID, nodeHistory := range nodeHistories {
		persistErr = errors.Join(persistErr, sampler.persistNodeHistory(ctx, nodeID, nodeHistory))
	}
	for nodeID, trendHistory := range nodeTrendHistories {
		persistErr = errors.Join(persistErr, sampler.persistNodeTrendHistory(ctx, nodeID, trendHistory))
	}
	return errors.Join(cacheErr, persistErr)
}

func (sampler *OverviewSampler) GetOverview(ctx context.Context) (OverviewResult, error) {
	if result, ok := sampler.latestSnapshot(); ok {
		return result, nil
	}
	sampler.sampleMu.Lock()
	defer sampler.sampleMu.Unlock()
	if result, ok := sampler.latestSnapshot(); ok {
		return result, nil
	}
	err := sampler.sampleOnceLocked(ctx)
	if result, ok := sampler.latestSnapshot(); ok {
		return result, nil
	}
	return OverviewResult{}, err
}

func (sampler *OverviewSampler) GetNodeRuntime(ctx context.Context, nodeID int64) (NodeRuntimeView, error) {
	result, err := sampler.source.GetNodeRuntime(ctx, nodeID)
	if err != nil {
		return NodeRuntimeView{}, err
	}
	sampler.sampleMu.Lock()
	if !sampler.nodeHistoryLoaded[nodeID] {
		_ = sampler.loadNodeHistoryLocked(ctx, nodeID)
	}
	if !sampler.nodeTrendHistoryLoaded[nodeID] {
		_ = sampler.loadNodeTrendHistoryLocked(ctx, nodeID)
	}
	sampler.sampleMu.Unlock()
	sampler.stateMu.RLock()
	result.MediaRateSamples = cloneMediaRateSamples(sampler.nodeHistories[nodeID])
	result.TrendSamples = cloneRuntimeTrendSamples(sampler.nodeTrendHistories[nodeID])
	sampler.stateMu.RUnlock()
	return result, nil
}

func (sampler *OverviewSampler) ListStreams(ctx context.Context, filter StreamFilter, page PageRequest) (StreamDistribution, error) {
	return sampler.source.ListStreams(ctx, filter, page)
}

func (sampler *OverviewSampler) latestSnapshot() (OverviewResult, bool) {
	if sampler == nil {
		return OverviewResult{}, false
	}
	sampler.stateMu.RLock()
	defer sampler.stateMu.RUnlock()
	if sampler.latest == nil {
		return OverviewResult{}, false
	}
	result := *sampler.latest
	result.MediaRateSamples = cloneMediaRateSamples(sampler.history)
	return result, true
}

func (sampler *OverviewSampler) loadHistory(ctx context.Context) error {
	if sampler == nil {
		return nil
	}
	sampler.sampleMu.Lock()
	defer sampler.sampleMu.Unlock()
	return sampler.loadHistoryLocked(ctx)
}

func (sampler *OverviewSampler) loadHistoryLocked(ctx context.Context) error {
	sampler.loaded = true
	if sampler.cache == nil {
		return nil
	}
	raw, err := sampler.cache.Get(ctx, mediaRateHistoryCacheKey)
	if errors.Is(err, app.ErrKeyNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取媒体速率历史失败: %w", err)
	}
	var history []MediaRateSample
	if err := json.Unmarshal([]byte(raw), &history); err != nil {
		return fmt.Errorf("解析媒体速率历史失败: %w", err)
	}
	sampler.stateMu.Lock()
	sampler.history = history
	sampler.pruneHistoryLocked(sampler.now())
	sampler.stateMu.Unlock()
	return nil
}

func (sampler *OverviewSampler) loadNodeHistoryLocked(ctx context.Context, nodeID int64) error {
	if sampler.cache == nil {
		sampler.nodeHistoryLoaded[nodeID] = true
		return nil
	}
	raw, err := sampler.cache.Get(ctx, nodeMediaRateHistoryCacheKey(nodeID))
	if errors.Is(err, app.ErrKeyNotFound) {
		sampler.nodeHistoryLoaded[nodeID] = true
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取节点 %d 媒体速率历史失败: %w", nodeID, err)
	}
	var history []MediaRateSample
	if err := json.Unmarshal([]byte(raw), &history); err != nil {
		return fmt.Errorf("解析节点 %d 媒体速率历史失败: %w", nodeID, err)
	}
	sampler.stateMu.Lock()
	sampler.nodeHistories[nodeID] = pruneMediaRateHistory(history, sampler.now(), sampler.window)
	sampler.stateMu.Unlock()
	sampler.nodeHistoryLoaded[nodeID] = true
	return nil
}

func (sampler *OverviewSampler) loadNodeTrendHistoryLocked(ctx context.Context, nodeID int64) error {
	if sampler.cache == nil {
		sampler.nodeTrendHistoryLoaded[nodeID] = true
		return nil
	}
	raw, err := sampler.cache.Get(ctx, nodeRuntimeTrendHistoryCacheKey(nodeID))
	if errors.Is(err, app.ErrKeyNotFound) {
		sampler.nodeTrendHistoryLoaded[nodeID] = true
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取节点 %d 运行趋势失败: %w", nodeID, err)
	}
	var history []RuntimeTrendSample
	if err := json.Unmarshal([]byte(raw), &history); err != nil {
		return fmt.Errorf("解析节点 %d 运行趋势失败: %w", nodeID, err)
	}
	sampler.stateMu.Lock()
	sampler.nodeTrendHistories[nodeID] = pruneRuntimeTrendHistory(history, sampler.now(), sampler.window)
	sampler.stateMu.Unlock()
	sampler.nodeTrendHistoryLoaded[nodeID] = true
	return nil
}

func (sampler *OverviewSampler) persistHistory(ctx context.Context, history []MediaRateSample) error {
	if sampler.cache == nil {
		return nil
	}
	payload, err := json.Marshal(history)
	if err != nil {
		return fmt.Errorf("编码媒体速率历史失败: %w", err)
	}
	if err := sampler.cache.Set(ctx, mediaRateHistoryCacheKey, string(payload), defaultMediaRateHistoryTTL); err != nil {
		return fmt.Errorf("保存媒体速率历史失败: %w", err)
	}
	return nil
}

func (sampler *OverviewSampler) persistNodeHistory(ctx context.Context, nodeID int64, history []MediaRateSample) error {
	if sampler.cache == nil {
		return nil
	}
	payload, err := json.Marshal(history)
	if err != nil {
		return fmt.Errorf("编码节点 %d 媒体速率历史失败: %w", nodeID, err)
	}
	if err := sampler.cache.Set(ctx, nodeMediaRateHistoryCacheKey(nodeID), string(payload), defaultMediaRateHistoryTTL); err != nil {
		return fmt.Errorf("保存节点 %d 媒体速率历史失败: %w", nodeID, err)
	}
	return nil
}

func (sampler *OverviewSampler) persistNodeTrendHistory(ctx context.Context, nodeID int64, history []RuntimeTrendSample) error {
	if sampler.cache == nil {
		return nil
	}
	payload, err := json.Marshal(history)
	if err != nil {
		return fmt.Errorf("编码节点 %d 运行趋势失败: %w", nodeID, err)
	}
	if err := sampler.cache.Set(ctx, nodeRuntimeTrendHistoryCacheKey(nodeID), string(payload), defaultMediaRateHistoryTTL); err != nil {
		return fmt.Errorf("保存节点 %d 运行趋势失败: %w", nodeID, err)
	}
	return nil
}

func (sampler *OverviewSampler) historySnapshot() []MediaRateSample {
	if sampler == nil {
		return nil
	}
	sampler.stateMu.RLock()
	defer sampler.stateMu.RUnlock()
	return cloneMediaRateSamples(sampler.history)
}

func (sampler *OverviewSampler) pruneHistoryLocked(now time.Time) {
	sampler.history = pruneMediaRateHistory(sampler.history, now, sampler.window)
}

func pruneMediaRateHistory(history []MediaRateSample, now time.Time, window time.Duration) []MediaRateSample {
	cutoff := now.Add(-window).UnixMilli()
	first := 0
	for first < len(history) && history[first].SampledAt < cutoff {
		first++
	}
	if first > 0 {
		return append([]MediaRateSample(nil), history[first:]...)
	}
	return history
}

func appendMediaRateSample(history []MediaRateSample, sample MediaRateSample) []MediaRateSample {
	if length := len(history); length > 0 && history[length-1].SampledAt == sample.SampledAt {
		history[length-1] = sample
		return history
	}
	return append(history, sample)
}

func nodeMediaRateHistoryCacheKey(nodeID int64) string {
	return fmt.Sprintf("%s%d:media-rate:5m:v1", nodeMediaRateHistoryCachePrefix, nodeID)
}

func nodeRuntimeTrendHistoryCacheKey(nodeID int64) string {
	return fmt.Sprintf("%s%d:trend:5m:v1", nodeMediaRateHistoryCachePrefix, nodeID)
}

func runtimeTrendSample(runtime NodeRuntimeView, now time.Time) RuntimeTrendSample {
	sample := RuntimeTrendSample{SampledAt: now.UnixMilli()}
	if runtime.MediaFreshness == RuntimeFreshnessFresh {
		streamCount := int64(len(runtime.Streams))
		viewerCount := int64(0)
		viewersAvailable := true
		throughput := uint64(0)
		for _, stream := range runtime.Streams {
			if stream.ReaderCount < 0 {
				viewersAvailable = false
			} else {
				viewerCount += int64(stream.ReaderCount)
			}
			throughput += stream.BytesSpeed
		}
		sample.StreamCount = &streamCount
		if viewersAvailable {
			sample.ViewerCount = &viewerCount
		}
		sample.Throughput = &throughput
	}
	if runtime.MetricsComplete {
		if runtime.Metrics.NetworkSessionCount >= 0 {
			sessionCount := int64(runtime.Metrics.NetworkSessionCount)
			sample.SessionCount = &sessionCount
		}
		statistics := runtime.Metrics.ObjectStatistics
		sample.ObjectStatistics = &statistics
	}
	return sample
}

func appendRuntimeTrendSample(history []RuntimeTrendSample, sample RuntimeTrendSample) []RuntimeTrendSample {
	if len(history) > 0 && history[len(history)-1].SampledAt == sample.SampledAt {
		history[len(history)-1] = sample
		return history
	}
	return append(history, sample)
}

func pruneRuntimeTrendHistory(history []RuntimeTrendSample, now time.Time, window time.Duration) []RuntimeTrendSample {
	cutoff := now.Add(-window).UnixMilli()
	first := 0
	for first < len(history) && history[first].SampledAt < cutoff {
		first++
	}
	if first == 0 {
		return history
	}
	return append([]RuntimeTrendSample(nil), history[first:]...)
}

func cloneRuntimeTrendSamples(samples []RuntimeTrendSample) []RuntimeTrendSample {
	if len(samples) == 0 {
		return []RuntimeTrendSample{}
	}
	cloned := make([]RuntimeTrendSample, len(samples))
	for index, sample := range samples {
		cloned[index] = sample
		if sample.ObjectStatistics != nil {
			statistics := *sample.ObjectStatistics
			cloned[index].ObjectStatistics = &statistics
		}
	}
	return cloned
}

func exactMediaRate(metrics OverviewMetrics) bool {
	return metrics.SampledNodeCount > 0 && metrics.MediaTrafficSampledNodes == metrics.SampledNodeCount
}

func cloneMediaRateSamples(samples []MediaRateSample) []MediaRateSample {
	if len(samples) == 0 {
		return []MediaRateSample{}
	}
	return append([]MediaRateSample(nil), samples...)
}
