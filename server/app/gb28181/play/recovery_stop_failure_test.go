package play

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
	"uvplatform.cn/uvp-gb28181/app/gb28181/stream"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type orderedStopChannels struct {
	*fakeChannels
	events *[]string
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
	return c.fakeChannels.ClearIfCurrent(ctx, streamID, ssrc)
}

type orderedStopZLM struct {
	*mockZLM
	events *[]string
}

func (z *orderedStopZLM) CloseRtpServer(ctx context.Context, streamID string) error {
	*z.events = append(*z.events, "close")
	return z.mockZLM.CloseRtpServer(ctx, streamID)
}

type orderedStopInviter struct {
	*mockInviter
	events *[]string
}

func (i *orderedStopInviter) Bye(ctx context.Context, sessions *uac.SessionManager, streamID string) error {
	*i.events = append(*i.events, "bye")
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
	if err := service.Stop(context.Background(), result.StreamID); !errors.Is(err, errClear) {
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
	if err := service.Stop(context.Background(), result.StreamID); !errors.Is(err, errBye) {
		t.Fatalf("stop error=%v, want BYE failure", err)
	}
	if !reflect.DeepEqual(events, []string{"bye", "close", "clear"}) {
		t.Fatalf("stop order=%v, want BYE then close then persistence cleanup", events)
	}
	if _, ok := service.CurrentLiveRef(result.StreamID); ok {
		t.Fatal("media close succeeded but coordinator retained the generation")
	}
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

	preauthorized, err := service.AuthorizeFixedPlayback(context.Background(), onlineDevice().DeviceID, aChannel().ChannelID, "")
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
		AuthorizationID: claims.AuthorizationGeneration,
	})
	if err != nil {
		t.Fatal(err)
	}
	bound := binding
	bound.MediaGeneration = result.Generation

	if err := service.Stop(context.Background(), result.StreamID); !errors.Is(err, errClose) {
		t.Fatalf("stop error=%v, want close failure", err)
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
	if err := service.Stop(context.Background(), result.StreamID); err != nil {
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
