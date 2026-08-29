package management

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func TestOverviewIncludesAllNodeStatesAndReadsOnlyActiveNodes(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	registry := overviewRegistryFake{nodes: []*node.Node{
		overviewNode(1, node.StateActive, now.Add(-time.Second)),
		overviewNode(2, node.StateMaintenance, now.Add(-time.Second)),
		overviewNode(3, node.StateOffline, now.Add(-time.Second)),
	}}
	tracker := &overviewCallTracker{}
	runtime := &overviewRuntimeFake{tracker: tracker, statistics: map[int64]zlm.Statistic{
		1: {MediaSource: 4, TcpSession: 2},
	}}
	media := &overviewMediaFake{tracker: tracker, media: map[int64][]zlm.MediaInfo{
		1: {{Online: true, Schema: "rtsp", VHost: "__defaultVhost__", App: "rtp", Stream: "live-1"}},
	}}
	service := NewOverviewService(OverviewDependencies{Registry: registry, Runtime: runtime, Media: media}, WithOverviewClock(func() time.Time { return now }))

	result, err := service.GetOverview(context.Background())
	require.NoError(t, err)
	require.Len(t, result.Nodes, 3)
	require.False(t, result.Partial)
	require.Equal(t, now, result.AsOf)

	active := overviewNodeResult(t, result, 1)
	require.Equal(t, node.StateActive, active.State)
	require.Equal(t, RuntimeFreshnessFresh, active.Freshness)
	require.Equal(t, uint64(4), active.Metrics.MediaSourceCount)
	require.Equal(t, RuntimeFreshnessFresh, active.MediaFreshness)
	require.Len(t, active.Streams, 1)

	maintenance := overviewNodeResult(t, result, 2)
	require.Equal(t, node.StateMaintenance, maintenance.State)
	require.Equal(t, RuntimeFreshnessMaintenance, maintenance.Freshness)
	require.Equal(t, RuntimeFreshnessMaintenance, maintenance.MediaFreshness)
	require.NotNil(t, maintenance.Error)

	offline := overviewNodeResult(t, result, 3)
	require.Equal(t, node.StateOffline, offline.State)
	require.Equal(t, RuntimeFreshnessUnavailable, offline.Freshness)
	require.Equal(t, RuntimeFreshnessUnavailable, offline.MediaFreshness)
	require.NotNil(t, offline.Error)
	require.Equal(t, 1, tracker.count("statistic", 1))
	require.Zero(t, tracker.count("statistic", 2))
	require.Zero(t, tracker.count("statistic", 3))
}

func TestOverviewPartialNodeFailureKeepsSuccessfulCurrentMetricsOnly(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 1, 0, 0, time.UTC)
	registry := overviewRegistryFake{nodes: []*node.Node{
		overviewNode(1, node.StateActive, now),
		overviewNode(2, node.StateActive, now),
	}}
	runtime := &overviewRuntimeFake{
		statistics: map[int64]zlm.Statistic{1: {MediaSource: 5}, 2: {MediaSource: 99}},
		errors:     map[int64]error{2: errors.New("node timeout with secret=must-not-leak")},
	}
	media := &overviewMediaFake{media: map[int64][]zlm.MediaInfo{
		1: {{Online: true, Schema: "rtsp", VHost: "__defaultVhost__", App: "rtp", Stream: "ok"}},
	}}
	service := NewOverviewService(OverviewDependencies{Registry: registry, Runtime: runtime, Media: media}, WithOverviewClock(func() time.Time { return now }))

	result, err := service.GetOverview(context.Background())
	require.NoError(t, err)
	require.True(t, result.Partial)
	require.Equal(t, []int64{1}, result.SuccessfulNodeIDs)
	require.Equal(t, []int64{2}, result.FailedNodeIDs)
	require.Equal(t, uint64(5), result.Metrics.MediaSourceCount)
	require.Equal(t, int64(1), result.Metrics.SampledNodeCount)

	failed := overviewNodeResult(t, result, 2)
	require.Equal(t, RuntimeFreshnessUnavailable, failed.Freshness)
	require.NotNil(t, failed.Error)
	require.Equal(t, CodeInternal, failed.Error.Code)
	require.NotContains(t, failed.Error.Message, "secret")
	require.Empty(t, failed.Streams)
}

