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
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type qualifiedTestRegistry struct {
	nodes []*node.Node
}

func (r qualifiedTestRegistry) Get(id int64) (*node.Node, bool) {
	for _, candidate := range r.nodes {
		if candidate != nil && candidate.ID == id {
			copy := *candidate
			return &copy, true
		}
	}
	return nil, false
}

func (r qualifiedTestRegistry) List() []*node.Node { return r.nodes }

func (r qualifiedTestRegistry) ListActive() []*node.Node {
	active := make([]*node.Node, 0, len(r.nodes))
	for _, candidate := range r.nodes {
		if candidate != nil && candidate.IsActive() {
			active = append(active, candidate)
		}
	}
	return active
}

type countingQualifiedValidator struct {
	calls  atomic.Int32
	failAt int32
}

func (v *countingQualifiedValidator) Validate(_ context.Context, req Request, snapshot NodeQualificationSnapshot) error {
	call := v.calls.Add(1)
	if snapshot.ID != req.RequiredNode || snapshot.MediaServerUUID == "" || snapshot.State != string(node.StateActive) {
		return errors.New("unexpected node snapshot")
	}
	if v.failAt != 0 && call == v.failAt {
		return errors.New("qualification changed")
	}
	return nil
}

func qualifiedTestRequest(id string) Request {
	return Request{
		DeviceID:         onlineDevice().DeviceID,
		ChannelID:        aChannel().ChannelID,
		RequiredNode:     1,
		RequiredProtocol: QualifiedProtocolHTTPSFLV,
		QualificationID:  id,
	}
}

func qualifiedTestNode(id int64, name string) *node.Node {
	return &node.Node{
		ID: id, Revision: 7, Name: name, Host: "192.168.10.22", PlaybackHost: "192.168.10.22",
		MediaServerUUID: name, State: node.StateActive, RTPPortStart: 40000, RTPPortEnd: 41000,
	}
}

func newQualifiedTestService(
	t *testing.T,
	zByNode map[int64]ZLM,
	inv Inviter,
	channels *fakeChannels,
	registry qualifiedTestRegistry,
	locations *stream.LocationMap,
	validator QualifiedNodeValidator,
) (*Service, *stream.Notifier) {
	t.Helper()
	notifier := stream.NewNotifier()
	service := NewWithScheduler(testCfg(), fixedTestPicker{mediaNode: registry.nodes[0]}, registry, locations,
		inv, uac.NewSessionManager(), notifier, fakeDevices{onlineDevice()}, channels,
		WithQualifiedNodeValidator(validator),
		WithURLResolver(NewURLResolver(fakeServerConfigProvider{cfg: node.ServerConfig{HTTPPort: 80, HLSEnabled: true, FMP4Enabled: true}})),
		WithNodeClientFactory(func(mediaNode *node.Node) ZLM { return zByNode[mediaNode.ID] }),
	)
	service.SetReadyTimings(800*time.Millisecond, 10*time.Millisecond)
	return service, notifier
}

func TestQualifiedRequestShapeFailsClosedBeforeCoordinator(t *testing.T) {
	service, _, _ := newFixedSvc(t, &mockZLM{}, &mockInviter{}, onlineDevice(), aChannel())
	request := qualifiedTestRequest("ticket-1")
	_, err := service.EnsureLive(context.Background(), request)
	if !errors.Is(err, ErrQualifiedPlaybackUnavailable) {
		t.Fatalf("missing validator error=%v, want ErrQualifiedPlaybackUnavailable", err)
	}
	if service.coordinator().HasTrackedGeneration(request.DeviceID, request.ChannelID) {
		t.Fatal("missing validator created a tracked live generation")
	}

	for name, mutate := range map[string]func(*Request){
		"missing node":      func(r *Request) { r.RequiredNode = 0 },
		"missing ticket":    func(r *Request) { r.QualificationID = "" },
		"unsupported proto": func(r *Request) { r.RequiredProtocol = "hls" },
		"legacy auth id":    func(r *Request) { r.AuthorizationID = "legacy" },
	} {
		request := qualifiedTestRequest("ticket-" + name)
		mutate(&request)
		if _, err := service.EnsureLive(context.Background(), request); !errors.Is(err, ErrQualifiedRequestInvalid) {
			t.Errorf("%s error=%v, want ErrQualifiedRequestInvalid", name, err)
		}
	}
}

