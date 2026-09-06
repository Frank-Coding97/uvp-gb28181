package gb28181

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoggingShutdownGenerationSignalAndEagerStart(t *testing.T) {
	var (
		eventsMu sync.Mutex
		events   []string
	)
	appendEvent := func(event string) {
		eventsMu.Lock()
		events = append(events, event)
		eventsMu.Unlock()
	}

	legacyStarted := make(chan struct{})
	legacyRelease := make(chan struct{})
	eagerStarted := make(chan struct{})
	eagerRelease := make(chan struct{})
	generation := newShutdownGeneration(context.Background(), func() {
		appendEvent("signal")
	}, []shutdownStep{
		{
			name: "legacy",
			stop: func(context.Context) error {
				appendEvent("legacy.start")
				close(legacyStarted)
				<-legacyRelease
				appendEvent("legacy.done")
				return nil
			},
		},
		{
			name:  "eager",
			eager: true,
			stop: func(context.Context) error {
				appendEvent("eager.start")
				close(eagerStarted)
				<-eagerRelease
				appendEvent("eager.done")
				return nil
			},
		},
	})

	select {
	case <-legacyStarted:
	case <-time.After(time.Second):
		t.Fatal("legacy stop did not start")
	}
	select {
	case <-eagerStarted:
	case <-time.After(time.Second):
		t.Fatal("eager stop did not start")
	}

	eventsMu.Lock()
	gotEvents := append([]string(nil), events...)
	eventsMu.Unlock()
	require.NotEmpty(t, gotEvents)
	require.Equal(t, "signal", gotEvents[0])
	require.Contains(t, gotEvents, "eager.start")

	close(legacyRelease)
	close(eagerRelease)
	require.NoError(t, generation.Wait(context.Background()))
	requireClosed(t, generation.Done())
}

func TestLoggingShutdownGenerationDeadlineStartsRemainingAndWaitTimeoutListsUnfinished(t *testing.T) {
	initialContext, initialCancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer initialCancel()

	legacyStarted := make(chan struct{})
	legacyRelease := make(chan struct{})
	laterStarted := make(chan struct{})
	generation := newShutdownGeneration(initialContext, nil, []shutdownStep{
		{
			name: "legacy",
			stop: func(context.Context) error {
				close(legacyStarted)
				<-legacyRelease
				return nil
			},
		},
		{
			name: "later",
			stop: func(context.Context) error {
				close(laterStarted)
				return nil
			},
		},
	})

	select {
	case <-legacyStarted:
	case <-time.After(time.Second):
		t.Fatal("legacy stop did not start")
	}
	select {
	case <-laterStarted:
	case <-time.After(time.Second):
		t.Fatal("coordinator did not start later stop after the initial deadline")
	}

	waitContext, waitCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer waitCancel()
	err := generation.Wait(waitContext)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Contains(t, err.Error(), "legacy")
	require.NotContains(t, err.Error(), "later")
	requireNotClosed(t, generation.Done())

	close(legacyRelease)
	require.NoError(t, generation.Wait(context.Background()))
	requireClosed(t, generation.Done())
}

func TestLoggingShutdownGenerationPreservesDependencyOrder(t *testing.T) {
	firstStarted := make(chan struct{})
	firstRelease := make(chan struct{})
	secondStarted := make(chan struct{})
	generation := newShutdownGeneration(context.Background(), nil, []shutdownStep{
		{
			name: "consumer",
			stop: func(context.Context) error {
				close(firstStarted)
				<-firstRelease
				return nil
			},
		},
		{
			name: "provider",
			stop: func(context.Context) error {
				close(secondStarted)
				return nil
			},
		},
	})

	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first stop did not start")
	}
	select {
	case <-secondStarted:
		t.Fatal("provider stop started before consumer stop completed")
	default:
	}

	close(firstRelease)
	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("provider stop did not start after consumer stop completed")
	}
	require.NoError(t, generation.Wait(context.Background()))
}