func TestOverviewMediaOnlyFailureExposesSeparateSamplingScopes(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 1, 15, 0, time.UTC)
	registry := overviewRegistryFake{nodes: []*node.Node{overviewNode(1, node.StateActive, now)}}
	runtime := &overviewRuntimeFake{statistics: map[int64]zlm.Statistic{1: {MediaSource: 6}}}
	media := &overviewMediaFake{errors: map[int64]error{1: errors.New("media list unavailable")}}
	service := NewOverviewService(OverviewDependencies{Registry: registry, Runtime: runtime, Media: media}, WithOverviewClock(func() time.Time { return now }))

	result, err := service.GetOverview(context.Background())
	require.NoError(t, err)
	require.True(t, result.Partial)
	require.Equal(t, []int64{1}, result.MetricsSampledNodeIDs)
	require.Empty(t, result.MediaSampledNodeIDs)
	require.Empty(t, result.SuccessfulNodeIDs)
	require.Equal(t, []int64{1}, result.FailedNodeIDs)
	require.Equal(t, uint64(6), result.Metrics.MediaSourceCount)
	require.Equal(t, int64(1), result.Metrics.SampledNodeCount)
}

func TestOverviewBoundedWorkerDoesNotExceedFourNodes(t *testing.T) {
	const nodeCount = 20
	registry := overviewRegistryFake{}
	for id := 1; id <= nodeCount; id++ {
		registry.nodes = append(registry.nodes, overviewNode(int64(id), node.StateActive, time.Now()))
	}
	tracker := &overviewCallTracker{block: make(chan struct{}), reached: make(chan struct{})}
	runtime := &overviewRuntimeFake{tracker: tracker}
	media := &overviewMediaFake{tracker: tracker}
	service := NewOverviewService(OverviewDependencies{Registry: registry, Runtime: runtime, Media: media})

	done := make(chan struct {
		result OverviewResult
		err    error
	}, 1)
	go func() {
		result, err := service.GetOverview(context.Background())
		done <- struct {
			result OverviewResult
			err    error
		}{result: result, err: err}
	}()
	select {
	case <-tracker.reached:
	case <-time.After(time.Second):
		t.Fatal("worker gate did not reach four concurrent nodes")
	}
	require.Equal(t, 4, tracker.maxDistinctNodes())
	close(tracker.block)
	completed := <-done
	result, err := completed.result, completed.err
	require.NoError(t, err)
	require.Len(t, result.Nodes, nodeCount)
	require.Equal(t, 4, tracker.maxDistinctNodes())
	require.Zero(t, tracker.activeNodeCount())
}

func TestOverviewNodeTimeoutIsPartialAndDoesNotBlockOtherNodes(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 1, 30, 0, time.UTC)
	registry := overviewRegistryFake{nodes: []*node.Node{
		overviewNode(1, node.StateActive, now),
		overviewNode(2, node.StateActive, now),
	}}
	block := make(chan struct{})
	tracker := &overviewCallTracker{}
	runtime := &overviewRuntimeFake{
		tracker:    tracker,
		statistics: map[int64]zlm.Statistic{1: {MediaSource: 2}, 2: {MediaSource: 8}},
		block:      block,
		blocked:    map[int64]bool{2: true},
	}
	media := &overviewMediaFake{tracker: tracker, media: map[int64][]zlm.MediaInfo{
		1: {{Online: true, Schema: "rtsp", VHost: "__defaultVhost__", App: "rtp", Stream: "healthy"}},
	}}
	service := NewOverviewService(OverviewDependencies{Registry: registry, Runtime: runtime, Media: media}, WithOverviewNodeTimeout(20*time.Millisecond), WithOverviewClock(func() time.Time { return now }))

	started := time.Now()
	result, err := service.GetOverview(context.Background())
	elapsed := time.Since(started)
	require.NoError(t, err)
	require.GreaterOrEqual(t, elapsed, 20*time.Millisecond)
	require.Less(t, elapsed, time.Second)
	require.True(t, result.Partial)
	require.Equal(t, []int64{1}, result.SuccessfulNodeIDs)
	require.Equal(t, []int64{2}, result.FailedNodeIDs)
	require.Equal(t, uint64(2), result.Metrics.MediaSourceCount)
	failed := overviewNodeResult(t, result, 2)
	require.NotNil(t, failed.Error)
	require.Equal(t, CodeUpstreamTimeout, failed.Error.Code)
	require.Zero(t, tracker.activeNodeCount())
	close(block)
}

