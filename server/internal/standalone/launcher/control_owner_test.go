package launcher

import (
	"context"
	"errors"
	"testing"
	"time"
	"uvplatform.cn/uvp-gb28181/internal/standalone/control"
)

func TestStopReplyWaitsForCleanupResult(t *testing.T) {
	for _, failure := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		owner := newControlOwner(cancel)
		owner.publish(Ready)
		done := make(chan control.Reply, 1)
		go func() { r, _ := owner.handle(context.Background(), control.Stop); done <- r }()
		<-ctx.Done()
		select {
		case <-done:
			t.Fatal("stop acknowledged before cleanup")
		default:
		}
		var err error
		if failure {
			err = errors.New("cleanup failed")
		}
		owner.finish(err)
		select {
		case r := <-done:
			want := control.Finalized
			if failure {
				want = control.Failed
			}
			if r != want {
				t.Fatalf("reply=%s", r)
			}
		case <-time.After(time.Second):
			t.Fatal("stop did not finish")
		}
	}
}

func TestStopBeforeReadyDoesNotCancelStartup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := newControlOwner(cancel)
	if r, err := owner.handle(ctx, control.Stop); err == nil || r != control.Failed {
		t.Fatal("early stop accepted")
	}
	if ctx.Err() != nil {
		t.Fatal("startup canceled")
	}
}

func TestStopClientDeadlineDoesNotAbandonShutdown(t *testing.T) {
	run, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := newControlOwner(cancel)
	owner.publish(Ready)
	ctx, end := context.WithCancel(context.Background())
	end()
	if _, err := owner.handle(ctx, control.Stop); err == nil {
		t.Fatal("client deadline ignored")
	}
	if run.Err() == nil {
		t.Fatal("shutdown abandoned")
	}
	owner.finish(nil)
}

func TestOwnedExitRequiresActualCompletion(t *testing.T) {
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitOwnedExit(ctx, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	failure := errors.New("process exit 1")
	done <- failure
	if err := waitOwnedExit(context.Background(), done); !errors.Is(err, failure) {
		t.Fatalf("exit failure=%v", err)
	}
	if err := waitOwnedExit(context.Background(), nil); err == nil {
		t.Fatal("missing child accepted")
	}
}
