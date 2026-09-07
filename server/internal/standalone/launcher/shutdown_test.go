package launcher

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestShutdownRunsStepsInOrder(t *testing.T) {
	stages := []string{"quiesce", "media", "finalize", "backend", "redis"}
	events := make(chan string, len(stages))
	release := make(map[string]chan struct{}, len(stages))
	for _, stage := range stages {
		release[stage] = make(chan struct{})
	}
	step := func(stage string) func(context.Context) error {
		return func(ctx context.Context) error {
			events <- stage
			select {
			case <-release[stage]:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	steps := ShutdownSteps{
		Quiesce:  step("quiesce"),
		Media:    step("media"),
		Finalize: step("finalize"),
		Backend:  step("backend"),
		Redis:    step("redis"),
	}

	done := make(chan error, 1)
	go func() { done <- Shutdown(context.Background(), steps) }()
	for _, want := range stages {
		select {
		case got := <-events:
			if got != want {
				t.Fatalf("stage order: got %q, want %q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("stage %q did not start", want)
		}
		select {
		case got := <-events:
			t.Fatalf("stage %q ran before %q completed", got, want)
		case <-time.After(10 * time.Millisecond):
		}
		close(release[want])
	}
	if err := <-done; err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestShutdownStopsAtFailureWithoutLeakingCause(t *testing.T) {
	const secret = "shutdown-secret-value"
	failure := errors.New(secret)
	called := make(chan string, 5)
	steps := ShutdownSteps{
		Quiesce: func(context.Context) error {
			called <- "quiesce"
			return failure
		},
		Media: func(context.Context) error {
			called <- "media"
			return nil
		},
		Finalize: func(context.Context) error {
			called <- "finalize"
			return nil
		},
		Backend: func(context.Context) error {
			called <- "backend"
			return nil
		},
		Redis: func(context.Context) error {
			called <- "redis"
			return nil
		},
	}

	err := Shutdown(context.Background(), steps)
	if !errors.Is(err, failure) {
		t.Fatalf("Shutdown() error does not retain cause: %v", err)
	}
	if !strings.Contains(err.Error(), "quiesce") {
		t.Fatalf("Shutdown() error missing stage: %v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("Shutdown() error leaked callback cause: %v", err)
	}
	if got := <-called; got != "quiesce" {
		t.Fatalf("first stage = %q", got)
	}
	select {
	case got := <-called:
		t.Fatalf("stage %q ran after failure", got)
	default:
	}
}

func TestShutdownChecksContextBeforeStarting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	step := func(context.Context) error {
		calls.Add(1)
		return nil
	}

	err := Shutdown(ctx, ShutdownSteps{Quiesce: step, Media: step, Finalize: step, Backend: step, Redis: step})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Shutdown() error = %v, want context.Canceled", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("steps called before context check: %d", got)
	}
}

func TestShutdownRejectsMissingStepBeforeRunningAnyStep(t *testing.T) {
	all := map[string]func(*ShutdownSteps){
		"quiesce":  func(steps *ShutdownSteps) { steps.Quiesce = nil },
		"media":    func(steps *ShutdownSteps) { steps.Media = nil },
		"finalize": func(steps *ShutdownSteps) { steps.Finalize = nil },
		"backend":  func(steps *ShutdownSteps) { steps.Backend = nil },
		"redis":    func(steps *ShutdownSteps) { steps.Redis = nil },
	}
	for missing, remove := range all {
		t.Run(missing, func(t *testing.T) {
			var calls atomic.Int32
			step := func(context.Context) error {
				calls.Add(1)
				return nil
			}
			steps := ShutdownSteps{Quiesce: step, Media: step, Finalize: step, Backend: step, Redis: step}
			remove(&steps)

			err := Shutdown(context.Background(), steps)
			if err == nil || !strings.Contains(err.Error(), missing) {
				t.Fatalf("Shutdown() error = %v, want missing %s", err, missing)
			}
			if got := calls.Load(); got != 0 {
				t.Fatalf("steps ran with missing %s: %d calls", missing, got)
			}
		})
	}
}

func TestShutdownDeadlineStopsBeforeNextStage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := make(chan struct{})
	var laterCalls atomic.Int32
	steps := ShutdownSteps{
		Quiesce: func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		},
		Media: func(context.Context) error {
			laterCalls.Add(1)
			return nil
		},
		Finalize: func(context.Context) error {
			laterCalls.Add(1)
			return nil
		},
		Backend: func(context.Context) error {
			laterCalls.Add(1)
			return nil
		},
		Redis: func(context.Context) error {
			laterCalls.Add(1)
			return nil
		},
	}

	done := make(chan error, 1)
	go func() { done <- Shutdown(ctx, steps) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("quiesce did not start")
	}
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown() error = %v, want deadline", err)
	}
	if got := laterCalls.Load(); got != 0 {
		t.Fatalf("later stages ran after deadline: %d calls", got)
	}
}

func TestShutdownDoesNotDetachUnresponsiveStep(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	var laterCalls atomic.Int32
	steps := ShutdownSteps{
		Quiesce: func(context.Context) error {
			close(entered)
			<-release
			return nil
		},
		Media: func(context.Context) error {
			laterCalls.Add(1)
			return nil
		},
		Finalize: func(context.Context) error {
			laterCalls.Add(1)
			return nil
		},
		Backend: func(context.Context) error {
			laterCalls.Add(1)
			return nil
		},
		Redis: func(context.Context) error {
			laterCalls.Add(1)
			return nil
		},
	}

	done := make(chan error, 1)
	go func() { done <- Shutdown(ctx, steps) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("quiesce did not start")
	}
	<-ctx.Done()
	select {
	case err := <-done:
		t.Fatalf("Shutdown() returned while step ignored context: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown() error after release = %v, want deadline", err)
	}
	if got := laterCalls.Load(); got != 0 {
		t.Fatalf("later stages ran after deadline: %d calls", got)
	}
}
