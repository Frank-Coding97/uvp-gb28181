package play

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"uvplatform.com/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.com/uvp-gb28181/app/gb28181/stream"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
)

type orderedStopChannels struct {
	*fakeChannels
	events   *[]string
	contexts *[]stopContextObservation
}

type stopContextObservation struct {
	err         error
	deadline    time.Time
	hasDeadline bool
}

func observeStopContext(ctx context.Context) stopContextObservation {
	deadline, hasDeadline := ctx.Deadline()
	return stopContextObservation{err: ctx.Err(), deadline: deadline, hasDeadline: hasDeadline}
}

type recoveryNodeRegistry struct {
	nodes []*node.Node
}

func (r recoveryNodeRegistry) Get(id int64) (*node.Node, bool) {
	for _, candidate := range r.nodes {
		if candidate != nil && candidate.ID == id {
			return candidate, true
		}
	}
	return nil, false
}

func (r recoveryNodeRegistry) List() []*node.Node {
	return r.nodes
}

func (r recoveryNodeRegistry) ListActive() []*node.Node {
	active := make([]*node.Node, 0, len(r.nodes))
	for _, candidate := range r.nodes {
		if candidate != nil && candidate.IsActive() {
			active = append(active, candidate)
		}
	}
	return active
}

func (c *orderedStopChannels) ClearIfCurrent(ctx context.Context, streamID, ssrc string) (bool, error) {
	*c.events = append(*c.events, "clear")
	if c.contexts != nil {
		*c.contexts = append(*c.contexts, observeStopContext(ctx))
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return c.fakeChannels.ClearIfCurrent(ctx, streamID, ssrc)
}

type orderedStopZLM struct {
	*mockZLM
	events   *[]string
	contexts *[]stopContextObservation
}

func (z *orderedStopZLM) CloseRtpServer(ctx context.Context, streamID string) error {
	*z.events = append(*z.events, "close")
	if z.contexts != nil {
		*z.contexts = append(*z.contexts, observeStopContext(ctx))
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return z.mockZLM.CloseRtpServer(ctx, streamID)
}

type orderedStopInviter struct {
	*mockInviter
	events *[]string
	onBye  func()
}

func (i *orderedStopInviter) Bye(ctx context.Context, sessions *uac.SessionManager, streamID string) error {
	*i.events = append(*i.events, "bye")
	if i.onBye != nil {
		i.onBye()
	}
	return i.mockInviter.Bye(ctx, sessions, streamID)
}

func newOrderedFixedStopService(t *testing.T, z *orderedStopZLM, inviter *orderedStopInviter, channels *orderedStopChannels) (*Service, *stream.Notifier) {
	t.Helper()
	mediaNode := &node.Node{
		ID: 1, Name: "node-a", Host: "192.168.10.222", PlaybackHost: "192.168.10.222",
		MediaServerUUID: "node-a", State: node.StateActive, RTPPortStart: 40000,
	}
	notifier := stream.NewNotifier()
	service := NewWithScheduler(testCfg(), fixedTestPicker{mediaNode}, fixedTestRegistry{mediaNode}, stream.NewLocationMap(),
		inviter, uac.NewSessionManager(), notifier, fakeDevices{onlineDevice()}, channels,
		WithNodeClientFactory(func(*node.Node) ZLM { return z }),
	)
	service.SetReadyTimings(800*time.Millisecond, 20*time.Millisecond)
	return service, notifier
}

func requireFreshBoundedStopContexts(t *testing.T, contexts []stopContextObservation) {
	t.Helper()
	if len(contexts) != 2 {
		t.Fatalf("cleanup contexts=%v, want active ZLM and CAS contexts", contexts)
	}
	for _, observed := range contexts {
		if observed.err != nil || !observed.hasDeadline {
			t.Fatalf("cleanup context=%+v, want active context with a deadline", observed)
		}
		remaining := time.Until(observed.deadline)
		if remaining <= 0 || remaining > 5*time.Second {
			t.Fatalf("cleanup context deadline window=%v, want (0, 5s]", remaining)
		}
	}
}

func TestServiceStopClearFailureStillClosesMedia(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, false)
	errClear := errors.New("clear failed")
	z := &mockZLM{port: 40000}
	inviter := &mockInviter{}
	service, notifier, channels := newFixedSvc(t, z, inviter, onlineDevice(), aChannel())
	channels.clearErr = errClear
	inviter.onInvite = func(session *uac.Session) {
		z.online.Store(true)
		go notifier.Publish(session.StreamID)
	}

	result, err := service.Start(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Stop(context.Background(), result.StreamID, "", ""); !errors.Is(err, errClear) {
		t.Fatalf("stop error=%v, want clear failure", err)
	}
	if z.closeCalls.Load() != 1 || inviter.byeCalls.Load() != 1 {
		t.Fatalf("clear failure left media cleanup unattempted: close=%d bye=%d", z.closeCalls.Load(), inviter.byeCalls.Load())
	}
	if _, ok := service.CurrentLiveRef(result.StreamID); ok {
		t.Fatal("media close succeeded but coordinator retained the generation")
	}
}

func TestServiceStopByeFailureClosesBeforeClearingPersistence(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, false)
	errBye := errors.New("bye failed")
	events := make([]string, 0, 3)
	baseZLM := &mockZLM{port: 40000}
	z := &orderedStopZLM{mockZLM: baseZLM, events: &events}
	baseInviter := &mockInviter{byeErr: errBye}
	inviter := &orderedStopInviter{mockInviter: baseInviter, events: &events}
	channels := &orderedStopChannels{fakeChannels: &fakeChannels{c: aChannel()}, events: &events}
	service, notifier := newOrderedFixedStopService(t, z, inviter, channels)
	baseInviter.onInvite = func(session *uac.Session) {
		baseZLM.online.Store(true)
		go notifier.Publish(session.StreamID)
	}

	result, err := service.Start(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Stop(context.Background(), result.StreamID, "", ""); !errors.Is(err, errBye) {
		t.Fatalf("stop error=%v, want BYE failure", err)
	} else if !IsMediaStopCompleted(err) {
		// 关流成功 ⇒ 媒体确实停了,只剩 BYE 没走通。这个标记必须立起来,
		// 否则 HTTP 停播口会把"对端推流器早就没了"判成 400(P-021)。
		t.Fatalf("BYE-only failure not marked as completed media stop: %v", err)
	}
	if !reflect.DeepEqual(events, []string{"bye", "close", "clear"}) {
		t.Fatalf("stop order=%v, want BYE then close then persistence cleanup", events)
	}
	if _, ok := service.CurrentLiveRef(result.StreamID); ok {
		t.Fatal("media close succeeded but coordinator retained the generation")
	}
}

func TestStopCurrentResultUsesFreshCleanupContextsAfterByeCancellation(t *testing.T) {
	events := make([]string, 0, 3)
	contexts := make([]stopContextObservation, 0, 2)
	channel := aChannel()
	channel.StreamID = "stream-current"
	channel.CurrentSSRC = "0100000001"
	z := &orderedStopZLM{mockZLM: &mockZLM{port: 40000}, events: &events, contexts: &contexts}
	inviter := &orderedStopInviter{mockInviter: &mockInviter{}, events: &events}
	channels := &orderedStopChannels{fakeChannels: &fakeChannels{c: channel}, events: &events, contexts: &contexts}
	service, _ := newOrderedFixedStopService(t, z, inviter, channels)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inviter.onBye = cancel
	result := &Result{
		StreamID:   channel.StreamID,
		SSRC:       channel.CurrentSSRC,
		Generation: 1,
		Node:       &ResultNode{ID: 1},
	}

	if err := service.stopCurrentResult(ctx, result); err != nil {
		t.Fatalf("stop after BYE cancellation: %v", err)
	}
	if !reflect.DeepEqual(events, []string{"bye", "close", "clear"}) {
		t.Fatalf("stop order=%v, want BYE then close then persistence cleanup", events)
	}
	requireFreshBoundedStopContexts(t, contexts)
}

func TestStopDirectUsesFreshCleanupContextsAfterByeCancellation(t *testing.T) {
	events := make([]string, 0, 3)
	contexts := make([]stopContextObservation, 0, 2)
	streamID := "stream-direct"
	channel := aChannel()
	channel.StreamID = streamID
	channel.CurrentSSRC = "0100000001"
	z := &orderedStopZLM{mockZLM: &mockZLM{port: 40000}, events: &events, contexts: &contexts}
	inviter := &orderedStopInviter{mockInviter: &mockInviter{}, events: &events}
	channels := &orderedStopChannels{fakeChannels: &fakeChannels{c: channel}, events: &events, contexts: &contexts}
	service, _, _ := newSvc(t, z, inviter, onlineDevice(), channel)
	service.channels = channels
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inviter.onBye = cancel

	if err := service.stopDirect(ctx, streamID); err != nil {
		t.Fatalf("stop direct after BYE cancellation: %v", err)
	}
	if !reflect.DeepEqual(events, []string{"bye", "close", "clear"}) {
		t.Fatalf("stop order=%v, want BYE then close then persistence cleanup", events)
	}
	requireFreshBoundedStopContexts(t, contexts)
}

func TestServiceStopCloseFailureKeepsGenerationTrackableAndRetryable(t *testing.T) {
	withFixedAddressPlaybackSettings(t, true, true)
	withPlayAuthorization(t, true, false)
	errClose := errors.New("close failed")
	z := &mockZLM{port: 40000, closeErr: errClose}
	inviter := &mockInviter{}
	service, authorization, _, _, notifier := newFixedAuthorizationService(t, true, z, inviter)
	inviter.onInvite = func(session *uac.Session) {
		z.online.Store(true)
		go notifier.Publish(session.StreamID)
	}

	preauthorized, err := service.AuthorizeFixedPlayback(context.Background(), AuthorizedRequest{DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID, ClientIP: "", DeviceEpoch: 1})
	if err != nil {
		t.Fatal(err)
	}
	token := tokenFromFixedAuthorization(t, preauthorized)
	binding := playauth.Binding{DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID, App: "rtp", Stream: preauthorized.StreamID, MediaServerID: "node-a"}
	claims, err := authorization.VerifyForAutoStart(token, binding)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.EnsureLive(context.Background(), Request{
		DeviceID: onlineDevice().DeviceID, ChannelID: aChannel().ChannelID, Trigger: "test", RequiredNode: 1,
		AuthorizationID: claims.AuthorizationGeneration, DeviceEpoch: claims.DeviceEpoch,
	})
	if err != nil {
		t.Fatal(err)
	}
	bound := binding
	bound.MediaGeneration = result.Generation

	if err := service.Stop(context.Background(), result.StreamID, "", ""); !errors.Is(err, errClose) {
		t.Fatalf("stop error=%v, want close failure", err)
	} else if IsMediaStopCompleted(err) {
		// 关流没走通 = 媒体状态未知。这个标记一旦立起来,停播口就会把真失败
		// 报成成功,通道从此打不开 —— 必须保持"未完成"。
		t.Fatalf("close failure must not be marked as completed media stop: %v", err)
	}
	if current, ok := service.CurrentLiveRef(result.StreamID); !ok || current != resultLiveRef(result) {
		t.Fatalf("close failure lost tracked generation: current=%+v ok=%v", current, ok)
	}
	channels := service.channels.(*fakeChannels)
	if channels.c.StreamID != result.StreamID || channels.c.CurrentSSRC != result.SSRC {
		t.Fatalf("close failure cleared persisted generation: %+v", channels.c)
	}
	locationMap := service.locationMap.(*stream.LocationMap)
	if current, ok := locationMap.LookupCurrent(result.StreamID); !ok || current != resultLiveRef(result) {
		t.Fatalf("close failure unbound location: current=%+v ok=%v", current, ok)
	}
	if err := service.ssrcAllocator.Reserve(result.SSRC); !errors.Is(err, ErrSSRCInUse) {
		t.Fatalf("close failure released SSRC lease: reserve err=%v", err)
	}
	if _, err := authorization.Verify(token, bound); err != nil {
		t.Fatalf("close failure terminated authorization: %v", err)
	}

	z.SetCloseErr(nil)
	if err := service.Stop(context.Background(), result.StreamID, "", ""); err != nil {
		t.Fatalf("retry after close failure: %v", err)
	}
	if _, ok := service.CurrentLiveRef(result.StreamID); ok {
		t.Fatal("successful retry retained the generation")
	}
	if _, err := authorization.Verify(token, bound); !errors.Is(err, playauth.ErrAuthorizationTerminal) {
		t.Fatalf("successful retry authorization state=%v, want terminal", err)
	}
}

func TestStopIfPersistedCurrentClosesInactiveRegisteredOwnerBeforeCAS(t *testing.T) {
	streamID, err := FixedStreamID(onlineDevice().DeviceID, aChannel().ChannelID)
	if err != nil {
		t.Fatal(err)
	}
	active := &node.Node{ID: 1, Name: "active", Host: "192.168.10.221", State: node.StateActive}
	inactive := &node.Node{ID: 2, Name: "stale-owner", Host: "192.168.10.222", State: node.StateOffline}
	activeClient := &mockZLM{port: 40000}
	inactiveClient := &mockZLM{port: 40000}
	channel := aChannel()
	channel.StreamID = streamID
	channel.CurrentSSRC = "0200000007"
	channels := &fakeChannels{c: channel}
	service := NewWithScheduler(testCfg(), fixedTestPicker{active}, recoveryNodeRegistry{nodes: []*node.Node{active, inactive}}, stream.NewLocationMap(),
		&mockInviter{}, uac.NewSessionManager(), stream.NewNotifier(), fakeDevices{onlineDevice()}, channels,
		WithNodeClientFactory(func(mediaNode *node.Node) ZLM {
			if mediaNode.ID == inactive.ID {
				return inactiveClient
			}
			return activeClient
		}),
	)

	if err := service.StopIfPersistedCurrent(context.Background(), streamID, channel.CurrentSSRC); err != nil {
		t.Fatalf("close persisted stream: %v", err)
	}
	if activeClient.closeCalls.Load() != 1 || inactiveClient.closeCalls.Load() != 1 {
		t.Fatalf("cleanup did not cover every registered node: active=%d inactive=%d", activeClient.closeCalls.Load(), inactiveClient.closeCalls.Load())
	}
	if channel.StreamID != "" || channel.CurrentSSRC != "" {
		t.Fatalf("CAS clear ran before all known owners were closed: %+v", channel)
	}
}

func TestStopIfPersistedCurrentKeepsCASWhenInactiveOwnerCannotClose(t *testing.T) {
	streamID, err := FixedStreamID(onlineDevice().DeviceID, aChannel().ChannelID)
	if err != nil {
		t.Fatal(err)
	}
	active := &node.Node{ID: 1, Name: "active", Host: "192.168.10.221", State: node.StateActive}
	inactive := &node.Node{ID: 2, Name: "stale-owner", Host: "192.168.10.222", State: node.StateOffline}
	activeClient := &mockZLM{port: 40000}
	closeErr := errors.New("inactive owner unavailable")
	inactiveClient := &mockZLM{port: 40000, closeErr: closeErr}
	channel := aChannel()
	channel.StreamID = streamID
	channel.CurrentSSRC = "0200000007"
	channels := &fakeChannels{c: channel}
	service := NewWithScheduler(testCfg(), fixedTestPicker{active}, recoveryNodeRegistry{nodes: []*node.Node{active, inactive}}, stream.NewLocationMap(),
		&mockInviter{}, uac.NewSessionManager(), stream.NewNotifier(), fakeDevices{onlineDevice()}, channels,
		WithNodeClientFactory(func(mediaNode *node.Node) ZLM {
			if mediaNode.ID == inactive.ID {
				return inactiveClient
			}
			return activeClient
		}),
	)

	if err := service.StopIfPersistedCurrent(context.Background(), streamID, channel.CurrentSSRC); !errors.Is(err, closeErr) {
		t.Fatalf("close persisted stream error=%v, want inactive owner failure", err)
	}
	if inactiveClient.closeCalls.Load() != 1 {
		t.Fatalf("inactive owner was not probed: close=%d", inactiveClient.closeCalls.Load())
	}
	if channel.StreamID != streamID || channel.CurrentSSRC != "0200000007" {
		t.Fatalf("cleanup cleared uncertain owner state: %+v", channel)
	}
}