func TestOverviewHeartbeatFreshnessIsSeparateFromRuntimeStatistic(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 2, 0, 0, time.UTC)
	registry := overviewRegistryFake{nodes: []*node.Node{{
		ID: 1, Name: "active", State: node.StateActive,
		Stats: node.Stats{MediaSourceCount: 777, SessionCount: 888, LastHeartbeatAt: now.Add(-2 * time.Minute)},
	}}}
	runtime := &overviewRuntimeFake{statistics: map[int64]zlm.Statistic{1: {MediaSource: 3, TcpSession: 4}}}
	media := &overviewMediaFake{}
	service := NewOverviewService(OverviewDependencies{Registry: registry, Runtime: runtime, Media: media},
		WithOverviewClock(func() time.Time { return now }), WithOverviewHeartbeatStaleAfter(time.Minute))

	result, err := service.GetOverview(context.Background())
	require.NoError(t, err)
	item := overviewNodeResult(t, result, 1)
	require.Equal(t, RuntimeFreshnessStale, item.HeartbeatFreshness)
	require.Equal(t, now.Add(-2*time.Minute), item.HeartbeatAsOf)
	require.Equal(t, uint64(3), item.Metrics.MediaSourceCount)
	require.Equal(t, uint64(4), item.Metrics.TCPSessionCount)
	require.NotEqual(t, uint64(777), item.Metrics.MediaSourceCount)
}

func TestOverviewStreamDistributionFiltersPaginatesAndExcludesFailedNodes(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 3, 0, 0, time.UTC)
	registry := overviewRegistryFake{nodes: []*node.Node{
		overviewNode(1, node.StateActive, now),
		overviewNode(2, node.StateActive, now),
	}}
	media := &overviewMediaFake{
		media: map[int64][]zlm.MediaInfo{
			1: {
				{Online: true, Schema: "rtsp", VHost: "__defaultVhost__", App: "rtp", Stream: "camera/1"},
				{Online: true, Schema: "hls", VHost: "__defaultVhost__", App: "rtp", Stream: "camera/1"},
			},
		},
		errors: map[int64]error{2: errors.New("media list unavailable")},
	}
	service := NewOverviewService(OverviewDependencies{Registry: registry, Runtime: &overviewRuntimeFake{}, Media: media}, WithOverviewClock(func() time.Time { return now }))

	page, err := service.ListStreams(context.Background(), StreamFilter{App: "rtp", Stream: "camera/1"}, PageRequest{Page: 1, PageSize: 1})
	require.NoError(t, err)
	require.True(t, page.Partial)
	require.Equal(t, int64(2), page.Total)
	require.True(t, page.Truncated)
	require.Len(t, page.List, 1)
	require.Equal(t, int64(1), page.List[0].NodeID)
	require.Equal(t, "camera/1", page.List[0].Media.Stream)
	require.Equal(t, []int64{1}, page.SuccessfulNodeIDs)
	require.Equal(t, []int64{2}, page.FailedNodeIDs)

	filtered, err := service.ListStreams(context.Background(), StreamFilter{
		NodeID: 1, Schema: "hls", Vhost: "__defaultVhost__", App: "rtp", Stream: "camera/1",
	}, PageRequest{Page: 0, PageSize: 999})
	require.NoError(t, err)
	require.False(t, filtered.Partial)
	require.Equal(t, int64(1), filtered.Total)
	require.Equal(t, 1, filtered.Page)
	require.Equal(t, MaxPageSize, filtered.PageSize)
	require.Len(t, filtered.List, 1)
	require.Equal(t, int64(1), filtered.List[0].NodeID)
	require.Equal(t, "hls", filtered.List[0].Media.Schema)
	require.Equal(t, "__defaultVhost__", filtered.List[0].Media.Vhost)
	require.Equal(t, "rtp", filtered.List[0].Media.App)
	require.Equal(t, "camera/1", filtered.List[0].Media.Stream)
	require.Equal(t, []int64{1}, filtered.SuccessfulNodeIDs)
	require.Empty(t, filtered.FailedNodeIDs)

	_, err = service.ListStreams(context.Background(), StreamFilter{NodeID: -1}, PageRequest{})
	var validationErr *ValidationError
	require.ErrorAs(t, err, &validationErr)

	clamped, err := service.ListStreams(context.Background(), StreamFilter{NodeID: 1}, PageRequest{Page: -3, PageSize: -10})
	require.NoError(t, err)
	require.Equal(t, 1, clamped.Page)
	require.Equal(t, DefaultPageSize, clamped.PageSize)
}

