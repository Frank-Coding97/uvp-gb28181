package runtimeexecutor

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type testScheduler struct{ app.JobSchedulerInterf }

func resetRegistryForTest(t *testing.T) {
	t.Helper()

	registryMu.Lock()
	pendingRegistrars = nil
	appliedRegistrars = 0
	activeScheduler = nil
	registryCommitted = false
	registryMu.Unlock()

	t.Cleanup(func() {
		registryMu.Lock()
		pendingRegistrars = nil
		appliedRegistrars = 0
		activeScheduler = nil
		registryCommitted = false
		registryMu.Unlock()
	})
}

func TestRegisterQueuesUntilSchedulerIsApplied(t *testing.T) {
	resetRegistryForTest(t)

	want := &testScheduler{}
	var calls int
	Register(func(got app.JobSchedulerInterf) {
		calls++
		require.Same(t, want, got)
	})
	require.Zero(t, calls)

	Apply(want)
	require.Equal(t, 1, calls)
	Commit(want)
	Apply(want)
	require.Equal(t, 1, calls)
}

func TestRegisterAfterCommitUsesActiveScheduler(t *testing.T) {
	resetRegistryForTest(t)

	want := &testScheduler{}
	Apply(want)
	Commit(want)

	var calls int
	Register(func(got app.JobSchedulerInterf) {
		calls++
		require.Same(t, want, got)
	})
	require.Equal(t, 1, calls)
}

func TestRollbackKeepsRegistrarsForRetry(t *testing.T) {
	resetRegistryForTest(t)

	first := &testScheduler{}
	second := &testScheduler{}
	var seen []app.JobSchedulerInterf
	Register(func(got app.JobSchedulerInterf) { seen = append(seen, got) })

	Apply(first)
	require.Len(t, seen, 1)
	Rollback()
	Apply(second)
	require.Len(t, seen, 2)
	require.Same(t, first, seen[0])
	require.Same(t, second, seen[1])
	Commit(second)
}

func TestRegistrarAddedDuringApplyIsApplied(t *testing.T) {
	resetRegistryForTest(t)

	want := &testScheduler{}
	var mu sync.Mutex
	var calls []string
	Register(func(got app.JobSchedulerInterf) {
		require.Same(t, want, got)
		mu.Lock()
		calls = append(calls, "first")
		mu.Unlock()
		Register(func(got app.JobSchedulerInterf) {
			require.Same(t, want, got)
			mu.Lock()
			calls = append(calls, "second")
			mu.Unlock()
		})
	})

	Apply(want)
	Commit(want)
	mu.Lock()
	require.Equal(t, []string{"first", "second"}, calls)
	mu.Unlock()
}