func TestQualifiedCanceledContextDoesNotValidateOrTouchMedia(t *testing.T) {
	withFixedAddressPlaybackSettings(t, false, false)
	z := &mockZLM{}
	inviter := &mockInviter{}
	validator := &countingQualifiedValidator{}
	service, _ := newQualifiedTestService(t, map[int64]ZLM{1: z}, inviter, &fakeChannels{c: aChannel()}, qualifiedTestRegistry{
		nodes: []*node.Node{qualifiedTestNode(1, "node-a")},
	}, stream.NewLocationMap(), validator)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := service.EnsureLive(ctx, qualifiedTestRequest("ticket-canceled"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled qualified request error=%v, want context.Canceled", err)
	}
	if validator.calls.Load() != 0 || z.onlineCalls.Load() != 0 || z.openCalls.Load() != 0 || inviter.inviteCalls.Load() != 0 {
		t.Fatalf("canceled qualified request touched validator/media: validator=%d online=%d open=%d invite=%d",
			validator.calls.Load(), z.onlineCalls.Load(), z.openCalls.Load(), inviter.inviteCalls.Load())
	}
}

func TestQualifiedNilContextUsesBackground(t *testing.T) {
	withFixedAddressPlaybackSettings(t, false, false)
	channel := aChannel()
	channel.StreamID = "nil-context-stream"
	locations := stream.NewLocationMap()
	z := &mockZLM{}
	z.online.Store(true)
	service, _ := newQualifiedTestService(t, map[int64]ZLM{1: z}, &mockInviter{}, &fakeChannels{c: channel}, qualifiedTestRegistry{
		nodes: []*node.Node{qualifiedTestNode(1, "node-a")},
	}, locations, &countingQualifiedValidator{})

	if _, err := service.EnsureLive(nil, qualifiedTestRequest("ticket-nil-context")); err != nil {
		t.Fatalf("nil context qualified reuse: %v", err)
	}
}

func TestQualifiedInactiveNodeFailsBeforeValidator(t *testing.T) {
	withFixedAddressPlaybackSettings(t, false, false)
	mediaNode := qualifiedTestNode(1, "node-a")
	mediaNode.State = node.StateOffline
	z := &mockZLM{}
	validator := &countingQualifiedValidator{}
	service, _ := newQualifiedTestService(t, map[int64]ZLM{1: z}, &mockInviter{}, &fakeChannels{c: aChannel()}, qualifiedTestRegistry{
		nodes: []*node.Node{mediaNode},
	}, stream.NewLocationMap(), validator)

	_, err := service.EnsureLive(context.Background(), qualifiedTestRequest("ticket-inactive"))
	if !errors.Is(err, ErrQualifiedPlaybackUnavailable) {
		t.Fatalf("inactive node error=%v, want ErrQualifiedPlaybackUnavailable", err)
	}
	if validator.calls.Load() != 0 || z.onlineCalls.Load() != 0 {
		t.Fatalf("inactive node reached validator/probe: validator=%d online=%d", validator.calls.Load(), z.onlineCalls.Load())
	}
}

func TestQualifiedCleanupPendingDoesNotTriggerStop(t *testing.T) {
	var stopCalls atomic.Int32
	coordinator := NewCoordinatorWithStop(func(context.Context, Request) (*Result, error) {
		return nil, errors.New("unexpected start")
	}, func(context.Context, *Result) error {
		stopCalls.Add(1)
		return nil
	})
	done := make(chan struct{})
	close(done)
	request := qualifiedTestRequest("ticket-cleanup")
	coordinator.mu.Lock()
	coordinator.entries[coordinatorKey{deviceID: request.DeviceID, channelID: request.ChannelID}] = &coordinatorEntry{
		state: LiveStateCleanupPending, ownerNode: request.RequiredNode,
		result: &Result{StreamID: "stream", Generation: 9}, done: done,
	}
	coordinator.mu.Unlock()

	result, err := coordinator.EnsureLive(context.Background(), request)
	if result != nil || !errors.Is(err, ErrLiveCleanupPending) {
		t.Fatalf("cleanup-pending qualified request result=%+v err=%v", result, err)
	}
	if stopCalls.Load() != 0 {
		t.Fatalf("qualified cleanup-pending request triggered stop %d times", stopCalls.Load())
	}
}

func TestQualifiedOwnerMismatchDoesNotProbeOrCleanup(t *testing.T) {
	withFixedAddressPlaybackSettings(t, false, false)
	streamID := "residual-stream"
	channel := aChannel()
	channel.StreamID = streamID
	locations := stream.NewLocationMap()
	locations.Bind(streamID, 2)
	z1, z2 := &mockZLM{}, &mockZLM{}
	inviter := &mockInviter{}
	validator := &countingQualifiedValidator{}
	service, _ := newQualifiedTestService(t, map[int64]ZLM{1: z1, 2: z2}, inviter, &fakeChannels{c: channel}, qualifiedTestRegistry{
		nodes: []*node.Node{qualifiedTestNode(1, "node-a"), qualifiedTestNode(2, "node-b")},
	}, locations, validator)

	request := qualifiedTestRequest("ticket-owner-mismatch")
	_, err := service.EnsureLive(context.Background(), request)
	var mismatch *OwnerNodeMismatchError
	if !errors.As(err, &mismatch) || !errors.Is(err, ErrOwnerNodeMismatch) {
		t.Fatalf("owner mismatch error=%v", err)
	}
	if z1.onlineCalls.Load() != 0 || z2.onlineCalls.Load() != 0 {
		t.Fatalf("owner mismatch performed probes: z1=%d z2=%d (validator calls=%d)", z1.onlineCalls.Load(), z2.onlineCalls.Load(), validator.calls.Load())
	}
	if z1.closeCalls.Load() != 0 || z2.closeCalls.Load() != 0 || inviter.byeCalls.Load() != 0 {
		t.Fatalf("owner mismatch performed cleanup: z1=%d z2=%d bye=%d", z1.closeCalls.Load(), z2.closeCalls.Load(), inviter.byeCalls.Load())
	}
}

func TestQualifiedUnknownOwnerProbesOnlyRequiredNode(t *testing.T) {
	withFixedAddressPlaybackSettings(t, false, false)
	channel := aChannel()
	channel.StreamID = "residual-stream"
	locations := stream.NewLocationMap()
	z1, z2 := &mockZLM{}, &mockZLM{}
	z2.online.Store(true)
	inviter := &mockInviter{}
	service, _ := newQualifiedTestService(t, map[int64]ZLM{1: z1, 2: z2}, inviter, &fakeChannels{c: channel}, qualifiedTestRegistry{
		nodes: []*node.Node{qualifiedTestNode(1, "node-a"), qualifiedTestNode(2, "node-b")},
	}, locations, &countingQualifiedValidator{})

	_, err := service.EnsureLive(context.Background(), qualifiedTestRequest("ticket-unknown-owner"))
	if !errors.Is(err, ErrQualifiedOwnerUnknown) {
		t.Fatalf("unknown owner error=%v, want ErrQualifiedOwnerUnknown", err)
	}
	if z1.onlineCalls.Load() != 1 || z2.onlineCalls.Load() != 0 {
		t.Fatalf("probe crossed node boundary: required=%d other=%d", z1.onlineCalls.Load(), z2.onlineCalls.Load())
	}
	if z1.closeCalls.Load() != 0 || z2.closeCalls.Load() != 0 || inviter.byeCalls.Load() != 0 {
		t.Fatalf("unknown owner performed cleanup: z1=%d z2=%d bye=%d", z1.closeCalls.Load(), z2.closeCalls.Load(), inviter.byeCalls.Load())
	}
}

func TestQualifiedBoundOwnerOfflineFailsClosedWithoutCleanup(t *testing.T) {
	withFixedAddressPlaybackSettings(t, false, false)
	channel := aChannel()
	channel.StreamID = "bound-offline-stream"
	locations := stream.NewLocationMap()
	locations.Bind(channel.StreamID, 1)
	z := &mockZLM{}
	inviter := &mockInviter{}
	service, _ := newQualifiedTestService(t, map[int64]ZLM{1: z}, inviter, &fakeChannels{c: channel}, qualifiedTestRegistry{
		nodes: []*node.Node{qualifiedTestNode(1, "node-a")},
	}, locations, &countingQualifiedValidator{})

	_, err := service.EnsureLive(context.Background(), qualifiedTestRequest("ticket-bound-offline"))
	if !errors.Is(err, ErrQualifiedOwnerUnknown) {
		t.Fatalf("bound offline owner error=%v, want ErrQualifiedOwnerUnknown", err)
	}
	if z.onlineCalls.Load() != 1 || z.openCalls.Load() != 0 || z.closeCalls.Load() != 0 || inviter.inviteCalls.Load() != 0 || inviter.byeCalls.Load() != 0 {
		t.Fatalf("bound offline owner caused media side effects: online=%d open=%d close=%d invite=%d bye=%d",
			z.onlineCalls.Load(), z.openCalls.Load(), z.closeCalls.Load(), inviter.inviteCalls.Load(), inviter.byeCalls.Load())
	}
}

func TestQualifiedMissingOwnerOnlineRestoresBindingAndReturnsIndependentClones(t *testing.T) {
	withFixedAddressPlaybackSettings(t, false, false)
	channel := aChannel()
	channel.StreamID = "residual-stream"
	channel.CurrentSSRC = "0200000001"
	locations := stream.NewLocationMap()
	z := &mockZLM{}
	z.online.Store(true)
	service, _ := newQualifiedTestService(t, map[int64]ZLM{1: z}, &mockInviter{}, &fakeChannels{c: channel}, qualifiedTestRegistry{
		nodes: []*node.Node{qualifiedTestNode(1, "node-a")},
	}, locations, &countingQualifiedValidator{})

	first, err := service.EnsureLive(context.Background(), qualifiedTestRequest("ticket-a"))
	if err != nil {
		t.Fatalf("first qualified reuse: %v", err)
	}
	second, err := service.EnsureLive(context.Background(), qualifiedTestRequest("ticket-b"))
	if err != nil {
		t.Fatalf("second qualified reuse: %v", err)
	}
	if first == second || first.Node == second.Node {
		t.Fatal("qualified callers received shared mutable result")
	}
	first.Node.Name = "caller-a-mutated"
	if second.Node.Name == first.Node.Name {
		t.Fatal("caller result mutation leaked into another caller")
	}
	if !first.Reused || !second.Reused {
		t.Fatalf("reuse flags first=%v second=%v", first.Reused, second.Reused)
	}
	if owner, ok := locations.Lookup(channel.StreamID); !ok || owner != 1 {
		t.Fatalf("missing-owner online probe did not restore binding: owner=%d ok=%v", owner, ok)
	}
	if z.openCalls.Load() != 0 {
		t.Fatalf("reuse opened RTP %d times", z.openCalls.Load())
	}
}

func TestQualifiedValidationRunsAtEntryAndMediaGates(t *testing.T) {
	for _, test := range []struct {
		name     string
		failAt   int32
		wantOpen int32
	}{
		{name: "entry", failAt: 1, wantOpen: 0},
		{name: "after device and channel", failAt: 2, wantOpen: 0},
		{name: "after node selection", failAt: 3, wantOpen: 0},
		{name: "after ensure", failAt: 4, wantOpen: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			withFixedAddressPlaybackSettings(t, false, false)
			z := &mockZLM{port: 40000}
			inviter := &mockInviter{}
			validator := &countingQualifiedValidator{failAt: test.failAt}
			channel := aChannel()
			service, notifier := newQualifiedTestService(t, map[int64]ZLM{1: z}, inviter, &fakeChannels{c: channel}, qualifiedTestRegistry{
				nodes: []*node.Node{qualifiedTestNode(1, "node-a")},
			}, stream.NewLocationMap(), validator)
			inviter.onInvite = func(session *uac.Session) {
				z.online.Store(true)
				notifier.Publish(session.StreamID)
			}

			_, err := service.EnsureLive(context.Background(), qualifiedTestRequest("ticket-three-gates"))
			if !errors.Is(err, ErrQualifiedPlaybackUnavailable) {
				t.Fatalf("error=%v, want ErrQualifiedPlaybackUnavailable", err)
			}
			if got := validator.calls.Load(); got != test.failAt {
				t.Fatalf("validation calls=%d, want failure at call %d", got, test.failAt)
			}
			if got := z.openCalls.Load(); got != test.wantOpen {
				t.Fatalf("open calls=%d, want %d", got, test.wantOpen)
			}
			if inviter.byeCalls.Load() != 0 || z.closeCalls.Load() != 0 {
				t.Fatalf("qualification failure cleaned media: bye=%d close=%d", inviter.byeCalls.Load(), z.closeCalls.Load())
			}
		})
	}
}

