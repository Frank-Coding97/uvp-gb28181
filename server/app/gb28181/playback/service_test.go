package playback

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func playbackRequest(now time.Time, owner, channel, key string) CreateRequest {
	return CreateRequest{
		OwnerID: owner, ChannelID: channel, RecordKey: key, IdempotencyKey: "idem-" + key,
		SegmentStart: now, SegmentEnd: now.Add(time.Minute), Now: now,
	}
}

func TestRegistryCreateIsIdempotentAndScopesActiveSession(t *testing.T) {
	now := time.Unix(1700000000, 0)
	r := NewRegistry(RegistryConfig{Now: func() time.Time { return now }})
	first, err := r.Create(context.Background(), playbackRequest(now, "u1", "c1", "r1"))
	if err != nil || first.Existing || first.Session.ID == "" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	repeat, err := r.Create(context.Background(), playbackRequest(now, "u1", "c1", "r1"))
	if err != nil || !repeat.Existing || repeat.Session.ID != first.Session.ID {
		t.Fatalf("repeat=%+v err=%v", repeat, err)
	}
	if _, err := r.Create(context.Background(), playbackRequest(now, "u1", "c1", "r2")); !errors.Is(err, ErrPlaybackBusy) {
		t.Fatalf("different idempotency err=%v", err)
	}
	other, err := r.Create(context.Background(), playbackRequest(now, "u2", "c1", "r2"))
	if err != nil || other.Session.ID == first.Session.ID {
		t.Fatalf("other owner=%+v err=%v", other, err)
	}
}

func TestRegistryConcurrentCreateHasAtMostOnePerOwnerChannel(t *testing.T) {
	now := time.Unix(1700000000, 0)
	r := NewRegistry(RegistryConfig{Now: func() time.Time { return now }})
	var success atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := r.Create(context.Background(), playbackRequest(now, "u1", "c1", "unique"))
			if err == nil && !result.Existing {
				success.Add(1)
			}
		}()
	}
	wg.Wait()
	if success.Load() != 1 {
		t.Fatalf("success=%d", success.Load())
	}
}

func TestRegistryStateMachineFirstTerminalWins(t *testing.T) {
	now := time.Unix(1700000000, 0)
	r := NewRegistry(RegistryConfig{Now: func() time.Time { return now }})
	created, _ := r.Create(context.Background(), playbackRequest(now, "u1", "c1", "r1"))
	for _, state := range []State{StateBuffering, StatePlaying, StatePaused, StatePlaying} {
		if err := r.Transition(created.Session.ID, state); err != nil {
			t.Fatalf("transition %s: %v", state, err)
		}
	}
	if err := r.Finalize(created.Session.ID, StateEnded, "file-to-end"); err != nil {
		t.Fatal(err)
	}
	if err := r.Finalize(created.Session.ID, StateFailed, "late-error"); err != nil {
		t.Fatal(err)
	}
	session, ok := r.Get(created.Session.ID)
	if !ok || session.State != StateEnded || session.EndReason != "file-to-end" {
		t.Fatalf("session=%+v ok=%v", session, ok)
	}
	if err := r.Transition(created.Session.ID, StatePlaying); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("terminal transition err=%v", err)
	}
}

func TestRegistryStopIsIdempotentAndContinuesCleanupAfterFailure(t *testing.T) {
	now := time.Unix(1700000000, 0)
	resources := &fakeCleanupResources{closeErr: errors.New("rtp close failed")}
	r := NewRegistry(RegistryConfig{Now: func() time.Time { return now }})
	request := playbackRequest(now, "u1", "c1", "r1")
	request.Resources = resources
	created, _ := r.Create(context.Background(), request)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = r.Stop(context.Background(), created.Session.ID, "user stop") }()
	}
	wg.Wait()
	if resources.teardown.Load() != 1 || resources.close.Load() != 1 || resources.unbind.Load() != 1 {
		t.Fatalf("cleanup counts teardown=%d close=%d unbind=%d", resources.teardown.Load(), resources.close.Load(), resources.unbind.Load())
	}
	session, _ := r.Get(created.Session.ID)
	if session.State != StateStopped || session.EndReason == "" {
		t.Fatalf("session=%+v", session)
	}
	if err := r.Stop(context.Background(), created.Session.ID, "repeat"); err == nil {
		t.Fatal("cleanup error must be returned to the owner")
	}
}

func TestRegistrySweepExpiresIdleAndMaxDeadline(t *testing.T) {
	now := time.Unix(1700000000, 0)
	r := NewRegistry(RegistryConfig{Now: func() time.Time { return now }, IdleTimeout: 10 * time.Second, MaxSession: time.Minute})
	idle, _ := r.Create(context.Background(), playbackRequest(now, "u1", "c1", "idle"))
	max, _ := r.Create(context.Background(), playbackRequest(now, "u2", "c2", "max"))
	if err := r.Touch(max.Session.ID, now.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := r.Sweep(context.Background(), now.Add(11*time.Second)); err != nil {
		t.Fatal(err)
	}
	if r.MustGet(idle.Session.ID).State != StateStopped || r.MustGet(max.Session.ID).State != StateCreating {
		t.Fatalf("idle=%+v max=%+v", r.MustGet(idle.Session.ID), r.MustGet(max.Session.ID))
	}
	if err := r.Sweep(context.Background(), now.Add(time.Minute+time.Second)); err != nil {
		t.Fatal(err)
	}
	if r.MustGet(max.Session.ID).State != StateStopped {
		t.Fatalf("max=%+v", r.MustGet(max.Session.ID))
	}
}

func TestRegistryCloseRejectsNewAndCleansAllSessions(t *testing.T) {
	now := time.Unix(1700000000, 0)
	resources := &fakeCleanupResources{}
	r := NewRegistry(RegistryConfig{Now: func() time.Time { return now }})
	request := playbackRequest(now, "u1", "c1", "r1")
	request.Resources = resources
	_, _ = r.Create(context.Background(), request)
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.ActiveCount() != 0 || r.Size() != 0 {
		t.Fatalf("active=%d size=%d", r.ActiveCount(), r.Size())
	}
	if _, err := r.Create(context.Background(), playbackRequest(now, "u2", "c2", "r2")); !errors.Is(err, ErrRegistryClosed) {
		t.Fatalf("create after close err=%v", err)
	}
}

func TestRegistryOwnerAndMissingSessionAreNotObservable(t *testing.T) {
	now := time.Unix(1700000000, 0)
	r := NewRegistry(RegistryConfig{Now: func() time.Time { return now }})
	created, _ := r.Create(context.Background(), playbackRequest(now, "u1", "c1", "r1"))
	if _, ok := r.GetForOwner(created.Session.ID, "u2"); ok {
		t.Fatal("other owner can observe session")
	}
	if err := r.StopForOwner(context.Background(), created.Session.ID, "u2", "x"); !errors.Is(err, ErrPlaybackNotFound) {
		t.Fatalf("other owner stop err=%v", err)
	}
}

type fakeCleanupResources struct {
	teardown, close, unbind atomic.Int32
	closeErr                error
}

func (f *fakeCleanupResources) Teardown(context.Context) error { f.teardown.Add(1); return nil }
func (f *fakeCleanupResources) CloseRTP(context.Context) error { f.close.Add(1); return f.closeErr }
func (f *fakeCleanupResources) Unbind(context.Context) error   { f.unbind.Add(1); return nil }
