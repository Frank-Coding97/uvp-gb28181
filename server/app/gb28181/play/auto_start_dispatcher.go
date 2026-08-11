package play

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/global/app"
)

const (
	autoStartWorkerCount = 8
	autoStartQueueSize   = 8

	// DefaultAutoStartServiceDeadline bounds one on-demand live start and the
	// normal dispatcher shutdown wait.
	DefaultAutoStartServiceDeadline = 12 * time.Second
	defaultAutoStartNodeRate        = 8.0
	defaultAutoStartNodeBurst       = 16
	autoStartStopGrace              = 10 * time.Millisecond
)

var (
	ErrAutoStartStopped         = errors.New("auto-start dispatcher stopped")
	ErrAutoStartQueueFull       = errors.New("auto-start dispatcher queue full")
	ErrAutoStartNodeRateLimited = errors.New("auto-start dispatcher node rate limited")
	ErrAutoStartInvalidRequest  = errors.New("invalid auto-start request")
)

// AutoStartSubmitter is the narrow non-blocking contract used by stream-not-
// found handlers. A nil error means the request is queued or already queued.
type AutoStartSubmitter interface {
	Available() bool
	Submit(Request) error
}

// AutoStartEnsurer is implemented by Service and Coordinator.
type AutoStartEnsurer interface {
	EnsureLive(context.Context, Request) (*Result, error)
}

// AutoStartDispatcherOptions configures testable time and node-limit inputs.
// KnownNodeIDs is the complete, bounded set for which rate limit state exists.
type AutoStartDispatcherOptions struct {
	KnownNodeIDs    []int64
	NodeAllowed     func(int64) bool
	Clock           func() time.Time
	ServiceDeadline time.Duration
	NodeRate        float64
	NodeBurst       int
}

type autoStartKey struct {
	deviceID        string
	channelID       string
	requiredNode    int64
	authorizationID string
}

type autoStartBucket struct {
	tokens float64
	last   time.Time
}

type autoStartJob struct {
	key autoStartKey
	req Request
}

// AutoStartDispatcher admits a bounded amount of auto-start work. It has a
// fixed eight-worker pool and an eight-slot queue, so queued plus running work
// never exceeds sixteen distinct request keys.
type AutoStartDispatcher struct {
	mu          sync.Mutex
	ensure      AutoStartEnsurer
	jobs        chan autoStartJob
	inflight    map[autoStartKey]struct{}
	buckets     map[int64]*autoStartBucket
	nodeAllowed func(int64) bool
	now         func() time.Time

	deadline time.Duration
	rate     float64
	burst    int
	stopped  bool
	stopCh   chan struct{}
	doneCh   chan struct{}
	workers  atomic.Int32
}

