package launcher

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestLifecycleStopsAtEachFailureAndCleansOnlyOwnedResources(t *testing.T) {
	for fail := 0; fail < 5; fail++ {
		called := []int{}
		closed := 0
		states := []State{}
		failure := errors.New("specific component failure")
		step := func(n int) func(context.Context) error {
			return func(context.Context) error {
				called = append(called, n)
				if n == fail {
					return failure
				}
				return nil
			}
		}
		steps := Steps{Preflight: step(0), Redis: step(1), Database: step(2), Backend: func(ctx context.Context) (string, error) { return "unconfigured", step(3)(ctx) }, Media: step(4), Cleanup: func() error { closed++; return nil }}
		err := Run(context.Background(), steps, func(s Status) { states = append(states, s.State) })
		if !errors.Is(err, failure) || closed != 1 || len(called) != fail+1 || states[len(states)-1] != Failed {
			t.Fatalf("fail %d: calls=%v states=%v cleanup=%d err=%v", fail, called, states, closed, err)
		}
	}
}

func TestLifecycleSeparatesComponentsReadyFromSIPAndDetectsExit(t *testing.T) {
	states := []State{}
	closed := 0
	exits := make(chan error, 1)
	step := func(context.Context) error { return nil }
	steps := Steps{Preflight: step, Redis: step, Database: step, Backend: func(context.Context) (string, error) { return "unconfigured", nil }, Media: step, Exits: exits, Cleanup: func() error { closed++; return nil }}
	failure := errors.New("redis exited with code 1")
	err := Run(context.Background(), steps, func(s Status) {
		states = append(states, s.State)
		if s.State == Ready {
			if s.BusinessReady || s.SIPState != "unconfigured" {
				t.Error("pending setup must not be business ready")
			}
			exits <- failure
		}
	})
	want := []State{Preflight, RedisReady, DatabaseReady, BackendReady, MediaReady, Ready, Failed}
	if !reflect.DeepEqual(states, want) || !errors.Is(err, failure) || closed != 1 {
		t.Fatalf("states=%v closed=%d err=%v", states, closed, err)
	}
}

func TestLifecycleCancellationBeforeStartDoesNotLaunch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := Run(ctx, Steps{Preflight: func(context.Context) error { called = true; return nil }, Cleanup: func() error { return nil }}, nil)
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("called=%v err=%v", called, err)
	}
}