func TestOverviewStreamDTODoesNotExposeZLMURLOrSecretFields(t *testing.T) {
	registry := overviewRegistryFake{nodes: []*node.Node{overviewNode(1, node.StateActive, time.Now())}}
	media := &overviewMediaFake{media: map[int64][]zlm.MediaInfo{1: {{
		Online: true, Schema: "rtsp", VHost: "__defaultVhost__", App: "rtp", Stream: "safe",
		OriginURL: "rtsp://user:secret@example.invalid/live?token=secret",
	}}}}
	service := NewOverviewService(OverviewDependencies{Registry: registry, Runtime: &overviewRuntimeFake{}, Media: media})

	page, err := service.ListStreams(context.Background(), StreamFilter{}, PageRequest{Page: 1, PageSize: 20})
	require.NoError(t, err)
	body, err := json.Marshal(page)
	require.NoError(t, err)
	require.NotContains(t, string(body), "originUrl")
	require.NotContains(t, string(body), "secret")
	require.NotContains(t, string(body), "example.invalid")
}

func TestOverviewNodeRuntimeMaintenanceDoesNotCallReaders(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 4, 0, 0, time.UTC)
	registry := overviewRegistryFake{nodes: []*node.Node{overviewNode(4, node.StateMaintenance, now)}}
	runtime := &overviewRuntimeFake{}
	service := NewOverviewService(OverviewDependencies{Registry: registry, Runtime: runtime}, WithOverviewClock(func() time.Time { return now }))

	item, err := service.GetNodeRuntime(context.Background(), 4)
	require.NoError(t, err)
	require.Equal(t, RuntimeFreshnessMaintenance, item.Freshness)
	require.Zero(t, runtime.totalCalls())
}

func overviewNodeResult(t *testing.T, result OverviewResult, id int64) NodeRuntimeView {
	t.Helper()
	for _, item := range result.Nodes {
		if item.NodeID == id {
			return item
		}
	}
	t.Fatalf("node %d not found", id)
	return NodeRuntimeView{}
}

func overviewNode(id int64, state node.State, heartbeat time.Time) *node.Node {
	return &node.Node{ID: id, Name: fmt.Sprintf("node-%d", id), State: state, Stats: node.Stats{LastHeartbeatAt: heartbeat}}
}

type overviewRegistryFake struct {
	nodes []*node.Node
}

func (r overviewRegistryFake) List() []*node.Node {
	result := make([]*node.Node, 0, len(r.nodes))
	for _, item := range r.nodes {
		if item == nil {
			result = append(result, nil)
			continue
		}
		copy := *item
		result = append(result, &copy)
	}
	return result
}

type overviewCallTracker struct {
	mu          sync.Mutex
	active      map[int64]int
	max         int
	calls       map[string]int
	block       chan struct{}
	reached     chan struct{}
	reachedOnce sync.Once
}

