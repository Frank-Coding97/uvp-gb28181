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
	DefaultMediaRateSampleInterval = 5 * time.Second
	DefaultMediaRateHistoryWindow  = 5 * time.Minute
	defaultMediaRateHistoryTTL     = 15 * time.Minute
	mediaRateHistoryCacheKey       = "uvp:gb28181:dashboard:media-rate:5m:v1"
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

	sampleMu sync.Mutex
	stateMu  sync.RWMutex
	latest   *OverviewResult
	history  []MediaRateSample
	loaded   bool

	startOnce sync.Once
	stopOnce  sync.Once
	stop      chan struct{}
	done      chan struct{}

	lifecycleCtx    context.Context
	lifecycleCancel context.CancelFunc
	shutdownOnce    sync.Once
	shutdownDone    chan struct{}
}

func NewOverviewSampler(source overviewSamplerSource, cache app.CacheInterf, options ...OverviewSamplerOption) *OverviewSampler {
	lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())
	sampler := &OverviewSampler{
		source: source, cache: cache, now: time.Now,
		interval: DefaultMediaRateSampleInterval,
		window:   DefaultMediaRateHistoryWindow,
		stop:     make(chan struct{}), done: make(chan struct{}),
		lifecycleCtx: lifecycleCtx, lifecycleCancel: lifecycleCancel,
		shutdownDone: make(chan struct{}),
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
			lifecycleCtx := sampler.lifecycleCtx
			if lifecycleCtx == nil {
				lifecycleCtx = context.Background()
			}
			recordError := func(err error) {
				if err != nil && onError != nil {
					onError(err)
				}
			}
			if lifecycleCtx.Err() != nil {
				return
			}
			recordError(sampler.SampleOnce(lifecycleCtx))
			ticker := time.NewTicker(sampler.interval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if lifecycleCtx.Err() != nil {
						return
					}
					recordError(sampler.SampleOnce(lifecycleCtx))
				case <-sampler.stop:
					return
				case <-lifecycleCtx.Done():
					return
				}
			}
		}()
	})
}

// Shutdown stops new background sampling, cancels the lifecycle context passed
// to the active SampleOnce call, and waits for the sampling goroutine to exit.
// A deadline reports that the source or cache is still running; a later call
// can continue waiting for the same shutdown.
func (sampler *OverviewSampler) Shutdown(ctx context.Context) error {
	if sampler == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sampler.shutdownOnce.Do(func() {
		// If Start has not won the startOnce yet, closing done here permanently
		// prevents a later Start from launching a new sampler goroutine.
		sampler.startOnce.Do(func() { close(sampler.done) })
		sampler.stopOnce.Do(func() { close(sampler.stop) })
		if sampler.lifecycleCancel != nil {
			sampler.lifecycleCancel()
		}
		go func() {
			<-sampler.done
			close(sampler.shutdownDone)
		}()
	})
	select {
	case <-sampler.shutdownDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close preserves the legacy no-return lifecycle API. New callers should use
// Shutdown when they need a bounded wait result.
func (sampler *OverviewSampler) Close() {
	if sampler == nil {
		return
	}
	_ = sampler.Shutdown(context.Background())
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
	result.MediaRateSamples = cloneMediaRateSamples(sampler.history)
	sampler.latest = &result
	history := cloneMediaRateSamples(sampler.history)
	sampler.stateMu.Unlock()

	persistErr := sampler.persistHistory(ctx, history)
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
	return sampler.source.GetNodeRuntime(ctx, nodeID)
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

func (sampler *OverviewSampler) historySnapshot() []MediaRateSample {
	if sampler == nil {
		return nil
	}
	sampler.stateMu.RLock()
	defer sampler.stateMu.RUnlock()
	return cloneMediaRateSamples(sampler.history)
}

func (sampler *OverviewSampler) pruneHistoryLocked(now time.Time) {
	cutoff := now.Add(-sampler.window).UnixMilli()
	first := 0
	for first < len(sampler.history) && sampler.history[first].SampledAt < cutoff {
		first++
	}
	if first > 0 {
		sampler.history = append([]MediaRateSample(nil), sampler.history[first:]...)
	}
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
