package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/sipclient"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

func TestManagerKeepsPlatformsIsolatedAndOneTransactionInFlight(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)}
	store := newTestStore(platform(1, "a", 1), platform(2, "b", 1))
	clients := newTestClients()
	clients.client(1).registerStarted = make(chan struct{})
	clients.client(1).registerGate = make(chan struct{})
	clients.client(2).registerResult = sipclient.TransactionResult{StatusCode: 401}
	resources := &testResources{}
	manager := NewManager(Dependencies{Store: store, Clients: clients, Resources: resources, Clock: clock, Scheduler: newTestScheduler(), RetryDelay: time.Minute})

	require.NoError(t, manager.Reload(context.Background()))
	<-clients.client(1).registerStarted
	require.Eventually(t, func() bool { return clients.client(2).registerCalls() == 1 }, time.Second, time.Millisecond)
	require.Equal(t, 1, clients.client(2).registerCalls(), "A's blocked transaction must not hold B")

	close(clients.client(1).registerGate)
	require.Eventually(t, func() bool { return store.registrationSuccesses(1) == 1 }, time.Second, time.Millisecond)
	require.Equal(t, 1, resources.acquires(1))
	require.Equal(t, 1, resources.acquires(2))
	require.NoError(t, manager.Shutdown(context.Background()))
}

func TestManagerReloadReplacesChangesStopsRemovedAndStartsAdded(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)}
	store := newTestStore(platform(1, "a", 1), platform(2, "b", 1))
	clients := newTestClients()
	resources := &testResources{}
	manager := NewManager(Dependencies{Store: store, Clients: clients, Resources: resources, Clock: clock, Scheduler: newTestScheduler(), RetryDelay: time.Minute})
	require.NoError(t, manager.Reload(context.Background()))
	require.Eventually(t, func() bool { return clients.client(1).registerCalls() == 1 && clients.client(2).registerCalls() == 1 }, time.Second, time.Millisecond)

	changed := platform(1, "a", 2)
	changed.Host = "198.51.100.10"
	store.setEnabled(changed, platform(3, "c", 1))
	require.NoError(t, manager.Reload(context.Background()))

	require.Eventually(t, func() bool {
		return clients.client(1).registerCalls() == 2 && clients.client(2).logoutCalls() == 1 && clients.client(3).registerCalls() == 1
	}, time.Second, time.Millisecond)
	require.Equal(t, 2, resources.acquires(1), "changed revision replaces actor")
	require.Equal(t, 1, resources.releases(1), "old A resources are released before replacement")
	require.Equal(t, 1, resources.releases(2), "removed B releases resources")
	require.ElementsMatch(t, []uint64{1, 3}, manager.PlatformIDs())
	require.NoError(t, manager.Shutdown(context.Background()))
}

func TestShutdownHonorsDeadlineWhenRegisterDoesNotReturn(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)}
	store := newTestStore(platform(1, "a", 1))
	clients := newTestClients()
	clients.client(1).registerStarted = make(chan struct{})
	clients.client(1).registerGate = make(chan struct{})
	resources := &testResources{}
	manager := NewManager(Dependencies{Store: store, Clients: clients, Resources: resources, Clock: clock, Scheduler: newTestScheduler(), RetryDelay: time.Minute})
	require.NoError(t, manager.Reload(context.Background()))
	<-clients.client(1).registerStarted

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, manager.Shutdown(ctx), context.Canceled)
	require.Eventually(t, func() bool { return resources.releases(1) == 1 }, time.Second, time.Millisecond)
	close(clients.client(1).registerGate)
}

func TestFirstKeepaliveIsScheduledShortlyAfterRegistrationSuccess(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)}
	store := newTestStore(platform(1, "a", 1))
	clients := newTestClients()
	resources := &testResources{}
	scheduler := &recordingScheduler{}
	manager := NewManager(Dependencies{Store: store, Clients: clients, Resources: resources, Clock: clock, Scheduler: scheduler, RetryDelay: time.Minute})

	require.NoError(t, manager.Reload(context.Background()))
	require.Eventually(t, func() bool { return store.registrationSuccesses(1) == 1 }, time.Second, time.Millisecond)
	require.Eventually(t, func() bool {
		return scheduler.hasSchedule(time.Second) && scheduler.hasSchedule(sipclient.RefreshDelay(3600))
	}, time.Second, time.Millisecond)
	require.Equal(t, 2, scheduler.count(), "registration success must schedule exactly refresh + first keepalive")
	require.NoError(t, manager.Shutdown(context.Background()))
}

