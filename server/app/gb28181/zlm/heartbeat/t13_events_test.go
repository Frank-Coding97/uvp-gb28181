package heartbeat_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/heartbeat"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/service"
)

type t13Events struct {
	mu        sync.Mutex
	offline   []int64
	heartbeat []int64
	pending   bool
}

func (e *t13Events) OnNodeOffline(id int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.offline = append(e.offline, id)
}

func (e *t13Events) OnNodeHeartbeat(id int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.heartbeat = append(e.heartbeat, id)
}

func (e *t13Events) RestartPending(int64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.pending
}

type t13Scheduler struct {
	mu    sync.Mutex
	calls int
}

func (s *t13Scheduler) ScheduleConfigConvergence(int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return true
}

func (s *t13Scheduler) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func TestWatcherT13_NotifiesOnlyAfterOfflinePersistence(t *testing.T) {
	repo := newMemoryRepo()
	reg := node.NewRegistry(repo)
	clock := newFakeClock(time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC))
	n, err := reg.Add(context.Background(), node.Node{Name: "n", Host: "zlm", APIPort: 18080, APISecret: "s", MediaServerUUID: "u", State: node.StateActive})
	require.NoError(t, err)
	reg.UpdateStats("u", node.Stats{LastHeartbeatAt: clock.Now()})
	events := &t13Events{}
	w := heartbeat.NewWatcherWithNotifier(reg, clock, time.Second, time.Second, events)
	clock.Advance(2 * time.Second)
	w.Tick()
	events.mu.Lock()
	require.Equal(t, []int64{n.ID}, events.offline)
	events.mu.Unlock()
}

func TestCollectorT13_NotifiesOfflineToActiveHeartbeat(t *testing.T) {
	reg, id := setupRegistry(t, "u")
	require.NoError(t, reg.MarkOffline(context.Background(), id))
	events := &t13Events{}
	c := heartbeat.NewCollectorWithNotifier(reg, nil, events)
	require.NoError(t, c.Receive([]byte(`{"mediaServerId":"u","data":{"MediaSource":1}}`)))
	events.mu.Lock()
	require.Equal(t, []int64{id}, events.heartbeat)
	events.mu.Unlock()
}

func TestCollectorT13_RestartNotifierOwnsConvergence(t *testing.T) {
	reg, id := setupRegistry(t, "u")
	require.NoError(t, reg.MarkOffline(context.Background(), id))
	events := &t13Events{pending: true}
	scheduler := &t13Scheduler{}
	c := heartbeat.NewCollectorWithNotifier(reg, scheduler, events)
	require.NoError(t, c.Receive([]byte(`{"mediaServerId":"u","data":{"MediaSource":1}}`)))
	events.mu.Lock()
	require.Equal(t, []int64{id}, events.heartbeat)
	events.mu.Unlock()
	require.Equal(t, 0, scheduler.count(), "restart coordinator owns verified convergence")
}

func TestCollectorT13_FastRestartActiveHeartbeatReachesReady(t *testing.T) {
	reg, id := setupRegistry(t, "u")
	coordinator := service.NewRestartCoordinator(reg, time.Second)
	accepted, err := coordinator.Begin(id)
	require.NoError(t, err)
	require.True(t, coordinator.AdvanceToWaitingOffline(id, accepted.Generation))
	coordinator.SetConverger(func(context.Context, int64) error {
		reg.SetAutoOnDemandReady(id, true)
		return nil
	})

	coordinator.OnNodeStarted(id)
	operation, ok := coordinator.Get(id)
	require.True(t, ok)
	require.Equal(t, service.RestartStatusWaitingHeartbeat, operation.Status)

	collector := heartbeat.NewCollectorWithNotifier(reg, nil, coordinator)
	require.NoError(t, collector.Receive([]byte(`{"mediaServerId":"u","data":{"MediaSource":1}}`)))
	require.Eventually(t, func() bool {
		current, ok := coordinator.Get(id)
		return ok && current.Status == service.RestartStatusReady
	}, time.Second, time.Millisecond)
}

func TestCollectorT13_ActiveHeartbeatBeforeStartedRemainsWaitingOffline(t *testing.T) {
	reg, id := setupRegistry(t, "u")
	coordinator := service.NewRestartCoordinator(reg, time.Second)
	accepted, err := coordinator.Begin(id)
	require.NoError(t, err)
	require.True(t, coordinator.AdvanceToWaitingOffline(id, accepted.Generation))
	scheduler := &t13Scheduler{}
	collector := heartbeat.NewCollectorWithNotifier(reg, scheduler, coordinator)

	require.NoError(t, collector.Receive([]byte(`{"mediaServerId":"u","data":{"MediaSource":1}}`)))
	operation, ok := coordinator.Get(id)
	require.True(t, ok)
	require.Equal(t, service.RestartStatusWaitingOffline, operation.Status)
	require.Equal(t, 0, scheduler.count(), "pending restart must suppress generic convergence before started")
}