func TestQualifiedColdRequestsShareOneStartAndIndependentResults(t *testing.T) {
	withFixedAddressPlaybackSettings(t, false, false)
	z := &mockZLM{port: 40000}
	inviter := &mockInviter{}
	validator := &countingQualifiedValidator{}
	service, notifier := newQualifiedTestService(t, map[int64]ZLM{1: z}, inviter, &fakeChannels{c: aChannel()}, qualifiedTestRegistry{
		nodes: []*node.Node{qualifiedTestNode(1, "node-a")},
	}, stream.NewLocationMap(), validator)
	inviter.onInvite = func(session *uac.Session) {
		z.online.Store(true)
		notifier.Publish(session.StreamID)
	}

	requests := []Request{qualifiedTestRequest("ticket-a"), qualifiedTestRequest("ticket-b")}
	results := make([]*Result, len(requests))
	errs := make([]error, len(requests))
	var wg sync.WaitGroup
	for i := range requests {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index], errs[index] = service.EnsureLive(context.Background(), requests[index])
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("request %d error=%v", i, err)
		}
	}
	if results[0] == results[1] || results[0].Node == results[1].Node {
		t.Fatal("shared qualified requests returned mutable shared results")
	}
	if results[0].StreamID == "" || results[0].StreamID != results[1].StreamID {
		t.Fatalf("shared stream IDs first=%q second=%q", results[0].StreamID, results[1].StreamID)
	}
	if z.openCalls.Load() != 1 || inviter.inviteCalls.Load() != 1 {
		t.Fatalf("qualified shared lane started media multiple times: open=%d invite=%d", z.openCalls.Load(), inviter.inviteCalls.Load())
	}
}