func TestActorUsesGrantedRegisterExpiryForPersistenceAndRefresh(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)}
	store := newTestStore(platform(1, "a", 1))
	clients := newTestClients()
	clients.client(1).registerResult = sipclient.TransactionResult{
		StatusCode: 200, Success: true, GrantedExpiresFound: true, GrantedExpires: 1200,
	}
	scheduler := &recordingScheduler{}
	manager := NewManager(Dependencies{Store: store, Clients: clients, Resources: &testResources{}, Clock: clock, Scheduler: scheduler, RetryDelay: time.Minute})
	require.NoError(t, manager.Reload(context.Background()))
	require.Eventually(t, func() bool { return store.registrationSuccesses(1) == 1 }, time.Second, time.Millisecond)
	require.True(t, scheduler.hasSchedule(sipclient.RefreshDelay(1200)))
	require.False(t, scheduler.hasSchedule(sipclient.RefreshDelay(3600)))
	require.NoError(t, manager.Shutdown(context.Background()))
}

func TestActorDoesNotDropMaintenanceCommandWhileTransactionIsInFlight(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)}
	store := newTestStore(platform(1, "a", 1))
	clients := newTestClients()
	resources := &testResources{}
	manager := NewManager(Dependencies{Store: store, Clients: clients, Resources: resources, Clock: clock, Scheduler: newTestScheduler(), RetryDelay: time.Minute})
	require.NoError(t, manager.Reload(context.Background()))
	require.Eventually(t, func() bool { return store.registrationSuccesses(1) == 1 }, time.Second, time.Millisecond)

	client := clients.client(1)
	client.mu.Lock()
	client.registerStarted = make(chan struct{})
	client.registerGate = make(chan struct{})
	client.mu.Unlock()
	manager.mu.RLock()
	actor := manager.actors[1]
	manager.mu.RUnlock()
	require.NotNil(t, actor)

	actor.enqueue(context.Background(), commandRefresh)
	require.Eventually(t, func() bool {
		client.mu.Lock()
		defer client.mu.Unlock()
		return client.registerStarted != nil && client.registerCount >= 2
	}, time.Second, time.Millisecond)
	actor.enqueue(context.Background(), commandKeepalive)
	close(client.registerGate)

	require.Eventually(t, func() bool { return client.keepaliveCalls() == 1 }, time.Second, time.Millisecond)
	require.NoError(t, manager.Shutdown(context.Background()))
}

func TestActorReRegistersAfterThreeHeartbeatTimeouts(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)}
	store := newTestStore(platform(1, "a", 1))
	clients := newTestClients()
	client := clients.client(1)
	client.keepaliveResults = []sipclient.TransactionResult{
		{TransportErr: context.DeadlineExceeded},
		{TransportErr: context.DeadlineExceeded},
		{TransportErr: context.DeadlineExceeded},
	}
	manager := NewManager(Dependencies{Store: store, Clients: clients, Resources: &testResources{}, Clock: clock, Scheduler: newTestScheduler(), RetryDelay: time.Minute})
	require.NoError(t, manager.Reload(context.Background()))
	require.Eventually(t, func() bool { return store.registrationSuccesses(1) == 1 }, time.Second, time.Millisecond)
	manager.mu.RLock()
	actor := manager.actors[1]
	manager.mu.RUnlock()
	require.NotNil(t, actor)

	for i := 0; i < 3; i++ {
		actor.enqueue(context.Background(), commandKeepalive)
		require.Eventually(t, func() bool { return client.keepaliveCalls() >= i+1 }, time.Second, time.Millisecond)
		// Let the result event return to the actor loop before the next command.
		require.Eventually(t, func() bool { return store.heartbeatFailures(1) >= i+1 }, time.Second, time.Millisecond)
	}
	require.Eventually(t, func() bool { return client.registerCalls() == 2 }, time.Second, time.Millisecond)
	require.NoError(t, manager.Shutdown(context.Background()))
}

func TestActorDoesNotCombineHeartbeatRejectionWithLaterTimeouts(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)}
	store := newTestStore(platform(1, "a", 1))
	clients := newTestClients()
	client := clients.client(1)
	client.keepaliveResults = []sipclient.TransactionResult{
		{StatusCode: 403},
		{TransportErr: context.DeadlineExceeded},
		{TransportErr: context.DeadlineExceeded},
	}
	manager := NewManager(Dependencies{Store: store, Clients: clients, Resources: &testResources{}, Clock: clock, Scheduler: newTestScheduler(), RetryDelay: time.Minute})
	require.NoError(t, manager.Reload(context.Background()))
	require.Eventually(t, func() bool { return store.registrationSuccesses(1) == 1 }, time.Second, time.Millisecond)
	manager.mu.RLock()
	actor := manager.actors[1]
	manager.mu.RUnlock()
	require.NotNil(t, actor)

	for i := 0; i < 3; i++ {
		actor.enqueue(context.Background(), commandKeepalive)
		require.Eventually(t, func() bool { return client.keepaliveCalls() >= i+1 }, time.Second, time.Millisecond)
		require.Eventually(t, func() bool { return store.heartbeatFailures(1) >= i+1 }, time.Second, time.Millisecond)
	}
	// The rejection breaks the timeout streak, so two later timeouts are not
	// enough to invalidate the registration.
	require.Equal(t, 1, client.registerCalls())
	require.NoError(t, manager.Shutdown(context.Background()))
}