func TestCollectorT13_NonPendingActiveHeartbeatDoesNotNotifyRestart(t *testing.T) {
	reg, id := setupRegistry(t, "u")
	require.True(t, reg.SetAutoOnDemandReady(id, true))
	events := &t13Events{}
	scheduler := &t13Scheduler{}
	collector := heartbeat.NewCollectorWithNotifier(reg, scheduler, events)
	payload := []byte(`{"mediaServerId":"u","data":{"MediaSource":1}}`)

	require.NoError(t, collector.Receive(payload))
	require.NoError(t, collector.Receive(payload))
	events.mu.Lock()
	require.Empty(t, events.heartbeat, "ordinary active heartbeat must not notify restart coordinator")
	events.mu.Unlock()
	require.Equal(t, 0, scheduler.count(), "ready ordinary node must not reschedule convergence")
}

func TestCollectorT13_RestartHeartbeatRaceStartsOneConvergence(t *testing.T) {
	reg, id := setupRegistry(t, "u")
	coordinator := service.NewRestartCoordinator(reg, time.Second)
	accepted, err := coordinator.Begin(id)
	require.NoError(t, err)
	require.True(t, coordinator.AdvanceToWaitingOffline(id, accepted.Generation))
	coordinator.OnNodeStarted(id)

	entered := make(chan struct{})
	release := make(chan struct{})
	var convergenceCalls atomic.Int32
	var enteredOnce sync.Once
	coordinator.SetConverger(func(context.Context, int64) error {
		convergenceCalls.Add(1)
		enteredOnce.Do(func() { close(entered) })
		<-release
		return nil
	})
	scheduler := &t13Scheduler{}
	collector := heartbeat.NewCollectorWithNotifier(reg, scheduler, coordinator)
	payload := []byte(`{"mediaServerId":"u","data":{"MediaSource":1}}`)

	const receivers = 32
	var wg sync.WaitGroup
	errs := make(chan error, receivers)
	wg.Add(receivers)
	for i := 0; i < receivers; i++ {
		go func() {
			defer wg.Done()
			errs <- collector.Receive(payload)
		}()
	}
	wg.Wait()
	close(errs)
	for receiveErr := range errs {
		require.NoError(t, receiveErr)
	}
	require.Eventually(t, func() bool {
		current, ok := coordinator.Get(id)
		return ok && current.Status == service.RestartStatusConverging
	}, time.Second, time.Millisecond)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("restart convergence did not start")
	}
	require.Equal(t, int32(1), convergenceCalls.Load(), "one restart generation may claim convergence")
	require.Equal(t, 0, scheduler.count(), "pending restart must own convergence")
	require.True(t, reg.SetAutoOnDemandReady(id, true))
	close(release)
	require.Eventually(t, func() bool {
		current, ok := coordinator.Get(id)
		return ok && current.Status == service.RestartStatusReady
	}, time.Second, time.Millisecond)
}

func TestCollectorT13_RestartCoordinatorDoesNotRaceGenericScheduler(t *testing.T) {
	reg, id := setupRegistry(t, "u")
	require.NoError(t, reg.MarkOffline(context.Background(), id))
	coordinator := service.NewRestartCoordinator(reg, time.Second)
	accepted, err := coordinator.Begin(id)
	require.NoError(t, err)
	require.True(t, coordinator.AdvanceToWaitingOffline(id, accepted.Generation))
	require.True(t, coordinator.MarkOfflineForGeneration(id, accepted.Generation))
	started := make(chan struct{})
	release := make(chan struct{})
	coordinator.SetConverger(func(context.Context, int64) error {
		close(started)
		<-release
		return nil
	})
	defer close(release)
	scheduler := &t13Scheduler{}
	c := heartbeat.NewCollectorWithNotifier(reg, scheduler, coordinator)
	require.NoError(t, c.Receive([]byte(`{"mediaServerId":"u","data":{"MediaSource":1}}`)))
	require.Equal(t, 0, scheduler.count(), "waiting restart must suppress generic convergence")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("restart coordinator did not own convergence")
	}
	require.True(t, reg.SetAutoOnDemandReady(id, true))
}
