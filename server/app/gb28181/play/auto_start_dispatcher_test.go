package play

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type autoStartEnsureFunc func(context.Context, Request) (*Result, error)

func (f autoStartEnsureFunc) EnsureLive(ctx context.Context, req Request) (*Result, error) {
	return f(ctx, req)
}

func newAutoStartDispatcherForTest(t *testing.T, ensure AutoStartEnsurer, opts AutoStartDispatcherOptions) *AutoStartDispatcher {
	t.Helper()
	d := NewAutoStartDispatcher(ensure, opts)
	t.Cleanup(func() {
		if err := d.Stop(); err != nil {
			t.Errorf("dispatcher stop: %v", err)
		}
	})
	return d
}

func autoStartRequest(device string) Request {
	return Request{DeviceID: device, ChannelID: "channel", RequiredNode: 7, Trigger: "caller-value"}
}

func TestAutoStartDispatcherDeduplicatesAndSetsTrigger(t *testing.T) {
	started := make(chan Request, 1)
	release := make(chan struct{})
	var calls atomic.Int32
	d := newAutoStartDispatcherForTest(t, autoStartEnsureFunc(func(ctx context.Context, req Request) (*Result, error) {
		calls.Add(1)
		started <- req
		select {
		case <-release:
			return &Result{StreamID: "stream"}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}), AutoStartDispatcherOptions{KnownNodeIDs: []int64{7}})

	if err := d.Submit(autoStartRequest("device")); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	if err := d.Submit(autoStartRequest("device")); err != nil {
		t.Fatalf("duplicate submit: %v", err)
	}
	got := <-started
	if got.Trigger != "on_stream_not_found" {
		t.Fatalf("trigger = %q, want on_stream_not_found", got.Trigger)
	}
	close(release)
	deadline := time.After(time.Second)
	for calls.Load() != 1 {
		select {
		case <-deadline:
			t.Fatalf("ensure calls = %d, want 1", calls.Load())
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestAutoStartDispatcherBoundsGlobalInFlightAtSixteen(t *testing.T) {
	started := make(chan struct{}, 8)
	release := make(chan struct{})
	var active atomic.Int32
	var maxActive atomic.Int32
	d := newAutoStartDispatcherForTest(t, autoStartEnsureFunc(func(ctx context.Context, _ Request) (*Result, error) {
		current := active.Add(1)
		for {
			old := maxActive.Load()
			if current <= old || maxActive.CompareAndSwap(old, current) {
				break
			}
		}
		started <- struct{}{}
		select {
		case <-release:
			active.Add(-1)
			return &Result{StreamID: "stream"}, nil
		case <-ctx.Done():
			active.Add(-1)
			return nil, ctx.Err()
		}
	}), AutoStartDispatcherOptions{KnownNodeIDs: []int64{7}})

	for i := 0; i < 8; i++ {
		if err := d.Submit(autoStartRequest(string(rune('a' + i)))); err != nil {
			t.Fatalf("running submit %d: %v", i, err)
		}
	}
	for i := 0; i < 8; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("workers did not start concurrently")
		}
	}
	for i := 0; i < 8; i++ {
		if err := d.Submit(autoStartRequest(string(rune('i' + i)))); err != nil {
			t.Fatalf("queued submit %d: %v", i, err)
		}
	}
	if err := d.Submit(autoStartRequest("overflow")); !errors.Is(err, ErrAutoStartQueueFull) {
		t.Fatalf("overflow submit err = %v, want ErrAutoStartQueueFull", err)
	}
	if got := maxActive.Load(); got != 8 {
		t.Fatalf("max concurrent EnsureLive calls = %d, want 8", got)
	}
	close(release)
}

func TestAutoStartDispatcherRateLimitAndValidation(t *testing.T) {
	now := time.Unix(100, 0)
	called := make(chan struct{}, 17)
	d := newAutoStartDispatcherForTest(t, autoStartEnsureFunc(func(context.Context, Request) (*Result, error) {
		called <- struct{}{}
		return &Result{StreamID: "stream"}, nil
	}), AutoStartDispatcherOptions{
		KnownNodeIDs: []int64{7},
		Clock:        func() time.Time { return now },
	})

	invalid := autoStartRequest("invalid")
	invalid.RequiredNode = 8
	if err := d.Submit(invalid); !errors.Is(err, ErrAutoStartInvalidRequest) {
		t.Fatalf("unknown node err = %v, want ErrAutoStartInvalidRequest", err)
	}
	for i := 0; i < 16; i++ {
		if err := d.Submit(autoStartRequest(string(rune('a' + i)))); err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
		select {
		case <-called:
		case <-time.After(time.Second):
			t.Fatalf("submit %d was not dispatched", i)
		}
	}
	if err := d.Submit(autoStartRequest("limited")); !errors.Is(err, ErrAutoStartNodeRateLimited) {
		t.Fatalf("rate limited submit err = %v, want ErrAutoStartNodeRateLimited", err)
	}
	now = now.Add(125 * time.Millisecond)
	if err := d.Submit(autoStartRequest("refilled")); err != nil {
		t.Fatalf("refilled submit: %v", err)
	}
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("refilled submit was not dispatched")
	}
}

func TestAutoStartDispatcherNodeRateLimitDoesNotAffectOtherNode(t *testing.T) {
	now := time.Unix(100, 0)
	called := make(chan Request, 32)
	d := newAutoStartDispatcherForTest(t, autoStartEnsureFunc(func(_ context.Context, req Request) (*Result, error) {
		called <- req
		return &Result{StreamID: "stream"}, nil
	}), AutoStartDispatcherOptions{
		KnownNodeIDs: []int64{7, 8},
		Clock:        func() time.Time { return now },
	})

	for i := 0; i < 16; i++ {
		request := autoStartRequest(string(rune('a' + i)))
		if err := d.Submit(request); err != nil {
			t.Fatalf("node 7 submit %d: %v", i, err)
		}
		<-called
	}
	limited := autoStartRequest("limited")
	if err := d.Submit(limited); !errors.Is(err, ErrAutoStartNodeRateLimited) {
		t.Fatalf("node 7 limit err = %v", err)
	}
	other := autoStartRequest("other-node")
	other.RequiredNode = 8
	if err := d.Submit(other); err != nil {
		t.Fatalf("node 8 was affected by node 7 limiter: %v", err)
	}
	select {
	case got := <-called:
		if got.RequiredNode != 8 {
			t.Fatalf("dispatched node = %d, want 8", got.RequiredNode)
		}
	case <-time.After(time.Second):
		t.Fatal("node 8 request was not dispatched")
	}
}

func TestAutoStartDispatcherAndExplicitEnsureShareOneStart(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var starts atomic.Int32
	coordinator := NewCoordinator(func(_ context.Context, _ Request) (*Result, error) {
		if starts.Add(1) == 1 {
			close(started)
		}
		<-release
		return &Result{StreamID: "stream", Node: &ResultNode{ID: 7}}, nil
	})
	d := newAutoStartDispatcherForTest(t, coordinator, AutoStartDispatcherOptions{KnownNodeIDs: []int64{7}})

	begin := make(chan struct{})
	var submits sync.WaitGroup
	for i := 0; i < 20; i++ {
		submits.Add(1)
		go func() {
			defer submits.Done()
			<-begin
			if err := d.Submit(Request{DeviceID: "device", ChannelID: "channel", RequiredNode: 7}); err != nil {
				t.Errorf("auto submit: %v", err)
			}
		}()
	}
	explicitDone := make(chan error, 1)
	go func() {
		<-begin
		_, err := coordinator.EnsureLive(context.Background(), Request{
			DeviceID: "device", ChannelID: "channel", Trigger: "explicit",
		})
		explicitDone <- err
	}()
	close(begin)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("shared start did not begin")
	}
	submits.Wait()
	close(release)
	if err := <-explicitDone; err != nil {
		t.Fatalf("explicit ensure: %v", err)
	}
	if got := starts.Load(); got != 1 {
		t.Fatalf("20 hook + 1 explicit starts = %d, want 1", got)
	}
}