func TestActorAppliesAutoNegotiatedRegisterVersion(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)}
	platform := platform(1, "a", 1)
	platform.ProfileOverride = model.CascadeProfileOverrideAuto
	platform.EffectiveVersion = string(protocol.Version2016)
	store := newTestStore(platform)
	clients := newTestClients()
	clients.client(1).registerResult = sipclient.TransactionResult{
		StatusCode: 200, Success: true, ReportedGBVersion: "3.0", ReportedGBVersionFound: true,
		NegotiatedVersion: protocol.Version2022, NegotiatedVersionFound: true,
	}
	manager := NewManager(Dependencies{Store: store, Clients: clients, Resources: &testResources{}, Clock: clock, Scheduler: newTestScheduler(), RetryDelay: time.Minute})
	require.NoError(t, manager.Reload(context.Background()))
	require.Eventually(t, func() bool { return store.registrationSuccesses(1) == 1 }, time.Second, time.Millisecond)
	require.Equal(t, string(protocol.Version2022), store.negotiatedVersion(1))
	manager.mu.RLock()
	actor := manager.actors[1]
	manager.mu.RUnlock()
	require.Equal(t, string(protocol.Version2022), actor.platform.EffectiveVersion)
	require.Equal(t, "negotiated", actor.platform.EffectiveVersionFrom)
	require.NoError(t, manager.Shutdown(context.Background()))
}

func TestInvalidPlatformDoesNotBlockOtherPlatforms(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)}
	store := newTestStore(platform(1, "broken", 1), platform(2, "healthy", 1))
	clients := newTestClients()
	clients.errs[1] = errors.New("missing cascade credential")
	resources := &testResources{}
	manager := NewManager(Dependencies{Store: store, Clients: clients, Resources: resources, Clock: clock, Scheduler: newTestScheduler(), RetryDelay: time.Minute})

	require.NoError(t, manager.Reload(context.Background()))
	require.Eventually(t, func() bool { return clients.client(2).registerCalls() == 1 }, time.Second, time.Millisecond)
	require.Equal(t, []uint64{2}, manager.PlatformIDs(), "invalid A must not prevent healthy B from starting")
	require.EqualError(t, manager.PlatformError(1), "missing cascade credential")
	require.Equal(t, "config", store.lastFailureCode(1))
	require.Equal(t, 0, resources.acquires(1))
	require.Equal(t, 1, resources.acquires(2))
	require.NoError(t, manager.Shutdown(context.Background()))
}

type testClock struct{ now time.Time }

func (c *testClock) Now() time.Time { return c.now }

type testStore struct {
	mu                 sync.Mutex
	enabled            []model.GbCascadePlatform
	registrationOK     map[uint64]int
	registrationFailed map[uint64]string
	heartbeatFailed    map[uint64]int
	negotiated         map[uint64]string
}

func newTestStore(platforms ...model.GbCascadePlatform) *testStore {
	return &testStore{enabled: platforms, registrationOK: make(map[uint64]int), registrationFailed: make(map[uint64]string), heartbeatFailed: make(map[uint64]int), negotiated: make(map[uint64]string)}
}

func (s *testStore) ListEnabledPlatforms(context.Context) ([]model.GbCascadePlatform, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]model.GbCascadePlatform(nil), s.enabled...), nil
}

func (s *testStore) RecordRegistrationSuccess(_ context.Context, id uint64, _, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registrationOK[id]++
	return nil
}

func (s *testStore) RecordRegistrationVersion(_ context.Context, id uint64, _, effective, _ string, _ time.Time) error {
	s.mu.Lock()
	s.negotiated[id] = effective
	s.mu.Unlock()
	return nil
}

func (s *testStore) RecordRegistrationExpired(context.Context, uint64, time.Time) error { return nil }

func (s *testStore) RecordRegistrationFailure(_ context.Context, id uint64, code, _ string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registrationFailed[id] = code
	return nil
}

func (s *testStore) RecordHeartbeatSuccess(context.Context, uint64, time.Time) error { return nil }

func (s *testStore) RecordHeartbeatFailure(_ context.Context, id uint64, _, _ string, _ time.Time) error {
	s.mu.Lock()
	s.heartbeatFailed[id]++
	s.mu.Unlock()
	return nil
}

func (s *testStore) heartbeatFailures(id uint64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.heartbeatFailed[id]
}

