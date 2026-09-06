package casbinhelper

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type blockingPolicyAdapter struct {
	block     atomic.Bool
	calls     atomic.Int32
	started   chan struct{}
	startOnce sync.Once
	release   chan struct{}
}

func newBlockingPolicyAdapter() *blockingPolicyAdapter {
	return &blockingPolicyAdapter{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (a *blockingPolicyAdapter) LoadPolicy(model.Model) error {
	a.calls.Add(1)
	if a.block.Load() {
		a.startOnce.Do(func() { close(a.started) })
		<-a.release
	}
	return nil
}

func (a *blockingPolicyAdapter) SavePolicy(model.Model) error { return nil }
func (a *blockingPolicyAdapter) AddPolicy(string, string, []string) error {
	return nil
}
func (a *blockingPolicyAdapter) RemovePolicy(string, string, []string) error {
	return nil
}
func (a *blockingPolicyAdapter) RemoveFilteredPolicy(string, string, int, ...string) error {
	return nil
}

type intervalAutoLoader interface {
	startAutoLoadPolicyWithInterval(time.Duration)
}

type contextCloser interface {
	CloseContext(context.Context) error
}

func newLoggingPolicyEnforcer(t *testing.T, adapter persist.Adapter) *casbin.Enforcer {
	t.Helper()
	m, err := model.NewModelFromString(testModelConfig)
	require.NoError(t, err)
	enforcer, err := casbin.NewEnforcer(m, adapter)
	require.NoError(t, err)
	return enforcer
}

func startAutoLoadForTest(t *testing.T, helper *CasbinHelper, interval time.Duration) {
	t.Helper()
	loader, ok := any(helper).(intervalAutoLoader)
	if !ok {
		t.Fatalf("CasbinHelper does not expose the testable interval auto-loader")
	}
	loader.startAutoLoadPolicyWithInterval(interval)
}

func closeCasbinContext(t *testing.T, helper *CasbinHelper, ctx context.Context) error {
	t.Helper()
	closer, ok := any(helper).(contextCloser)
	if !ok {
		t.Fatalf("CasbinHelper does not expose CloseContext")
	}
	return closer.CloseContext(ctx)
}

func installCasbinObserver(t *testing.T) *observer.ObservedLogs {
	t.Helper()
	core, observed := observer.New(zap.DebugLevel)
	previous := app.ZapLog
	app.ZapLog = zap.New(core)
	t.Cleanup(func() { app.ZapLog = previous })
	return observed
}

func countCasbinEvent(observed *observer.ObservedLogs, event string) int {
	count := 0
	for _, entry := range observed.All() {
		if entry.ContextMap()["event"] == event {
			count++
		}
	}
	return count
}

func waitForCasbinEvent(t *testing.T, observed *observer.ObservedLogs, event string) observer.LoggedEntry {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, entry := range observed.All() {
			if entry.ContextMap()["event"] == event {
				return entry
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("Casbin log event not found: %s", event)
	return observer.LoggedEntry{}
}

func TestLoggingCasbinCloseContextWaitsForInFlightPolicyReload(t *testing.T) {
	observed := installCasbinObserver(t)
	adapter := newBlockingPolicyAdapter()
	helper := &CasbinHelper{enforcer: newLoggingPolicyEnforcer(t, adapter)}
	adapter.block.Store(true)
	startAutoLoadForTest(t, helper, time.Millisecond)

	select {
	case <-adapter.started:
	case <-time.After(2 * time.Second):
		t.Fatal("policy reload did not enter the blocking adapter")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, closeCasbinContext(t, helper, ctx), context.DeadlineExceeded)
	longResult := make(chan error, 1)
	go func() {
		longResult <- closeCasbinContext(t, helper, context.Background())
	}()
	select {
	case <-time.After(20 * time.Millisecond):
	case <-adapter.release:
		t.Fatal("test adapter was released unexpectedly")
	}

	close(adapter.release)
	require.NoError(t, <-longResult)
	require.NoError(t, closeCasbinContext(t, helper, context.Background()))
	calls := adapter.calls.Load()
	require.GreaterOrEqual(t, calls, int32(2))
	before := observed.Len()
	time.Sleep(20 * time.Millisecond)
	require.Equal(t, before, observed.Len(), "logs must not arrive after CloseContext completes")
	require.Equal(t, 1, countCasbinEvent(observed, "casbin.policy_reload.stopped"))
}

func TestLoggingCasbinConcurrentStopsAreIdempotentAcross100Ticks(t *testing.T) {
	observed := installCasbinObserver(t)
	for round := 0; round < 100; round++ {
		adapter := newBlockingPolicyAdapter()
		helper := &CasbinHelper{enforcer: newLoggingPolicyEnforcer(t, adapter)}
		startAutoLoadForTest(t, helper, 100*time.Microsecond)
		time.Sleep(500 * time.Microsecond)
		stoppedBefore := countCasbinEvent(observed, "casbin.policy_reload.stopped")

		var waiters sync.WaitGroup
		errors := make(chan error, 8)
		for i := 0; i < 8; i++ {
			waiters.Add(1)
			go func() {
				defer waiters.Done()
				errors <- closeCasbinContext(t, helper, context.Background())
			}()
		}
		waiters.Wait()
		close(errors)
		for err := range errors {
			require.NoError(t, err)
		}
		require.Equal(t, stoppedBefore+1, countCasbinEvent(observed, "casbin.policy_reload.stopped"))
		before := observed.Len()
		time.Sleep(200 * time.Microsecond)
		require.Equal(t, before, observed.Len(), "stopped helper emitted a late log")
	}
}

func TestLoggingCasbinCanRestartAutoReloadAfterClose(t *testing.T) {
	observed := installCasbinObserver(t)
	helper := &CasbinHelper{enforcer: newLoggingPolicyEnforcer(t, newBlockingPolicyAdapter())}
	startAutoLoadForTest(t, helper, time.Millisecond)
	helper.Close()

	helper.enforcer = newLoggingPolicyEnforcer(t, newBlockingPolicyAdapter())
	startAutoLoadForTest(t, helper, time.Millisecond)
	require.NoError(t, closeCasbinContext(t, helper, context.Background()))
	require.Equal(t, 2, countCasbinEvent(observed, "casbin.policy_reload.stopped"))
}

var _ persist.Adapter = (*blockingPolicyAdapter)(nil)