func NewAutoStartDispatcher(ensure AutoStartEnsurer, opts AutoStartDispatcherOptions) *AutoStartDispatcher {
	if ensure == nil {
		panic("play: nil auto-start ensurer")
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.ServiceDeadline <= 0 {
		opts.ServiceDeadline = DefaultAutoStartServiceDeadline
	}
	if opts.NodeRate <= 0 {
		opts.NodeRate = defaultAutoStartNodeRate
	}
	if opts.NodeBurst <= 0 {
		opts.NodeBurst = defaultAutoStartNodeBurst
	}

	now := opts.Clock()
	d := &AutoStartDispatcher{
		ensure:      ensure,
		jobs:        make(chan autoStartJob, autoStartQueueSize),
		inflight:    make(map[autoStartKey]struct{}, autoStartWorkerCount+autoStartQueueSize),
		buckets:     make(map[int64]*autoStartBucket, len(opts.KnownNodeIDs)),
		nodeAllowed: opts.NodeAllowed,
		now:         opts.Clock,
		deadline:    opts.ServiceDeadline,
		rate:        opts.NodeRate,
		burst:       opts.NodeBurst,
		stopCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
	}
	for _, nodeID := range opts.KnownNodeIDs {
		if nodeID > 0 {
			d.buckets[nodeID] = &autoStartBucket{tokens: float64(d.burst), last: now}
		}
	}
	d.workers.Store(autoStartWorkerCount)
	for i := 0; i < autoStartWorkerCount; i++ {
		go d.worker()
	}
	return d
}

// Available reports whether Submit can accept a new request.
func (d *AutoStartDispatcher) Available() bool {
	d.mu.Lock()
	available := !d.stopped
	d.mu.Unlock()
	return available
}

// Submit queues one on-demand live start without blocking. Repeated queued or
// running requests with the same device, channel, and required node succeed
// without consuming capacity or a rate-limit token.
func (d *AutoStartDispatcher) Submit(req Request) error {
	key, err := d.requestKey(req)
	if err != nil {
		return err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return ErrAutoStartStopped
	}
	if _, ok := d.inflight[key]; ok {
		return nil
	}
	if len(d.inflight) >= autoStartWorkerCount+autoStartQueueSize {
		return ErrAutoStartQueueFull
	}
	if d.nodeAllowed != nil && !d.nodeAllowed(key.requiredNode) {
		return ErrAutoStartInvalidRequest
	}
	bucket := d.buckets[key.requiredNode]
	if bucket == nil {
		if d.nodeAllowed == nil || !d.nodeAllowed(key.requiredNode) {
			return ErrAutoStartInvalidRequest
		}
		bucket = &autoStartBucket{tokens: float64(d.burst), last: d.now()}
		d.buckets[key.requiredNode] = bucket
	}
	if !bucket.take(d.now(), d.rate, d.burst) {
		return ErrAutoStartNodeRateLimited
	}
	req.Trigger = "on_stream_not_found"
	job := autoStartJob{key: key, req: req}
	select {
	case d.jobs <- job:
		d.inflight[key] = struct{}{}
		return nil
	default:
		bucket.refund(d.burst)
		return ErrAutoStartQueueFull
	}
}

func (d *AutoStartDispatcher) requestKey(req Request) (autoStartKey, error) {
	if req.DeviceID == "" || req.ChannelID == "" || req.RequiredNode <= 0 || req.AuthorizationID == "" {
		return autoStartKey{}, ErrAutoStartInvalidRequest
	}
	return autoStartKey{
		deviceID: req.DeviceID, channelID: req.ChannelID,
		requiredNode: req.RequiredNode, authorizationID: req.AuthorizationID,
	}, nil
}

// Stop rejects new work before closing the queue, then waits for the fixed
// worker pool. Pending jobs are discarded; in-flight starts retain their own
// service deadline. Repeated calls share the same shutdown state.
func (d *AutoStartDispatcher) Stop() error {
	d.mu.Lock()
	if !d.stopped {
		d.stopped = true
		close(d.stopCh)
		close(d.jobs)
	}
	d.mu.Unlock()

	// Give workers a small scheduling margin after their service deadline so a
	// timely cancellation is observed as a clean shutdown rather than a timer
	// race. The bound remains approximately one service deadline.
	timer := time.NewTimer(d.deadline + autoStartStopGrace)
	defer timer.Stop()
	select {
	case <-d.doneCh:
		return nil
	case <-timer.C:
		return context.DeadlineExceeded
	}
}

func (d *AutoStartDispatcher) worker() {
	defer func() {
		if d.workers.Add(-1) == 0 {
			close(d.doneCh)
		}
	}()
	for job := range d.jobs {
		select {
		case <-d.stopCh:
			d.finish(job.key)
			continue
		default:
		}
		ctx, cancel := context.WithTimeout(context.Background(), d.deadline)
		result, err := d.ensure.EnsureLive(ctx, job.req)
		cancel()
		logAutoStartResult(job, result, err)
		d.finish(job.key)
	}
}

func (d *AutoStartDispatcher) finish(key autoStartKey) {
	d.mu.Lock()
	delete(d.inflight, key)
	d.mu.Unlock()
}

func (b *autoStartBucket) take(now time.Time, rate float64, burst int) bool {
	if now.After(b.last) {
		b.tokens += now.Sub(b.last).Seconds() * rate
		if b.tokens > float64(burst) {
			b.tokens = float64(burst)
		}
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (b *autoStartBucket) refund(burst int) {
	b.tokens++
	if b.tokens > float64(burst) {
		b.tokens = float64(burst)
	}
}

func logAutoStartResult(job autoStartJob, result *Result, err error) {
	if app.ZapLog == nil {
		return
	}
	fields := []zap.Field{
		zap.String("reason", autoStartResultReason(err)),
		zap.String("deviceId", job.key.deviceID),
		zap.String("channelId", job.key.channelID),
		zap.Int64("nodeId", job.key.requiredNode),
	}
	if err != nil {
		app.ZapLog.Warn("自动点播后台启动失败", fields...)
		return
	}
	if result != nil {
		fields = append(fields, zap.String("streamId", result.StreamID))
	}
	app.ZapLog.Info("自动点播后台启动成功", fields...)
}

func autoStartResultReason(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, ErrPlayTimeout):
		return "timeout"
	case errors.Is(err, ErrOwnerNodeMismatch):
		return "owner-node-mismatch"
	case errors.Is(err, ErrRequiredNodeUnavailable):
		return "required-node-unavailable"
	case errors.Is(err, ErrLiveRecoveryPending):
		return "recovery-pending"
	case errors.Is(err, ErrDeviceNotFound):
		return "device-not-found"
	case errors.Is(err, ErrDeviceOffline):
		return "device-offline"
	case errors.Is(err, ErrChannelNotFound):
		return "channel-not-found"
	case errors.Is(err, ErrStreamNotReady):
		return "stream-not-ready"
	default:
		return "ensure-live-failed"
	}
}