func (s *testStore) registrationSuccesses(id uint64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.registrationOK[id]
}

func (s *testStore) lastFailureCode(id uint64) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.registrationFailed[id]
}

func (s *testStore) negotiatedVersion(id uint64) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.negotiated[id]
}

func (s *testStore) setEnabled(platforms ...model.GbCascadePlatform) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled = append([]model.GbCascadePlatform(nil), platforms...)
}

type testClients struct {
	mu    sync.Mutex
	items map[uint64]*testClient
	errs  map[uint64]error
}

func newTestClients() *testClients {
	return &testClients{items: make(map[uint64]*testClient), errs: make(map[uint64]error)}
}

func (f *testClients) NewClient(platform model.GbCascadePlatform) (PlatformClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.errs[platform.ID]; err != nil {
		return nil, err
	}
	if f.items[platform.ID] == nil {
		f.items[platform.ID] = &testClient{registerResult: sipclient.TransactionResult{StatusCode: 200, Success: true}, logoutResult: sipclient.TransactionResult{StatusCode: 200, Success: true}}
	}
	return f.items[platform.ID], nil
}

func (f *testClients) client(id uint64) *testClient {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.items[id] == nil {
		f.items[id] = &testClient{registerResult: sipclient.TransactionResult{StatusCode: 200, Success: true}, logoutResult: sipclient.TransactionResult{StatusCode: 200, Success: true}}
	}
	return f.items[id]
}

type testClient struct {
	mu               sync.Mutex
	registerResult   sipclient.TransactionResult
	logoutResult     sipclient.TransactionResult
	registerStarted  chan struct{}
	registerGate     chan struct{}
	registerCount    int
	logoutCount      int
	keepaliveCount   int
	keepaliveResults []sipclient.TransactionResult
}

func (c *testClient) Register(ctx context.Context, _ int, _ string) sipclient.TransactionResult {
	c.mu.Lock()
	c.registerCount++
	started, gate, result := c.registerStarted, c.registerGate, c.registerResult
	c.mu.Unlock()
	if started != nil {
		select {
		case <-started:
		default:
			close(started)
		}
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return sipclient.TransactionResult{TransportErr: ctx.Err()}
		}
	}
	return result
}

func (c *testClient) Logout(context.Context, string) sipclient.TransactionResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logoutCount++
	return c.logoutResult
}

func (c *testClient) Keepalive(context.Context, string) sipclient.TransactionResult {
	c.mu.Lock()
	c.keepaliveCount++
	index := c.keepaliveCount - 1
	var result sipclient.TransactionResult
	if index < len(c.keepaliveResults) {
		result = c.keepaliveResults[index]
	} else {
		result = sipclient.TransactionResult{StatusCode: 200, Success: true}
	}
	c.mu.Unlock()
	return result
}

func (c *testClient) keepaliveCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.keepaliveCount
}

func (c *testClient) registerCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.registerCount
}

func (c *testClient) logoutCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.logoutCount
}

type testResources struct {
	mu       sync.Mutex
	acquired map[uint64]int
	released map[uint64]int
}

func (r *testResources) Acquire(platform model.GbCascadePlatform) (func() error, error) {
	r.mu.Lock()
	if r.acquired == nil {
		r.acquired = make(map[uint64]int)
	}
	if r.released == nil {
		r.released = make(map[uint64]int)
	}
	r.acquired[platform.ID]++
	r.mu.Unlock()
	var once sync.Once
	return func() error {
		once.Do(func() {
			r.mu.Lock()
			r.released[platform.ID]++
			r.mu.Unlock()
		})
		return nil
	}, nil
}

func (r *testResources) acquires(id uint64) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.acquired[id]
}
func (r *testResources) releases(id uint64) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.released[id]
}

type testScheduler struct{}

func newTestScheduler() *testScheduler { return &testScheduler{} }

func (*testScheduler) Schedule(time.Duration, func()) Timer { return testTimer{} }

type recordingScheduler struct {
	mu     sync.Mutex
	delays []time.Duration
}

func (s *recordingScheduler) Schedule(delay time.Duration, _ func()) Timer {
	s.mu.Lock()
	s.delays = append(s.delays, delay)
	s.mu.Unlock()
	return testTimer{}
}

func (s *recordingScheduler) hasSchedule(delay time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, recorded := range s.delays {
		if recorded == delay {
			return true
		}
	}
	return false
}

func (s *recordingScheduler) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.delays)
}

type testTimer struct{}

func (testTimer) Stop() {}

func platform(id uint64, name string, revision uint64) model.GbCascadePlatform {
	return model.GbCascadePlatform{ID: id, Name: name, Enabled: true, ConfigRevision: revision, RegisterExpires: 3600, KeepaliveInterval: 60}
}