func TestAutoStartDispatcherAdmitsDynamicallyKnownNodeWithoutUnboundedKeys(t *testing.T) {
	called := make(chan Request, 1)
	allowed := map[int64]bool{9: true}
	d := newAutoStartDispatcherForTest(t, autoStartEnsureFunc(func(_ context.Context, req Request) (*Result, error) {
		called <- req
		return &Result{StreamID: "stream"}, nil
	}), AutoStartDispatcherOptions{
		KnownNodeIDs: []int64{7},
		NodeAllowed:  func(nodeID int64) bool { return allowed[nodeID] },
	})

	request := autoStartRequest("dynamic-node")
	request.RequiredNode = 9
	if err := d.Submit(request); err != nil {
		t.Fatalf("dynamic known node submit: %v", err)
	}
	select {
	case got := <-called:
		if got.RequiredNode != 9 {
			t.Fatalf("required node = %d, want 9", got.RequiredNode)
		}
	case <-time.After(time.Second):
		t.Fatal("dynamic known node was not dispatched")
	}

	request.DeviceID = "untrusted-node"
	request.RequiredNode = 10
	if err := d.Submit(request); !errors.Is(err, ErrAutoStartInvalidRequest) {
		t.Fatalf("untrusted dynamic node err = %v, want ErrAutoStartInvalidRequest", err)
	}
	request.DeviceID = "known-but-no-longer-allowed"
	request.RequiredNode = 7
	if err := d.Submit(request); !errors.Is(err, ErrAutoStartInvalidRequest) {
		t.Fatalf("disallowed known node err = %v, want ErrAutoStartInvalidRequest", err)
	}
}

