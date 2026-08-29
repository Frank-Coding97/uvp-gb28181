package heartbeat_test

import (
	"context"
	"sync"
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

type t13Scheduler struct{ calls int }

func (s *t13Scheduler) ScheduleConfigConvergence(int64) bool {
	s.calls++
	return true
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
	require.Equal(t, 0, scheduler.calls, "restart coordinator owns verified convergence")
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
	require.Equal(t, 0, scheduler.calls, "waiting restart must suppress generic convergence")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("restart coordinator did not own convergence")
	}
	require.True(t, reg.SetAutoOnDemandReady(id, true))
}