func TestQualifiedReuseIgnoresNearCapacityForExistingOwner(t *testing.T) {
	withFixedAddressPlaybackSettings(t, false, false)
	channel := aChannel()
	channel.StreamID = "existing-stream"
	channel.CurrentSSRC = "0200000002"
	locations := stream.NewLocationMap()
	locations.Bind(channel.StreamID, 1)
	mediaNode := qualifiedTestNode(1, "node-a")
	mediaNode.Stats.MediaSourceCount = 800
	z := &mockZLM{}
	z.online.Store(true)
	inviter := &mockInviter{}
	service, _ := newQualifiedTestService(t, map[int64]ZLM{1: z}, inviter, &fakeChannels{c: channel}, qualifiedTestRegistry{
		nodes: []*node.Node{mediaNode},
	}, locations, &countingQualifiedValidator{})

	result, err := service.EnsureLive(context.Background(), qualifiedTestRequest("ticket-capacity"))
	if err != nil {
		t.Fatalf("near-capacity existing owner reuse: %v", err)
	}
	if !result.Reused || z.openCalls.Load() != 0 || inviter.inviteCalls.Load() != 0 {
		t.Fatalf("near-capacity owner was not reused safely: result=%+v open=%d invite=%d", result, z.openCalls.Load(), inviter.inviteCalls.Load())
	}
}