func TestAutoStartDispatcherStopRejectsNewRequestsAndWaitsForWorkers(t *testing.T) {
	started := make(chan struct{})
	var startedOnce sync.Once
	d := newAutoStartDispatcherForTest(t, autoStartEnsureFunc(func(ctx context.Context, _ Request) (*Result, error) {
		startedOnce.Do(func() { close(started) })
		<-ctx.Done()
		return nil, ctx.Err()
	}), AutoStartDispatcherOptions{
		KnownNodeIDs:    []int64{7},
		ServiceDeadline: 50 * time.Millisecond,
	})
	if !d.Available() {
		t.Fatal("new dispatcher is unavailable")
	}
	if err := d.Submit(autoStartRequest("device")); err != nil {
		t.Fatalf("submit: %v", err)
	}
	<-started
	stopDone := make(chan error, 1)
	go func() { stopDone <- d.Stop() }()

	deadline := time.After(time.Second)
	for {
		err := d.Submit(autoStartRequest("after-stop"))
		if errors.Is(err, ErrAutoStartStopped) {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("dispatcher did not reject requests during stop; last err=%v", err)
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if err := <-stopDone; err != nil {
		t.Fatalf("stop: %v", err)
	}
	if d.Available() {
		t.Fatal("stopped dispatcher remains available")
	}
	if err := d.Stop(); err != nil {
		t.Fatalf("idempotent stop: %v", err)
	}
}

func TestAutoStartDispatcherStopDiscardsQueuedJobs(t *testing.T) {
	started := make(chan struct{}, 8)
	var calls atomic.Int32
	d := newAutoStartDispatcherForTest(t, autoStartEnsureFunc(func(ctx context.Context, _ Request) (*Result, error) {
		calls.Add(1)
		started <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}), AutoStartDispatcherOptions{
		KnownNodeIDs:    []int64{7},
		ServiceDeadline: 50 * time.Millisecond,
	})
	for i := 0; i < 8; i++ {
		if err := d.Submit(autoStartRequest(string(rune('a' + i)))); err != nil {
			t.Fatalf("running submit %d: %v", i, err)
		}
	}
	for i := 0; i < 8; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("workers did not start")
		}
	}
	for i := 0; i < 8; i++ {
		if err := d.Submit(autoStartRequest(string(rune('i' + i)))); err != nil {
			t.Fatalf("queued submit %d: %v", i, err)
		}
	}
	if err := d.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if got := calls.Load(); got != 8 {
		t.Fatalf("EnsureLive calls after stop = %d, want only 8 running calls", got)
	}
}

func TestAutoStartResultReasonIsStableAndRedacted(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "success", want: "success"},
		{name: "deadline", err: context.DeadlineExceeded, want: "timeout"},
		{name: "play timeout", err: ErrPlayTimeout, want: "timeout"},
		{name: "owner conflict", err: ErrOwnerNodeMismatch, want: "owner-node-mismatch"},
		{name: "required node", err: ErrRequiredNodeUnavailable, want: "required-node-unavailable"},
		{name: "recovery", err: ErrLiveRecoveryPending, want: "recovery-pending"},
		{name: "device offline", err: ErrDeviceOffline, want: "device-offline"},
		{name: "unknown with secret", err: errors.New("play_token=must-not-appear"), want: "ensure-live-failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := autoStartResultReason(test.err); got != test.want {
				t.Fatalf("reason = %q, want %q", got, test.want)
			}
			if got := autoStartResultReason(test.err); got == "play_token=must-not-appear" {
				t.Fatal("reason leaked the raw error")
			}
		})
	}
}
