package asyncgroup

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGroupWaitsForAcceptedWorkAndClosesAdmission(t *testing.T) {
	var group Group
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	if !group.Go(func() {
		close(started)
		<-release
		close(finished)
	}) {
		t.Fatal("zero-value group rejected work")
	}
	<-started

	stopCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := group.StopContext(stopCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("StopContext error = %v, want context.Canceled while work is active", err)
	}
	if group.Go(func() {}) {
		t.Fatal("group admitted work after StopContext closed admission")
	}

	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("accepted work did not finish")
	}
	if err := group.StopContext(context.Background()); err != nil {
		t.Fatalf("second StopContext error = %v", err)
	}
}

func TestGroupCompletionWinsOverExpiredContext(t *testing.T) {
	var group Group
	finished := make(chan struct{})
	if !group.Go(func() { close(finished) }) {
		t.Fatal("zero-value group rejected work")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("accepted work did not finish")
	}
	if err := group.StopContext(context.Background()); err != nil {
		t.Fatalf("StopContext after work completion = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := group.StopContext(ctx); err != nil {
		t.Fatalf("completed work returned expired-context error: %v", err)
	}
	if group.Go(func() {}) {
		t.Fatal("group admitted work after completion")
	}
}

func TestGroupConcurrentStopIsIdempotent(t *testing.T) {
	var group Group
	release := make(chan struct{})
	if !group.Go(func() { <-release }) {
		t.Fatal("zero-value group rejected work")
	}

	const stops = 100
	var waiters sync.WaitGroup
	errs := make(chan error, stops)
	waiters.Add(stops)
	for i := 0; i < stops; i++ {
		go func() {
			defer waiters.Done()
			errs <- group.StopContext(context.Background())
		}()
	}
	close(release)
	waiters.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent StopContext error = %v", err)
		}
	}
	if group.Go(func() {}) {
		t.Fatal("group admitted work after concurrent StopContext")
	}
}

func TestGroupPanicStillReleasesCount(t *testing.T) {
	var group Group
	var recovered atomic.Bool
	if !group.Go(func() {
		defer func() {
			if recover() != nil {
				recovered.Store(true)
			}
		}()
		panic("test panic")
	}) {
		t.Fatal("zero-value group rejected work")
	}
	if err := group.StopContext(context.Background()); err != nil {
		t.Fatalf("StopContext error after recovered panic = %v", err)
	}
	if !recovered.Load() {
		t.Fatal("test task did not recover its panic")
	}
}

func TestGroupStartStopOneHundredRounds(t *testing.T) {
	for round := 0; round < 100; round++ {
		var group Group
		release := make(chan struct{})
		if !group.Go(func() { <-release }) {
			t.Fatalf("round %d: zero-value group rejected work", round)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := group.StopContext(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("round %d: StopContext error = %v, want context.Canceled", round, err)
		}
		if group.Go(func() {}) {
			t.Fatalf("round %d: group admitted work after stop", round)
		}
		close(release)
		if err := group.StopContext(context.Background()); err != nil {
			t.Fatalf("round %d: final StopContext error = %v", round, err)
		}
	}
}