func TestQualifiedResultMetadataMustMatchFreshNodeSnapshot(t *testing.T) {
	withFixedAddressPlaybackSettings(t, false, false)
	z := &mockZLM{port: 40000}
	inviter := &mockInviter{}
	mediaNode := qualifiedTestNode(1, "node-a")
	nodes := qualifiedTestRegistry{nodes: []*node.Node{mediaNode}}
	validator := &countingQualifiedValidator{}
	service, notifier := newQualifiedTestService(t, map[int64]ZLM{1: z}, inviter, &fakeChannels{c: aChannel()}, nodes,
		stream.NewLocationMap(), validator)
	inviter.onInvite = func(session *uac.Session) {
		z.online.Store(true)
		notifier.Publish(session.StreamID)
	}
	// The result is built from revision 7. Change the registry after the
	// media generation is ready but before EnsureLive's final fresh read.
	service.liveReady = func(LiveSession) { mediaNode.Revision = 8 }

	_, err := service.EnsureLive(context.Background(), qualifiedTestRequest("ticket-stale-result"))
	if !errors.Is(err, ErrQualifiedPlaybackUnavailable) {
		t.Fatalf("stale result metadata error=%v, want ErrQualifiedPlaybackUnavailable", err)
	}
	if validator.calls.Load() != 3 {
		t.Fatalf("metadata mismatch should stop before final validator, calls=%d", validator.calls.Load())
	}
	if z.openCalls.Load() != 1 || inviter.inviteCalls.Load() != 1 || z.closeCalls.Load() != 0 || inviter.byeCalls.Load() != 0 {
		t.Fatalf("metadata mismatch caused unexpected media compensation: open=%d invite=%d close=%d bye=%d",
			z.openCalls.Load(), inviter.inviteCalls.Load(), z.closeCalls.Load(), inviter.byeCalls.Load())
	}
}
