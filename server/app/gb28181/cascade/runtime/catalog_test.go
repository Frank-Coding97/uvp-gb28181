package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/sipclient"
)

type testCatalogPusher struct {
	mu        sync.Mutex
	calls     map[uint64]int
	gate      chan struct{}
	cancelled chan struct{}
	failures  int
}

func (p *testCatalogPusher) PushCatalog(ctx context.Context, id uint64) (int, int, error) {
	p.mu.Lock()
	p.calls[id]++
	fail := p.failures > 0
	if fail {
		p.failures--
	}
	p.mu.Unlock()
	if fail {
		return 0, 0, errors.New("temporary MESSAGE failure")
	}
	if p.gate != nil {
		select {
		case <-p.gate:
		case <-ctx.Done():
			if p.cancelled != nil {
				close(p.cancelled)
			}
			return 0, 0, ctx.Err()
		}
	}
	return 1, 1, nil
}

type catalogRetryScheduler struct {
	mu    sync.Mutex
	tasks map[time.Duration]func()
}

func (s *catalogRetryScheduler) Schedule(delay time.Duration, fn func()) Timer {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[delay] = fn
	return testTimer{}
}
func (s *catalogRetryScheduler) fire(delay time.Duration) bool {
	s.mu.Lock()
	fn := s.tasks[delay]
	delete(s.tasks, delay)
	s.mu.Unlock()
	if fn == nil {
		return false
	}
	fn()
	return true
}

func TestAutoCatalogRetriesFailedPushAndRepublishesAfterRegistrationRecovery(t *testing.T) {
	a := platform(1, "auto", 1)
	a.AutoPushCatalog = true
	store, clients := newTestStore(a), newTestClients()
	pusher := &testCatalogPusher{calls: make(map[uint64]int), failures: 1}
	scheduler := &catalogRetryScheduler{tasks: make(map[time.Duration]func())}
	m := NewManager(Dependencies{Store: store, Clients: clients, CatalogPusher: pusher, Scheduler: scheduler, RetryDelay: time.Minute})
	require.NoError(t, m.Reload(context.Background()))
	t.Cleanup(func() { require.NoError(t, m.Shutdown(context.Background())) })
	require.Eventually(t, func() bool { return scheduler.fire(time.Minute) }, time.Second, time.Millisecond)
	require.Eventually(t, func() bool { return pusher.count(1) == 2 }, time.Second, time.Millisecond)
	clients.client(1).mu.Lock()
	clients.client(1).registerResult = sipclient.TransactionResult{StatusCode: 503}
	clients.client(1).mu.Unlock()
	m.mu.RLock()
	actor := m.actors[1]
	m.mu.RUnlock()
	actor.enqueue(context.Background(), commandRefresh)
	require.Eventually(t, func() bool { return store.lastFailureCode(1) == "503" }, time.Second, time.Millisecond)
	clients.client(1).mu.Lock()
	clients.client(1).registerResult = sipclient.TransactionResult{Success: true, StatusCode: 200}
	clients.client(1).mu.Unlock()
	require.Eventually(t, func() bool { return scheduler.fire(time.Minute) }, time.Second, time.Millisecond)
	require.Eventually(t, func() bool { return pusher.count(1) == 3 }, time.Second, time.Millisecond)
}

func (p *testCatalogPusher) count(id uint64) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls[id]
}

func TestAutoCatalogPushAfterRegistrationAndProjectionChangesOnly(t *testing.T) {
	a, b, c := platform(1, "auto", 1), platform(2, "manual", 1), platform(3, "failed", 1)
	a.AutoPushCatalog, c.AutoPushCatalog = true, true
	store := newTestStore(a, b, c)
	clients := newTestClients()
	clients.client(3).registerResult = sipclient.TransactionResult{StatusCode: 403}
	pusher := &testCatalogPusher{calls: make(map[uint64]int)}
	m := NewManager(Dependencies{Store: store, Clients: clients, CatalogPusher: pusher, Scheduler: newTestScheduler()})
	require.NoError(t, m.Reload(context.Background()))
	t.Cleanup(func() { require.NoError(t, m.Shutdown(context.Background())) })
	require.Eventually(t, func() bool { return pusher.count(1) == 1 && clients.client(3).registerCalls() == 1 }, time.Second, time.Millisecond)
	require.Zero(t, pusher.count(2))
	require.Zero(t, pusher.count(3))
	require.NoError(t, m.Reload(context.Background()))
	m.mu.RLock()
	actor := m.actors[1]
	m.mu.RUnlock()
	actor.enqueue(context.Background(), commandRefresh)
	require.Eventually(t, func() bool { return store.registrationSuccesses(1) == 2 }, time.Second, time.Millisecond)
	a.ProjectionRevision++
	store.setEnabled(a, b, c)
	require.NoError(t, m.Reload(context.Background()))
	require.Eventually(t, func() bool { return pusher.count(1) == 2 }, time.Second, time.Millisecond)
	require.Equal(t, 2, clients.client(1).registerCalls(), "sharing changes must not re-register")
}

func TestAutoCatalogPushDoesNotBlockHeartbeatAndCancelsOnStop(t *testing.T) {
	a := platform(1, "auto", 1)
	a.AutoPushCatalog = true
	store, clients := newTestStore(a), newTestClients()
	pusher := &testCatalogPusher{calls: make(map[uint64]int), gate: make(chan struct{}), cancelled: make(chan struct{})}
	m := NewManager(Dependencies{Store: store, Clients: clients, CatalogPusher: pusher, Scheduler: newTestScheduler()})
	require.NoError(t, m.Reload(context.Background()))
	require.Eventually(t, func() bool { return pusher.count(1) == 1 }, time.Second, time.Millisecond)
	m.mu.RLock()
	actor := m.actors[1]
	m.mu.RUnlock()
	actor.enqueue(context.Background(), commandKeepalive)
	require.Eventually(t, func() bool { return clients.client(1).keepaliveCalls() == 1 }, time.Second, time.Millisecond)
	require.NoError(t, m.Shutdown(context.Background()))
	select {
	case <-pusher.cancelled:
	case <-time.After(time.Second):
		t.Fatal("catalog push did not cancel")
	}
}