func TestLoggingShutdownGenerationConcurrentWaitStopsEachStepOnce(t *testing.T) {
	const waiters = 64
	var (
		signalCalls atomic.Int32
		stopCalls   [2]atomic.Int32
	)
	generation := newShutdownGeneration(context.Background(), func() {
		signalCalls.Add(1)
	}, []shutdownStep{
		{
			name: "first",
			stop: func(context.Context) error {
				stopCalls[0].Add(1)
				return nil
			},
		},
		{
			name: "second",
			stop: func(context.Context) error {
				stopCalls[1].Add(1)
				return nil
			},
		},
	})

	var waitGroup sync.WaitGroup
	errs := make(chan error, waiters)
	waitGroup.Add(waiters)
	for i := 0; i < waiters; i++ {
		go func() {
			defer waitGroup.Done()
			errs <- generation.Wait(context.Background())
		}()
	}
	waitGroup.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	require.Equal(t, int32(1), signalCalls.Load())
	require.Equal(t, int32(1), stopCalls[0].Load())
	require.Equal(t, int32(1), stopCalls[1].Load())
	requireClosed(t, generation.Done())
}

func TestLoggingShutdownGenerationAggregatesNamedErrors(t *testing.T) {
	firstErr := errors.New("first stop failed")
	secondErr := errors.New("second stop failed")
	generation := newShutdownGeneration(context.Background(), nil, []shutdownStep{
		{
			name: "consumer",
			stop: func(context.Context) error { return firstErr },
		},
		{
			name: "provider",
			stop: func(context.Context) error { return secondErr },
		},
	})

	firstResult := generation.Wait(context.Background())
	require.Error(t, firstResult)
	require.ErrorIs(t, firstResult, firstErr)
	require.ErrorIs(t, firstResult, secondErr)
	require.Contains(t, firstResult.Error(), "consumer")
	require.Contains(t, firstResult.Error(), "provider")

	secondResult := generation.Wait(context.Background())
	require.Error(t, secondResult)
	require.Equal(t, firstResult, secondResult)
}

func TestLoggingShutdownGenerationStartStopOneHundredRounds(t *testing.T) {
	for round := 0; round < 100; round++ {
		var calls [3]atomic.Int32
		generation := newShutdownGeneration(context.Background(), nil, []shutdownStep{
			{
				name:  "leaf",
				eager: true,
				stop: func(context.Context) error {
					calls[0].Add(1)
					return nil
				},
			},
			{
				name: "middle",
				stop: func(context.Context) error {
					calls[1].Add(1)
					return nil
				},
			},
			{
				name: "root",
				stop: func(context.Context) error {
					calls[2].Add(1)
					return nil
				},
			},
		})
		require.NoError(t, generation.Wait(context.Background()), "round %d", round)
		requireClosed(t, generation.Done())
		for i := range calls {
			require.Equal(t, int32(1), calls[i].Load(), "round %d step %d", round, i)
		}
	}
}

func requireClosed(t *testing.T, channel <-chan struct{}) {
	t.Helper()
	select {
	case <-channel:
	case <-time.After(time.Second):
		t.Fatal("channel did not close")
	}
}

func requireNotClosed(t *testing.T, channel <-chan struct{}) {
	t.Helper()
	select {
	case <-channel:
		t.Fatal("channel closed unexpectedly")
	default:
	}
}

func TestLoggingShutdownGenerationTimeoutListsAllNotYetCompletedSteps(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	generation := newShutdownGeneration(context.Background(), nil, []shutdownStep{
		{
			name: "blocked",
			stop: func(context.Context) error {
				close(started)
				<-release
				return nil
			},
		},
		{
			name: "unstarted",
			stop: func(context.Context) error { return nil },
		},
	})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("blocked stop did not start")
	}

	waitContext, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := generation.Wait(waitContext)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.True(t, strings.Contains(err.Error(), "blocked"))
	require.True(t, strings.Contains(err.Error(), "unstarted"))
	requireNotClosed(t, generation.Done())

	close(release)
	require.NoError(t, generation.Wait(context.Background()))
}
