package play

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

func coordinatorRequest(device, channel string) Request {
	return Request{DeviceID: device, ChannelID: channel, Trigger: "test"}
}

func TestCoordinatorConcurrentEnsureSharesOneStart(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	result := &Result{StreamID: "stream-1", SSRC: "ssrc-1", Node: &ResultNode{ID: 7}}
	c := NewCoordinator(func(ctx context.Context, req Request) (*Result, error) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			return result, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})

	const n = 20
	results := make([]*Result, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = c.EnsureLive(context.Background(), coordinatorRequest("device", "channel"))
		}(i)
	}
	<-started
	close(release)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("start calls = %d, want 1", got)
	}
	for i := range results {
		if errs[i] != nil || results[i] != result {
			t.Fatalf("request %d got result=%p err=%v, want shared result=%p", i, results[i], errs[i], result)
		}
	}
	if got, err := c.EnsureLive(context.Background(), coordinatorRequest("device", "channel")); err != nil || got != result {
		t.Fatalf("ready reuse got result=%p err=%v", got, err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("ready reuse start calls = %d, want 1", got)
	}
}

func TestCoordinatorWaiterCancellationDoesNotCancelOwner(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	c := NewCoordinator(func(ctx context.Context, req Request) (*Result, error) {
		calls.Add(1)
		close(started)
		<-release
		return &Result{StreamID: "stream-1"}, nil
	})

	ownerDone := make(chan error, 1)
	go func() {
		_, err := c.EnsureLive(context.Background(), coordinatorRequest("device", "channel"))
		ownerDone <- err
	}()
	<-started

	waiterCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.EnsureLive(waiterCtx, coordinatorRequest("device", "channel")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter err = %v, want context.Canceled", err)
	}
	close(release)
	if err := <-ownerDone; err != nil {
		t.Fatalf("owner err = %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("start calls = %d, want 1", got)
	}
}

func TestCoordinatorRequiredNodeConflictDoesNotStart(t *testing.T) {
	var calls atomic.Int32
	c := NewCoordinator(func(context.Context, Request) (*Result, error) {
		calls.Add(1)
		return &Result{StreamID: "stream-1", Node: &ResultNode{ID: 2}}, nil
	})
	if _, err := c.EnsureLive(context.Background(), coordinatorRequest("device", "channel")); err != nil {
		t.Fatalf("initial ensure: %v", err)
	}
	request := coordinatorRequest("device", "channel")
	request.RequiredNode = 3
	_, err := c.EnsureLive(context.Background(), request)
	var conflict *OwnerNodeMismatchError
	if !errors.As(err, &conflict) || !errors.Is(err, ErrOwnerNodeMismatch) {
		t.Fatalf("conflict err = %v, want typed owner-node-mismatch", err)
	}
	if conflict.OwnerNode != 2 || conflict.RequiredNode != 3 {
		t.Fatalf("conflict = %+v", conflict)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("conflicting ensure start calls = %d, want 1", got)
	}
}

func TestCoordinatorFailureReturnsSharedErrorAndAllowsRetry(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	start := func(context.Context, Request) (*Result, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
			return nil, errors.New("start failed")
		}
		return &Result{StreamID: "stream-2"}, nil
	}
	c := NewCoordinator(start)
	const n = 4
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, errs[0] = c.EnsureLive(context.Background(), coordinatorRequest("device", "channel"))
	}()
	<-started
	for i := 0; i < n; i++ {
		if i == 0 {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = c.EnsureLive(context.Background(), coordinatorRequest("device", "channel"))
		}(i)
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	for i, err := range errs {
		if err == nil || err.Error() != "start failed" {
			t.Fatalf("request %d err = %v, want shared failure", i, err)
		}
	}
	if _, err := c.EnsureLive(context.Background(), coordinatorRequest("device", "channel")); err != nil {
		t.Fatalf("retry err = %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("start calls = %d, want 2", got)
	}
}

func TestCoordinatorDifferentChannelsStartConcurrently(t *testing.T) {
	var active atomic.Int32
	var maxActive atomic.Int32
	release := make(chan struct{})
	c := NewCoordinator(func(context.Context, Request) (*Result, error) {
		current := active.Add(1)
		for {
			old := maxActive.Load()
			if current <= old || maxActive.CompareAndSwap(old, current) {
				break
			}
		}
		<-release
		active.Add(-1)
		return &Result{StreamID: "stream"}, nil
	})
	results := make(chan error, 2)
	go func() {
		_, err := c.EnsureLive(context.Background(), coordinatorRequest("device", "channel-a"))
		results <- err
	}()
	go func() {
		_, err := c.EnsureLive(context.Background(), coordinatorRequest("device", "channel-b"))
		results <- err
	}()
	deadline := time.After(time.Second)
	for maxActive.Load() < 2 {
		select {
		case <-deadline:
			t.Fatal("different channels did not start concurrently")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatalf("ensure %d err = %v", i, err)
		}
	}
}

func TestCoordinatorStoppingIsBarrier(t *testing.T) {
	stopStarted := make(chan struct{})
	stopRelease := make(chan struct{})
	var startCalls atomic.Int32
	var stopCalls atomic.Int32
	c := NewCoordinatorWithStop(
		func(context.Context, Request) (*Result, error) {
			startCalls.Add(1)
			return &Result{StreamID: "stream-1"}, nil
		},
		func(context.Context, *Result) error {
			stopCalls.Add(1)
			close(stopStarted)
			<-stopRelease
			return nil
		},
	)
	request := coordinatorRequest("device", "channel")
	if _, err := c.EnsureLive(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	stopDone := make(chan error, 1)
	go func() { stopDone <- c.Stop(context.Background(), request) }()
	<-stopStarted
	ensureDone := make(chan error, 1)
	go func() {
		_, err := c.EnsureLive(context.Background(), request)
		ensureDone <- err
	}()
	select {
	case err := <-ensureDone:
		t.Fatalf("ensure crossed stopping barrier with err=%v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(stopRelease)
	if err := <-stopDone; err != nil {
		t.Fatalf("stop err = %v", err)
	}
	if err := <-ensureDone; err != nil {
		t.Fatalf("ensure after stop err = %v", err)
	}
	if startCalls.Load() != 2 || stopCalls.Load() != 1 {
		t.Fatalf("start calls=%d stop calls=%d, want 2/1", startCalls.Load(), stopCalls.Load())
	}
}

func TestCoordinatorRestoreReusesRecoveredGeneration(t *testing.T) {
	var startCalls atomic.Int32
	recovered := &Result{
		StreamID: "device_channel", SSRC: "0200000001", Generation: 7,
		Node: &ResultNode{ID: 9}, ModeAtStart: LiveModeFixed,
	}
	c := NewCoordinator(func(context.Context, Request) (*Result, error) {
		startCalls.Add(1)
		return nil, errors.New("must not start")
	})
	req := coordinatorRequest("device", "channel")
	req.RequiredNode = 9
	if !c.Restore(req, recovered) {
		t.Fatal("recovered generation was not restored")
	}
	got, err := c.EnsureLive(context.Background(), req)
	if err != nil || got != recovered {
		t.Fatalf("recovered ensure result=%p err=%v", got, err)
	}
	if startCalls.Load() != 0 {
		t.Fatalf("recovered generation started %d times", startCalls.Load())
	}
}

func TestCoordinatorConditionalStopRejectsStaleGeneration(t *testing.T) {
	var stopCalls atomic.Int32
	current := &Result{
		StreamID: "device_channel", SSRC: "0200000002", Generation: 2,
		Node: &ResultNode{ID: 20}, ModeAtStart: LiveModeFixed,
	}
	c := NewCoordinatorWithStop(
		func(context.Context, Request) (*Result, error) { return current, nil },
		func(context.Context, *Result) error { stopCalls.Add(1); return nil },
	)
	req := coordinatorRequest("device", "channel")
	if _, err := c.EnsureLive(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	stale := stream.LiveRef{StreamID: current.StreamID, SSRC: "0200000001", Generation: 1, NodeID: 10}
	handled, err := c.StopIfCurrent(context.Background(), stale)
	if err != nil || !handled {
		t.Fatalf("stale stop handled=%v err=%v", handled, err)
	}
	if stopCalls.Load() != 0 {
		t.Fatalf("stale generation stopped current stream %d times", stopCalls.Load())
	}
	if got, ok := c.CurrentResult(current.StreamID); !ok || !resultMatchesRef(got, resultLiveRef(current)) {
		t.Fatalf("current generation disappeared: got=%+v ok=%v", got, ok)
	}
}

func TestCoordinatorConditionalStopIsIdempotentForCurrentGeneration(t *testing.T) {
	var stopCalls atomic.Int32
	current := &Result{
		StreamID: "device_channel", SSRC: "0200000002", Generation: 2,
		Node: &ResultNode{ID: 20}, ModeAtStart: LiveModeFixed,
	}
	c := NewCoordinatorWithStop(
		func(context.Context, Request) (*Result, error) { return current, nil },
		func(context.Context, *Result) error { stopCalls.Add(1); return nil },
	)
	if _, err := c.EnsureLive(context.Background(), coordinatorRequest("device", "channel")); err != nil {
		t.Fatal(err)
	}
	ref := stream.LiveRef{StreamID: current.StreamID, SSRC: current.SSRC, Generation: current.Generation, NodeID: current.Node.ID}
	if handled, err := c.StopIfCurrent(context.Background(), ref); err != nil || !handled {
		t.Fatalf("current stop handled=%v err=%v", handled, err)
	}
	if handled, err := c.StopIfCurrent(context.Background(), ref); err != nil || handled {
		t.Fatalf("repeated stop handled=%v err=%v", handled, err)
	}
	if stopCalls.Load() != 1 {
		t.Fatalf("current generation stopped %d times", stopCalls.Load())
	}
}

func TestServiceStartConcurrentUsesCoordinator(t *testing.T) {
	z := &mockZLM{port: 40000}
	inv := &mockInviter{}
	s, notifier, _ := newSvc(t, z, inv, onlineDevice(), aChannel())
	inv.onInvite = func(sess *uac.Session) {
		z.online.Store(true)
		go func(streamID string) {
			time.Sleep(20 * time.Millisecond)
			notifier.Publish(streamID)
		}(sess.StreamID)
	}

	const n = 20
	results := make([]*Result, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = s.Start(context.Background(), "34020000001320000002", "12345678911116666661")
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("request %d err = %v", i, err)
		}
		if results[i].StreamID != results[0].StreamID || results[i].SSRC != results[0].SSRC ||
			results[i].Generation != results[0].Generation {
			t.Fatalf("request %d returned a different live generation", i)
		}
	}
	if inv.inviteCalls.Load() != 1 || z.openCalls.Load() != 1 {
		t.Fatalf("invite calls=%d open calls=%d, want 1/1", inv.inviteCalls.Load(), z.openCalls.Load())
	}
}

func TestServiceStopReleasesCoordinatorGeneration(t *testing.T) {
	z := &mockZLM{port: 40000}
	inv := &mockInviter{}
	s, notifier, _ := newSvc(t, z, inv, onlineDevice(), aChannel())
	inv.onInvite = func(sess *uac.Session) {
		z.online.Store(true)
		go func(streamID string) {
			time.Sleep(10 * time.Millisecond)
			notifier.Publish(streamID)
		}(sess.StreamID)
	}
	stream, err := s.Start(context.Background(), "34020000001320000002", "12345678911116666661")
	if err != nil {
		t.Fatalf("first start: %v", err)
	}
	z.online.Store(false)
	if err := s.Stop(context.Background(), stream.StreamID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if _, err := s.Start(context.Background(), "34020000001320000002", "12345678911116666661"); err != nil {
		t.Fatalf("second start: %v", err)
	}
	if inv.inviteCalls.Load() != 2 || z.openCalls.Load() != 2 {
		t.Fatalf("invite calls=%d open calls=%d, want 2/2", inv.inviteCalls.Load(), z.openCalls.Load())
	}
}
