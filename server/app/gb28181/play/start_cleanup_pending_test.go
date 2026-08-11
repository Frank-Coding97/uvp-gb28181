package play

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

func TestStartRollbackCloseFailureQuarantinesGenerationUntilRetry(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, true)
	withPlayAuthorization(t, true, false)
	z := &mockZLM{port: 40000, closeErr: errors.New("close failed")}
	inviter := &mockInviter{inviteErr: errors.New("invite failed")}
	service, authorization, _, _, _ := newFixedAuthorizationService(t, true, z, inviter)
	preauthorized, err := service.AuthorizeFixedPlayback(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID, "")
	if err != nil {
		t.Fatal(err)
	}
	token := tokenFromFixedAuthorization(t, preauthorized)
	binding := playauth.Binding{
		DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID,
		App: "rtp", Stream: preauthorized.StreamID, MediaServerID: "node-a",
	}
	claims, err := authorization.VerifyForAutoStart(token, binding)
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.EnsureLive(context.Background(), Request{
		DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID,
		Trigger: "on_stream_not_found", RequiredNode: 1,
		AuthorizationID: claims.AuthorizationGeneration,
	})
	if !errors.Is(err, ErrLiveCleanupPending) {
		t.Fatalf("cleanup failure error=%v, want ErrLiveCleanupPending", err)
	}
	coordinator := service.coordinator()
	waitForCloseCalls(t, z, 2)
	result := waitForCleanupPendingResult(t, coordinator, onlineDevice().DeviceID, aChannel().ChannelID)
	if result.Generation == 0 {
		t.Fatalf("cleanup failure did not retain a quarantined generation: %+v", result)
	}
	channels := service.channels.(*fakeChannels)
	if channels.c.StreamID != result.StreamID || channels.c.CurrentSSRC != result.SSRC {
		t.Fatalf("cleanup-pending generation was not persisted for restart recovery: %+v", channels.c)
	}
	if _, ok := service.locationMap.Lookup(result.StreamID); !ok {
		t.Fatal("cleanup failure released the node location")
	}
	if reserveErr := service.ssrcAllocator.Reserve(result.SSRC); !errors.Is(reserveErr, ErrSSRCInUse) {
		t.Fatalf("cleanup failure released SSRC: %v", reserveErr)
	}
	if _, ok := service.CurrentLiveRef(result.StreamID); ok {
		t.Fatal("cleanup-pending generation was exposed as ready media")
	}
	if _, resolveErr := service.ResolvePlaybackMediaContext("rtp", result.StreamID, "node-a"); !errors.Is(resolveErr, ErrPlaybackMediaStateUncertain) {
		t.Fatalf("cleanup-pending generation resolve error=%v, want uncertain", resolveErr)
	}
	if _, coldErr := service.ResolveColdPlaybackMediaContext(context.Background(), "rtp", result.StreamID, "node-a"); !errors.Is(coldErr, ErrPlaybackMediaStateUncertain) {
		t.Fatalf("cleanup-pending cold resolve error=%v, want uncertain", coldErr)
	}
	if _, verifyErr := authorization.VerifyForAutoStart(token, binding); !errors.Is(verifyErr, playauth.ErrAuthorizationTerminal) {
		t.Fatalf("failed generation authorization state=%v, want terminal", verifyErr)
	}

	openCalls := z.openCalls.Load()
	inviteCalls := inviter.inviteCalls.Load()
	const retries = 20
	errs := make([]error, retries)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, errs[index] = service.EnsureLive(context.Background(), Request{
				DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID,
				Trigger: "explicit", RequiredNode: 1,
			})
		}(i)
	}
	wg.Wait()
	for index, retryErr := range errs {
		if !errors.Is(retryErr, ErrLiveCleanupPending) {
			t.Fatalf("cleanup-pending retry %d error=%v", index, retryErr)
		}
	}
	if z.openCalls.Load() != openCalls || inviter.inviteCalls.Load() != inviteCalls {
		t.Fatalf("cleanup-pending retry duplicated media effects: open=%d/%d invite=%d/%d",
			z.openCalls.Load(), openCalls, inviter.inviteCalls.Load(), inviteCalls)
	}

	// The initial bounded retry may still be running while callers observe the
	// quarantine. Wait until it has settled before changing the mock result.
	_ = waitForCleanupPendingResult(t, coordinator, onlineDevice().DeviceID, aChannel().ChannelID)
	z.SetCloseErr(nil)
	if stopErr := service.Stop(context.Background(), result.StreamID); stopErr != nil {
		t.Fatalf("retry cleanup: %v", stopErr)
	}
	if _, ok := service.locationMap.Lookup(result.StreamID); ok {
		t.Fatal("successful cleanup retry retained location")
	}
	if reserveErr := service.ssrcAllocator.Reserve(result.SSRC); reserveErr != nil {
		t.Fatalf("successful cleanup retry did not release SSRC: %v", reserveErr)
	}
	if cold, coldErr := service.ResolveColdPlaybackMediaContext(context.Background(), "rtp", result.StreamID, "node-a"); coldErr != nil || cold.MediaGeneration != 0 {
		t.Fatalf("cleaned offline stream was not eligible for cold authorization: binding=%+v err=%v", cold, coldErr)
	}
}

type observedDoneContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func waitForCleanupPendingResult(t *testing.T, coordinator *Coordinator, deviceID, channelID string) *Result {
	t.Helper()
	key := coordinatorKey{deviceID: deviceID, channelID: channelID}
	deadline := time.After(time.Second)
	for {
		coordinator.mu.Lock()
		entry := coordinator.entries[key]
		if entry != nil && entry.state == LiveStateCleanupPending && entry.result != nil {
			result := cloneResult(entry.result)
			coordinator.mu.Unlock()
			return result
		}
		coordinator.mu.Unlock()
		select {
		case <-deadline:
			t.Fatalf("cleanup did not settle into CleanupPending for %s/%s", deviceID, channelID)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func waitForCloseCalls(t *testing.T, z *mockZLM, want int32) {
	t.Helper()
	deadline := time.After(time.Second)
	for z.closeCalls.Load() < want {
		select {
		case <-deadline:
			t.Fatalf("close calls=%d, want at least %d", z.closeCalls.Load(), want)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func (c *observedDoneContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

func TestCoordinatorStopRetriesCleanupPendingAfterWaitingForStart(t *testing.T) {
	startEntered := make(chan struct{})
	releaseStart := make(chan struct{})
	var stopCalls atomic.Int32
	result := &Result{
		StreamID: "device-1_channel-1", SSRC: "0200000007", Generation: 7,
	}
	coordinator := NewCoordinatorWithStop(func(context.Context, Request) (*Result, error) {
		close(startEntered)
		<-releaseStart
		return result, errors.Join(ErrLiveCleanupPending, errors.New("close failed"))
	}, func(context.Context, *Result) error {
		stopCalls.Add(1)
		return nil
	})
	startErr := make(chan error, 1)
	go func() {
		_, err := coordinator.EnsureLive(context.Background(), Request{DeviceID: "device-1", ChannelID: "channel-1"})
		startErr <- err
	}()
	<-startEntered

	stopCtx := &observedDoneContext{Context: context.Background(), observed: make(chan struct{})}
	stopErr := make(chan error, 1)
	go func() {
		stopErr <- coordinator.Stop(stopCtx, Request{DeviceID: "device-1", ChannelID: "channel-1"})
	}()
	<-stopCtx.observed
	close(releaseStart)
	if err := <-startErr; !errors.Is(err, ErrLiveCleanupPending) {
		t.Fatalf("start error=%v, want cleanup pending", err)
	}
	if err := <-stopErr; err != nil {
		t.Fatalf("stop did not retry pending cleanup: %v", err)
	}
	if stopCalls.Load() != 1 || coordinator.HasTrackedGeneration("device-1", "channel-1") {
		t.Fatalf("pending cleanup was not completed: stopCalls=%d", stopCalls.Load())
	}
}

func TestCoordinatorRetriesCleanupPendingAfterStartPublishes(t *testing.T) {
	failed := &Result{StreamID: "device-1_channel-1", SSRC: "0200000007", Generation: 7, Node: &ResultNode{ID: 3}}
	fresh := &Result{StreamID: "device-1_channel-1", SSRC: "0200000008", Generation: 8, Node: &ResultNode{ID: 3}}
	var starts atomic.Int32
	var stops atomic.Int32
	coordinator := NewCoordinatorWithStop(func(context.Context, Request) (*Result, error) {
		if starts.Add(1) == 1 {
			return failed, errors.Join(ErrLiveCleanupPending, errors.New("first close failed"))
		}
		return fresh, nil
	}, func(context.Context, *Result) error {
		stops.Add(1)
		return nil
	})
	req := Request{DeviceID: "device-1", ChannelID: "channel-1", RequiredNode: 3}

	if _, err := coordinator.EnsureLive(context.Background(), req); !errors.Is(err, ErrLiveCleanupPending) {
		t.Fatalf("first start error=%v, want cleanup pending", err)
	}
	deadline := time.After(time.Second)
	for stops.Load() != 1 || coordinator.HasTrackedGeneration(req.DeviceID, req.ChannelID) {
		select {
		case <-deadline:
			t.Fatalf("published cleanup was not retried: stops=%d tracked=%v", stops.Load(), coordinator.HasTrackedGeneration(req.DeviceID, req.ChannelID))
		default:
			time.Sleep(time.Millisecond)
		}
	}

	result, err := coordinator.EnsureLive(context.Background(), req)
	if err != nil || result != fresh {
		t.Fatalf("ensure after successful cleanup result=%p err=%v, want %p", result, err, fresh)
	}
	if starts.Load() != 2 {
		t.Fatalf("start calls=%d, want 2", starts.Load())
	}
}

func TestStopIfPersistedCurrentRetriesMatchingCleanupPending(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, true)
	withPlayAuthorization(t, true, false)
	z := &mockZLM{port: 40000, closeErr: errors.New("close failed")}
	inviter := &mockInviter{inviteErr: errors.New("invite failed")}
	service, _, _, _, _ := newFixedAuthorizationService(t, true, z, inviter)
	channels := service.channels.(*fakeChannels)
	_, err := service.EnsureLive(context.Background(), Request{
		DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID,
		Trigger: "test", RequiredNode: 1,
	})
	if !errors.Is(err, ErrLiveCleanupPending) {
		t.Fatalf("start error=%v, want cleanup pending", err)
	}
	coordinator := service.coordinator()
	waitForCloseCalls(t, z, 2)
	result := waitForCleanupPendingResult(t, coordinator, onlineDevice().DeviceID, aChannel().ChannelID)
	channels.c.StreamID = result.StreamID
	channels.c.CurrentSSRC = result.SSRC
	_ = waitForCleanupPendingResult(t, coordinator, onlineDevice().DeviceID, aChannel().ChannelID)
	z.SetCloseErr(nil)
	closeCalls := z.closeCalls.Load()

	if err := service.StopIfPersistedCurrent(context.Background(), result.StreamID, result.SSRC); err != nil {
		t.Fatalf("reconcile cleanup pending: %v", err)
	}
	if z.closeCalls.Load() != closeCalls+1 || coordinator.HasTrackedGeneration(onlineDevice().DeviceID, aChannel().ChannelID) {
		t.Fatalf("reconciler did not close tracked pending generation: close=%d/%d", z.closeCalls.Load(), closeCalls+1)
	}
}
