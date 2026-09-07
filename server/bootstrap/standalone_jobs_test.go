//go:build integration

package bootstrap

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/internal/standalone"
)

type runtimeJobsTestScheduler struct {
	app.JobSchedulerInterf
	stopCalls int32
}

func (s *runtimeJobsTestScheduler) Stop() {
	atomic.AddInt32(&s.stopCalls, 1)
}

func installRuntimeJobsTestHooks(t *testing.T, hooks runtimeJobsHooks) {
	t.Helper()

	runtimeJobsMu.Lock()
	oldHooks := runtimeJobsStartHooks
	oldStarted := runtimeJobsStarted
	oldScheduler := app.JobScheduler
	oldPaths := standalonePaths
	runtimeJobsStartHooks = hooks
	runtimeJobsStarted = false
	app.JobScheduler = nil
	standalonePaths = standalone.Paths{}
	runtimeJobsMu.Unlock()

	t.Cleanup(func() {
		runtimeJobsMu.Lock()
		runtimeJobsStartHooks = oldHooks
		runtimeJobsStarted = oldStarted
		app.JobScheduler = oldScheduler
		standalonePaths = oldPaths
		runtimeJobsMu.Unlock()
	})
}

func TestStartLegacyRuntimeJobsSkipsExplicitStandalone(t *testing.T) {
	var constructorCalls int32
	installRuntimeJobsTestHooks(t, runtimeJobsHooks{
		newScheduler: func() app.JobSchedulerInterf {
			atomic.AddInt32(&constructorCalls, 1)
			return &runtimeJobsTestScheduler{}
		},
	})

	runtimeJobsMu.Lock()
	standalonePaths = standalone.Paths{Explicit: true}
	runtimeJobsMu.Unlock()

	require.NoError(t, startLegacyRuntimeJobs())
	require.Zero(t, atomic.LoadInt32(&constructorCalls))
	require.Nil(t, app.JobScheduler)
}

func TestStartLegacyRuntimeJobsRunsLegacyPath(t *testing.T) {
	var constructorCalls, registerExecutorCalls, registerSystemCalls, loadCalls int32
	scheduler := &runtimeJobsTestScheduler{}
	installRuntimeJobsTestHooks(t, runtimeJobsHooks{
		newScheduler: func() app.JobSchedulerInterf {
			atomic.AddInt32(&constructorCalls, 1)
			return scheduler
		},
		registerExecutors: func() {
			atomic.AddInt32(&registerExecutorCalls, 1)
		},
		registerSystemJobs: func() error {
			atomic.AddInt32(&registerSystemCalls, 1)
			return nil
		},
		loadJobsFromDB: func() error {
			atomic.AddInt32(&loadCalls, 1)
			return nil
		},
	})

	require.NoError(t, startLegacyRuntimeJobs())
	require.Equal(t, int32(1), atomic.LoadInt32(&constructorCalls))
	require.Equal(t, int32(1), atomic.LoadInt32(&registerExecutorCalls))
	require.Equal(t, int32(1), atomic.LoadInt32(&registerSystemCalls))
	require.Equal(t, int32(1), atomic.LoadInt32(&loadCalls))
	require.Same(t, scheduler, app.JobScheduler)
}

func TestStartRuntimeJobsIsIdempotentUnderConcurrency(t *testing.T) {
	var constructorCalls, registerExecutorCalls, registerSystemCalls, loadCalls int32
	scheduler := &runtimeJobsTestScheduler{}
	installRuntimeJobsTestHooks(t, runtimeJobsHooks{
		newScheduler: func() app.JobSchedulerInterf {
			atomic.AddInt32(&constructorCalls, 1)
			return scheduler
		},
		registerExecutors: func() {
			atomic.AddInt32(&registerExecutorCalls, 1)
		},
		registerSystemJobs: func() error {
			atomic.AddInt32(&registerSystemCalls, 1)
			return nil
		},
		loadJobsFromDB: func() error {
			atomic.AddInt32(&loadCalls, 1)
			return nil
		},
	})

	const callers = 32
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			errs <- StartRuntimeJobs()
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, int32(1), atomic.LoadInt32(&constructorCalls))
	require.Equal(t, int32(1), atomic.LoadInt32(&registerExecutorCalls))
	require.Equal(t, int32(1), atomic.LoadInt32(&registerSystemCalls))
	require.Equal(t, int32(1), atomic.LoadInt32(&loadCalls))
	require.Same(t, scheduler, app.JobScheduler)
}

func TestStartRuntimeJobsRegisterFailureStopsSchedulerAndSkipsLoad(t *testing.T) {
	var registerExecutorCalls, loadCalls int32
	scheduler := &runtimeJobsTestScheduler{}
	wantErr := errors.New("register system jobs failed")
	installRuntimeJobsTestHooks(t, runtimeJobsHooks{
		newScheduler: func() app.JobSchedulerInterf {
			return scheduler
		},
		registerExecutors: func() {
			atomic.AddInt32(&registerExecutorCalls, 1)
		},
		registerSystemJobs: func() error {
			return wantErr
		},
		loadJobsFromDB: func() error {
			atomic.AddInt32(&loadCalls, 1)
			return nil
		},
	})

	err := StartRuntimeJobs()
	require.ErrorIs(t, err, wantErr)
	require.Equal(t, int32(1), atomic.LoadInt32(&registerExecutorCalls))
	require.Zero(t, atomic.LoadInt32(&loadCalls))
	require.Equal(t, int32(1), atomic.LoadInt32(&scheduler.stopCalls))
	require.Nil(t, app.JobScheduler)

	runtimeJobsMu.Lock()
	started := runtimeJobsStarted
	runtimeJobsMu.Unlock()
	require.False(t, started)
}