func (t *overviewCallTracker) enter(kind string, nodeID int64) func() {
	t.mu.Lock()
	if t.active == nil {
		t.active = make(map[int64]int)
	}
	if t.calls == nil {
		t.calls = make(map[string]int)
	}
	t.active[nodeID]++
	t.calls[kind+":"+fmt.Sprint(nodeID)]++
	if len(t.active) > t.max {
		t.max = len(t.active)
		if t.max == 4 && t.reached != nil {
			t.reachedOnce.Do(func() { close(t.reached) })
		}
	}
	block := t.block
	t.mu.Unlock()
	if block != nil {
		<-block
	}
	return func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.active[nodeID]--
		if t.active[nodeID] == 0 {
			delete(t.active, nodeID)
		}
	}
}

func (t *overviewCallTracker) activeNodeCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.active)
}

func (t *overviewCallTracker) count(kind string, nodeID int64) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls[kind+":"+fmt.Sprint(nodeID)]
}

func (t *overviewCallTracker) maxDistinctNodes() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.max
}

type overviewRuntimeFake struct {
	tracker    *overviewCallTracker
	statistics map[int64]zlm.Statistic
	sessions   map[int64][]zlm.Session
	loads      map[int64]float64
	workloads  map[int64]float64
	errors     map[int64]error
	block      <-chan struct{}
	blocked    map[int64]bool
}

func (r *overviewRuntimeFake) enter(kind string, nodeID int64) func() {
	if r.tracker == nil {
		return func() {}
	}
	return r.tracker.enter(kind, nodeID)
}

func (r *overviewRuntimeFake) err(nodeID int64) error {
	if r.errors == nil {
		return nil
	}
	return r.errors[nodeID]
}

func (r *overviewRuntimeFake) wait(ctx context.Context, nodeID int64) error {
	if r.block == nil || !r.blocked[nodeID] {
		return nil
	}
	select {
	case <-r.block:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *overviewRuntimeFake) GetStatistic(ctx context.Context, nodeID int64) (zlm.Statistic, error) {
	defer r.enter("statistic", nodeID)()
	if err := r.err(nodeID); err != nil {
		return zlm.Statistic{}, err
	}
	if err := r.wait(ctx, nodeID); err != nil {
		return zlm.Statistic{}, err
	}
	return r.statistics[nodeID], nil
}

func (r *overviewRuntimeFake) GetAllSessions(ctx context.Context, nodeID int64, _ zlm.SessionFilter) ([]zlm.Session, error) {
	defer r.enter("sessions", nodeID)()
	if err := r.err(nodeID); err != nil {
		return nil, err
	}
	if err := r.wait(ctx, nodeID); err != nil {
		return nil, err
	}
	return append([]zlm.Session(nil), r.sessions[nodeID]...), nil
}

func (r *overviewRuntimeFake) GetThreadsLoad(ctx context.Context, nodeID int64) (float64, error) {
	defer r.enter("threads", nodeID)()
	if err := r.err(nodeID); err != nil {
		return 0, err
	}
	if err := r.wait(ctx, nodeID); err != nil {
		return 0, err
	}
	return r.loads[nodeID], nil
}

func (r *overviewRuntimeFake) GetWorkThreadsLoad(ctx context.Context, nodeID int64) (float64, error) {
	defer r.enter("workload", nodeID)()
	if err := r.err(nodeID); err != nil {
		return 0, err
	}
	if err := r.wait(ctx, nodeID); err != nil {
		return 0, err
	}
	return r.workloads[nodeID], nil
}

func (r *overviewRuntimeFake) totalCalls() int {
	if r.tracker == nil {
		return 0
	}
	r.tracker.mu.Lock()
	defer r.tracker.mu.Unlock()
	return len(r.tracker.calls)
}

type overviewMediaFake struct {
	tracker *overviewCallTracker
	media   map[int64][]zlm.MediaInfo
	errors  map[int64]error
}

func (r *overviewMediaFake) GetMediaList(_ context.Context, nodeID int64) ([]zlm.MediaInfo, error) {
	var done func()
	if r.tracker != nil {
		done = r.tracker.enter("media", nodeID)
		defer done()
	}
	if err := r.errors[nodeID]; err != nil {
		return nil, err
	}
	return append([]zlm.MediaInfo(nil), r.media[nodeID]...), nil
}
