package casbinhelper

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type shutdownConfig struct {
	autoLoadSeconds int
}

func (c shutdownConfig) ConfigFileChangeListen(...func()) {}
func (c shutdownConfig) Get(string) interface{}           { return nil }
func (c shutdownConfig) GetString(string) string          { return "" }
func (c shutdownConfig) GetBool(string) bool              { return false }
func (c shutdownConfig) GetInt(key string) int {
	if key == "casbin.autoloadpolicyseconds" {
		return c.autoLoadSeconds
	}
	return 0
}
func (c shutdownConfig) GetInt32(string) int32            { return 0 }
func (c shutdownConfig) GetInt64(string) int64            { return 0 }
func (c shutdownConfig) GetFloat64(string) float64        { return 0 }
func (c shutdownConfig) GetDuration(string) time.Duration { return 0 }
func (c shutdownConfig) GetStringSlice(string) []string   { return nil }
func (c shutdownConfig) GetUintSlice(string) []uint       { return nil }
func (c shutdownConfig) Set(string, interface{})          {}
func (c shutdownConfig) SaveConfig() error                { return nil }

type shutdownPolicyAdapter struct {
	started    chan struct{}
	release    chan struct{}
	releaseOne sync.Once
	loads      atomic.Int32
	block      bool
}

func (a *shutdownPolicyAdapter) LoadPolicy(model.Model) error {
	a.loads.Add(1)
	select {
	case a.started <- struct{}{}:
	default:
	}
	if a.block {
		<-a.release
	}
	return nil
}

func (*shutdownPolicyAdapter) SavePolicy(model.Model) error { return nil }
func (*shutdownPolicyAdapter) AddPolicy(string, string, []string) error {
	return nil
}
func (*shutdownPolicyAdapter) RemovePolicy(string, string, []string) error {
	return nil
}
func (*shutdownPolicyAdapter) RemoveFilteredPolicy(string, string, int, ...string) error {
	return nil
}

func (a *shutdownPolicyAdapter) unblock() {
	a.releaseOne.Do(func() { close(a.release) })
}

func newShutdownHelper(t *testing.T, autoLoadSeconds int, block bool) (*CasbinHelper, *shutdownPolicyAdapter) {
	t.Helper()
	oldConfig, oldLog := app.ConfigYml, app.ZapLog
	app.ConfigYml = shutdownConfig{autoLoadSeconds: autoLoadSeconds}
	app.ZapLog = zap.NewNop()
	t.Cleanup(func() {
		app.ConfigYml, app.ZapLog = oldConfig, oldLog
	})

	m, err := model.NewModelFromString(testModelConfig)
	require.NoError(t, err)
	enforcer, err := casbin.NewEnforcer(m)
	require.NoError(t, err)
	adapter := &shutdownPolicyAdapter{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
		block:   block,
	}
	enforcer.SetAdapter(adapter)
	helper := NewCasbinHelper()
	helper.enforcer = enforcer
	t.Cleanup(func() {
		adapter.unblock()
		_ = helper.Shutdown(context.Background())
	})
	return helper, adapter
}

func waitForReload(t *testing.T, adapter *shutdownPolicyAdapter) {
	t.Helper()
	select {
	case <-adapter.started:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for accepted policy reload")
	}
}

func TestShutdownWaitsForAcceptedPolicyReload(t *testing.T) {
	helper, adapter := newShutdownHelper(t, 1, true)
	helper.startAutoLoadPolicy()
	waitForReload(t, adapter)
	require.Equal(t, int32(1), adapter.loads.Load())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := helper.Shutdown(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	// The accepted adapter call is still blocking, so the timeout must not
	// report a successful drain or discard the in-flight call.
	require.Equal(t, int32(1), adapter.loads.Load())

	adapter.unblock()
	require.Eventually(t, func() bool {
		return helper.Shutdown(context.Background()) == nil
	}, 2*time.Second, 10*time.Millisecond)
	require.NoError(t, helper.Shutdown(context.Background()))
}

func TestStopAndShutdownAreConcurrentAndIdempotent(t *testing.T) {
	helper, _ := newShutdownHelper(t, 1, false)
	helper.startAutoLoadPolicy()

	const callers = 32
	start := make(chan struct{})
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			if i%2 == 0 {
				helper.StopAutoLoadPolicy()
				return
			}
			errs <- helper.Shutdown(context.Background())
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.NoError(t, helper.Shutdown(context.Background()))
	helper.StopAutoLoadPolicy()
}

func TestShutdownWithoutAutoReloadIsNoop(t *testing.T) {
	helper, _ := newShutdownHelper(t, 0, false)
	require.NoError(t, helper.Shutdown(context.Background()))
	require.NoError(t, helper.Shutdown(context.Background()))
	helper.StopAutoLoadPolicy()
}

func TestShutdownTimeoutErrorIsBounded(t *testing.T) {
	helper, adapter := newShutdownHelper(t, 1, true)
	helper.startAutoLoadPolicy()
	waitForReload(t, adapter)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := helper.Shutdown(ctx)
	require.Error(t, err)
	require.True(t, errors.Is(err, context.DeadlineExceeded))
	require.Less(t, time.Since(started), 500*time.Millisecond)
	adapter.unblock()
	require.NoError(t, helper.Shutdown(context.Background()))
}
