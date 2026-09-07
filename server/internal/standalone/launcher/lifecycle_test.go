package launcher

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
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

func TestLifecycleManualStopUsesFreshContextAndReportsFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		step := func(context.Context) error { return nil }
		sequence := []string{}
		failure := errors.New("media finalization unconfirmed")
		steps := Steps{Preflight: step, Redis: step, Database: step,
			Backend: func(context.Context) (string, error) { return "unconfigured", nil }, Media: step,
			Stop: func(stopCtx context.Context) error {
				sequence = append(sequence, "stop")
				if stopCtx.Err() != nil {
					t.Error("stop inherited canceled run context")
				}
				if _, ok := stopCtx.Deadline(); !ok {
					t.Error("stop must be bounded")
				}
				if fail {
					return failure
				}
				return nil
			}, Cleanup: func() error { sequence = append(sequence, "cleanup"); return nil },
		}
		states := []State{}
		err := Run(ctx, steps, func(s Status) {
			states = append(states, s.State)
			if s.State == Ready {
				cancel()
			}
		})
		if !reflect.DeepEqual(sequence, []string{"stop", "cleanup"}) {
			t.Fatalf("sequence=%v", sequence)
		}
		if fail {
			if !errors.Is(err, failure) || states[len(states)-1] != Failed {
				t.Fatalf("err=%v states=%v", err, states)
			}
		} else if err != nil || states[len(states)-1] != Stopped {
			t.Fatalf("err=%v states=%v", err, states)
		}
	}
}

func TestLifecycleUnexpectedExitDoesNotClaimGracefulStop(t *testing.T) {
	exits := make(chan error, 1)
	step := func(context.Context) error { return nil }
	called := false
	failure := errors.New("backend crashed")
	steps := Steps{Preflight: step, Redis: step, Database: step, Backend: func(context.Context) (string, error) { return "", nil }, Media: step, Exits: exits, Stop: func(context.Context) error { called = true; return nil }}
	err := Run(context.Background(), steps, func(s Status) {
		if s.State == Ready {
			exits <- failure
		}
	})
	if called || !errors.Is(err, failure) {
		t.Fatalf("graceful=%v err=%v", called, err)
	}
}

func TestLifecyclePublishesChangedBusinessStatusAndWaitsForObserver(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan BusinessStatus, 8)
	observerDone := make(chan struct{})
	var stopCalled bool
	var statuses []Status
	steps := Steps{
		Preflight: func(context.Context) error { return nil },
		Redis:     func(context.Context) error { return nil },
		Database:  func(context.Context) error { return nil },
		Backend:   func(context.Context) (string, error) { return "starting", nil },
		Media:     func(context.Context) error { return nil },
		ObserveBusiness: func(observeCtx context.Context) <-chan BusinessStatus {
			go func() {
				<-observeCtx.Done()
				close(updates)
				close(observerDone)
			}()
			return updates
		},
		Stop: func(context.Context) error {
			stopCalled = true
			return nil
		},
	}

	updates <- BusinessStatus{SIPState: "starting", BusinessReason: "SIP 正在启动"}
	updates <- BusinessStatus{SIPState: "starting", BusinessReason: "SIP 正在启动"}
	updates <- BusinessStatus{SIPState: "ready", BusinessReady: true}
	updates <- BusinessStatus{SIPState: "ready", BusinessReady: true}
	updates <- BusinessStatus{SIPState: "failed", BusinessReason: "SIP 已停止"}

	err := Run(ctx, steps, func(status Status) {
		statuses = append(statuses, status)
		if status.State == Ready && status.SIPState == "failed" {
			cancel()
		}
	})
	if err != nil || !stopCalled {
		t.Fatalf("run err=%v stop=%v", err, stopCalled)
	}
	select {
	case <-observerDone:
	case <-time.After(time.Second):
		t.Fatal("business observer was not stopped")
	}

	var business []BusinessStatus
	for _, status := range statuses {
		if status.State == Ready {
			business = append(business, BusinessStatus{SIPState: status.SIPState, BusinessReady: status.BusinessReady, BusinessReason: status.BusinessReason})
		}
	}
	want := []BusinessStatus{
		{SIPState: "starting", BusinessReason: "SIP 正在启动"},
		{SIPState: "ready", BusinessReady: true},
		{SIPState: "failed", BusinessReason: "SIP 已停止"},
	}
	if !reflect.DeepEqual(business, want) {
		t.Fatalf("business updates=%v, want %v", business, want)
	}
}
