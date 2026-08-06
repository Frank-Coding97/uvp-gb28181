package playback

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeNodePicker struct {
	node  NodeInfo
	err   error
	calls atomic.Int32
}

func (f *fakeNodePicker) Pick(context.Context, PickRequest) (NodeInfo, error) {
	f.calls.Add(1)
	return f.node, f.err
}

type fakeRTP struct {
	openErr     error
	openCalls   atomic.Int32
	closeCalls  atomic.Int32
	bindCalls   atomic.Int32
	unbindCalls atomic.Int32
	bound       bool
}

func (f *fakeRTP) Open(_ context.Context, request RTPRequest) (RTPAllocation, error) {
	f.openCalls.Add(1)
	if f.openErr != nil {
		return RTPAllocation{}, f.openErr
	}
	return RTPAllocation{StreamID: "pb-stream-1", SSRC: request.SSRC, Port: 30000,
		Close:  func(context.Context) error { f.closeCalls.Add(1); return nil },
		Bind:   func() error { f.bindCalls.Add(1); f.bound = true; return nil },
		Unbind: func() error { f.unbindCalls.Add(1); f.bound = false; return nil }}, nil
}

type fakeInvite struct {
	err         error
	actionErr   error
	calls       atomic.Int32
	actionCalls atomic.Int32
	last        UACInvite
	teardown    atomic.Int32
}

func (f *fakeInvite) Invite(_ context.Context, in UACInvite) (DialogInfo, error) {
	f.calls.Add(1)
	f.last = in
	if f.err != nil {
		return DialogInfo{}, f.err
	}
	return DialogInfo{CallID: "playback-call-1"}, nil
}
func (f *fakeInvite) Teardown(context.Context, string) error { f.teardown.Add(1); return nil }
func (f *fakeInvite) Action(context.Context, string, string, float64, float64, time.Duration) (float64, float64, error) {
	f.actionCalls.Add(1)
	if f.actionErr != nil {
		return 0, 0, f.actionErr
	}
	return 12, 2, nil
}

type fakeMedia struct {
	ready MediaReady
	err   error
	calls atomic.Int32
	wait  <-chan struct{}
}

func (f *fakeMedia) Wait(ctx context.Context, _ string) (MediaReady, error) {
	f.calls.Add(1)
	if f.wait != nil {
		select {
		case <-ctx.Done():
			return MediaReady{}, ctx.Err()
		case <-f.wait:
		}
	}
	return f.ready, f.err
}

func validCreate(now time.Time) CreateRequest {
	return playbackRequest(now, "u1", "c1", "record-1")
}

func TestPlaybackServiceCreateOpensRTPBeforeInviteAndWaitsForMedia(t *testing.T) {
	now := time.Unix(1700000000, 0)
	picker := &fakeNodePicker{node: NodeInfo{ID: "node-1", ServerID: "34020000002000000001", Destination: "192.0.2.20:5060", RecvIP: "192.0.2.10"}}
	rtp := &fakeRTP{}
	invite := &fakeInvite{}
	media := &fakeMedia{ready: MediaReady{URLs: map[string]string{"wsFlv": "ws://node/live.flv"}, HasAudio: true}}
	service := NewService(NewRegistry(RegistryConfig{Now: func() time.Time { return now }}), picker, rtp, invite, media,
		ServiceConfig{ServerID: "34020000002000000001"})
	request := validCreate(now)
	request.SIPChannelID = "34020000001320000001"
	request.Destination = "192.0.2.20:5060"
	request.Transport = "UDP"
	result, err := service.Create(context.Background(), request)
	if err != nil || result.Session.State != StatePlaying || !result.Session.HasAudio || result.Session.MediaURLs["wsFlv"] == "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if invite.calls.Load() != 1 || rtp.openCalls.Load() != 1 || rtp.bindCalls.Load() != 1 || media.calls.Load() != 1 {
		t.Fatalf("calls invite=%d open=%d bind=%d media=%d", invite.calls.Load(), rtp.openCalls.Load(), rtp.bindCalls.Load(), media.calls.Load())
	}
	if invite.last.SDP == "" || invite.last.SDP[:1] != "v" {
		t.Fatalf("sdp=%q", invite.last.SDP)
	}
	if invite.last.ChannelID != request.SIPChannelID {
		t.Fatalf("invite channel=%q want=%q", invite.last.ChannelID, request.SIPChannelID)
	}
	assertValidPlaybackSSRC(t, invite.last.SSRC)
	if want := "\r\ny=" + invite.last.SSRC + "\r\n"; !strings.Contains(invite.last.SDP, want) {
		t.Fatalf("sdp=%q, want SSRC line %q", invite.last.SDP, want)
	}
}

func TestPlaybackServiceIdempotentCreateReportsExisting(t *testing.T) {
	now := time.Unix(1700000000, 0)
	service := NewService(NewRegistry(RegistryConfig{Now: func() time.Time { return now }}),
		&fakeNodePicker{node: NodeInfo{ID: "node-1", ServerID: "34020000002000000001", Destination: "192.0.2.20:5060", RecvIP: "192.0.2.10"}},
		&fakeRTP{}, &fakeInvite{}, &fakeMedia{ready: MediaReady{}}, ServiceConfig{})
	first, err := service.Create(context.Background(), validCreate(now))
	if err != nil || first.Existing {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	repeat, err := service.Create(context.Background(), validCreate(now))
	if err != nil || !repeat.Existing || repeat.Session.ID != first.Session.ID {
		t.Fatalf("repeat=%+v err=%v", repeat, err)
	}
}

func TestPlaybackServiceIdempotentCreateSurvivesFreshRecordSnapshot(t *testing.T) {
	now := time.Unix(1700000000, 0)
	service := NewService(NewRegistry(RegistryConfig{Now: func() time.Time { return now }}),
		&fakeNodePicker{node: NodeInfo{ID: "node-1", ServerID: "34020000002000000001", Destination: "192.0.2.20:5060", RecvIP: "192.0.2.10"}},
		&fakeRTP{}, &fakeInvite{}, &fakeMedia{ready: MediaReady{URLs: map[string]string{"wsFlv": "ws://node/live.flv"}}}, ServiceConfig{})
	firstRequest := validCreate(now)
	firstRequest.IdempotencyKey = "playback-31-stable-segment"
	first, err := service.Create(context.Background(), firstRequest)
	if err != nil {
		t.Fatal(err)
	}
	secondRequest := firstRequest
	secondRequest.RecordKey = "fresh-query-snapshot-key"
	secondRequest.IdempotencyKey = "playback-31-new-client-key"
	second, err := service.Create(context.Background(), secondRequest)
	if err != nil || !second.Existing || second.Session.ID != first.Session.ID || second.Session.MediaURLs["wsFlv"] == "" {
		t.Fatalf("second=%+v err=%v", second, err)
	}
}

func TestPlaybackServiceMediaWaitUsesConfiguredDeadline(t *testing.T) {
	now := time.Unix(1700000000, 0)
	service := NewService(NewRegistry(RegistryConfig{Now: func() time.Time { return now }}),
		&fakeNodePicker{node: NodeInfo{ID: "node-1", ServerID: "34020000002000000001", Destination: "192.0.2.20:5060", RecvIP: "192.0.2.10"}},
		&fakeRTP{}, &fakeInvite{}, &fakeMedia{wait: make(chan struct{})}, ServiceConfig{MediaWait: time.Millisecond})
	_, err := service.Create(context.Background(), validCreate(now))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
}

func TestPlaybackServiceNodeOrRTPFailureDoesNotInvite(t *testing.T) {
	now := time.Unix(1700000000, 0)
	picker := &fakeNodePicker{err: errors.New("no active node")}
	rtp, invite, media := &fakeRTP{}, &fakeInvite{}, &fakeMedia{}
	service := NewService(NewRegistry(RegistryConfig{Now: func() time.Time { return now }}), picker, rtp, invite, media, ServiceConfig{})
	if _, err := service.Create(context.Background(), validCreate(now)); err == nil {
		t.Fatal("node error expected")
	}
	if invite.calls.Load() != 0 || rtp.openCalls.Load() != 0 {
		t.Fatalf("invite=%d open=%d", invite.calls.Load(), rtp.openCalls.Load())
	}

	picker.err = nil
	rtp.openErr = errors.New("open rtp failed")
	request := validCreate(now)
	request.RecordKey, request.IdempotencyKey = "record-2", "idem-record-2"
	if _, err := service.Create(context.Background(), request); err == nil {
		t.Fatal("rtp error expected")
	}
	if invite.calls.Load() != 0 {
		t.Fatalf("invite=%d", invite.calls.Load())
	}
}

func TestRandomPlaybackSSRCAlwaysUsesTenDecimalDigits(t *testing.T) {
	for range 1000 {
		assertValidPlaybackSSRC(t, randomPlaybackSSRC())
	}
}

func assertValidPlaybackSSRC(t *testing.T, ssrc string) {
	t.Helper()
	if len(ssrc) != 10 || ssrc[0] != '1' {
		t.Fatalf("SSRC = %q, want 10 digits starting with 1", ssrc)
	}
	for _, digit := range ssrc {
		if digit < '0' || digit > '9' {
			t.Fatalf("SSRC = %q, want decimal digits only", ssrc)
		}
	}
}

func TestPlaybackServiceInviteFailureCleansRTPAndBinding(t *testing.T) {
	now := time.Unix(1700000000, 0)
	rtp, invite := &fakeRTP{}, &fakeInvite{err: errors.New("486")}
	service := NewService(NewRegistry(RegistryConfig{Now: func() time.Time { return now }}),
		&fakeNodePicker{node: NodeInfo{ID: "node-1", ServerID: "34020000002000000001", Destination: "192.0.2.20:5060", RecvIP: "192.0.2.10"}}, rtp, invite, &fakeMedia{}, ServiceConfig{})
	if _, err := service.Create(context.Background(), validCreate(now)); err == nil {
		t.Fatal("invite error expected")
	}
	if rtp.closeCalls.Load() != 1 || rtp.unbindCalls.Load() != 1 {
		t.Fatalf("close=%d unbind=%d", rtp.closeCalls.Load(), rtp.unbindCalls.Load())
	}
}

func TestPlaybackServiceMediaWaitFailureCleansDialogRTPAndBinding(t *testing.T) {
	now := time.Unix(1700000000, 0)
	rtp, invite := &fakeRTP{}, &fakeInvite{}
	service := NewService(NewRegistry(RegistryConfig{Now: func() time.Time { return now }}),
		&fakeNodePicker{node: NodeInfo{ID: "node-1", ServerID: "34020000002000000001", Destination: "192.0.2.20:5060", RecvIP: "192.0.2.10"}}, rtp, invite, &fakeMedia{err: errors.New("media wait timeout")}, ServiceConfig{})
	if _, err := service.Create(context.Background(), validCreate(now)); err == nil {
		t.Fatal("media error expected")
	} else {
		var stageErr *ServiceError
		if !errors.As(err, &stageErr) || stageErr.Stage != "media_wait" || stageErr.Code != "timeout" {
			t.Fatalf("stage error=%+v err=%v", stageErr, err)
		}
	}
	if invite.teardown.Load() != 1 || rtp.closeCalls.Load() != 1 || rtp.unbindCalls.Load() != 1 {
		t.Fatalf("teardown=%d close=%d unbind=%d", invite.teardown.Load(), rtp.closeCalls.Load(), rtp.unbindCalls.Load())
	}
}

func TestPlaybackServiceStreamAndSSRCAreDistinctFromInputChannel(t *testing.T) {
	now := time.Unix(1700000000, 0)
	invite := &fakeInvite{}
	service := NewService(NewRegistry(RegistryConfig{Now: func() time.Time { return now }}),
		&fakeNodePicker{node: NodeInfo{ID: "node-1", ServerID: "34020000002000000001", Destination: "192.0.2.20:5060", RecvIP: "192.0.2.10"}}, &fakeRTP{}, invite, &fakeMedia{ready: MediaReady{}}, ServiceConfig{})
	result, err := service.Create(context.Background(), validCreate(now))
	if err != nil || result.Session.StreamID == result.Session.ChannelID || result.Session.SSRC == "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestPlaybackServiceActionUpdatesStateOnlyAfterDeviceAccepts(t *testing.T) {
	now := time.Unix(1700000000, 0)
	invite := &fakeInvite{}
	service := NewService(NewRegistry(RegistryConfig{Now: func() time.Time { return now }}),
		&fakeNodePicker{node: NodeInfo{ID: "node-1", ServerID: "34020000002000000001", Destination: "192.0.2.20:5060", RecvIP: "192.0.2.10"}}, &fakeRTP{}, invite, &fakeMedia{ready: MediaReady{}}, ServiceConfig{})
	created, err := service.Create(context.Background(), validCreate(now))
	if err != nil {
		t.Fatal(err)
	}
	paused, err := service.Action(context.Background(), created.Session.ID, "u1", ActionRequest{Action: "pause"})
	if err != nil || paused.State != StatePaused || invite.actionCalls.Load() != 1 {
		t.Fatalf("paused=%+v err=%v calls=%d", paused, err, invite.actionCalls.Load())
	}
	invite.actionErr = errors.New("device rejected")
	if _, err := service.Action(context.Background(), created.Session.ID, "u1", ActionRequest{Action: "resume"}); err == nil {
		t.Fatal("device rejection expected")
	}
	current := service.registry.MustGet(created.Session.ID)
	if current.State != StatePaused {
		t.Fatalf("state changed after rejected action: %s", current.State)
	}
}

func TestPlaybackServiceFileToEndFinalizesAndCleansExactlyOnce(t *testing.T) {
	now := time.Unix(1700000000, 0)
	metrics := &Metrics{}
	rtp := &fakeRTP{}
	invite := &fakeInvite{}
	service := NewService(NewRegistry(RegistryConfig{Now: func() time.Time { return now }}),
		&fakeNodePicker{node: NodeInfo{ID: "node-1", DeviceID: "device-1", ServerID: "34020000002000000001", Destination: "192.0.2.20:5060", RecvIP: "192.0.2.10"}}, rtp, invite, &fakeMedia{ready: MediaReady{}}, ServiceConfig{Metrics: metrics})
	request := validCreate(now)
	request.DeviceID = "device-1"
	created, err := service.Create(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.OnPlaybackFileToEnd(context.Background(), created.Session.CallID, "device-1", nil); err != nil {
		t.Fatal(err)
	}
	if err := service.OnPlaybackFileToEnd(context.Background(), created.Session.CallID, "device-1", nil); err != nil {
		t.Fatal(err)
	}
	session := service.registry.MustGet(created.Session.ID)
	if session.State != StateEnded || session.EndReason != "file-to-end" {
		t.Fatalf("session=%+v", session)
	}
	if invite.teardown.Load() != 1 || rtp.closeCalls.Load() != 1 || rtp.unbindCalls.Load() != 1 {
		t.Fatalf("cleanup teardown=%d close=%d unbind=%d", invite.teardown.Load(), rtp.closeCalls.Load(), rtp.unbindCalls.Load())
	}
	if err := service.Stop(context.Background(), created.Session.ID, "late-stop"); err != nil {
		t.Fatal(err)
	}
	if service.registry.MustGet(created.Session.ID).State != StateEnded {
		t.Fatal("late stop resurrected or changed natural terminal state")
	}
	snapshot := service.MetricsSnapshot()
	if snapshot.Created != 1 || snapshot.Ended != 1 || snapshot.Cleaned != 1 || snapshot.Active != 0 {
		t.Fatalf("metrics=%+v", snapshot)
	}
}

func TestPlaybackServiceReplaceWaitsForOldStopBeforeNewCreate(t *testing.T) {
	now := time.Unix(1700000000, 0)
	registry := NewRegistry(RegistryConfig{Now: func() time.Time { return now }})
	service := NewService(registry, &fakeNodePicker{node: NodeInfo{ID: "node-1", ServerID: "34020000002000000001", Destination: "192.0.2.20:5060", RecvIP: "192.0.2.10"}}, &fakeRTP{}, &fakeInvite{}, &fakeMedia{ready: MediaReady{}}, ServiceConfig{})
	first, err := service.Create(context.Background(), validCreate(now))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Stop(context.Background(), first.Session.ID, "replace"); err != nil {
		t.Fatal(err)
	}
	secondRequest := validCreate(now)
	secondRequest.RecordKey, secondRequest.IdempotencyKey = "record-2", "idem-record-2"
	second, err := service.Create(context.Background(), secondRequest)
	if err != nil || second.Session.ID == first.Session.ID {
		t.Fatalf("second=%+v err=%v", second, err)
	}
}

func TestPlaybackServiceConcurrentCreateAndStopIsRaceSafe(t *testing.T) {
	now := time.Unix(1700000000, 0)
	registry := NewRegistry(RegistryConfig{Now: func() time.Time { return now }})
	service := NewService(registry, &fakeNodePicker{node: NodeInfo{ID: "node-1", ServerID: "34020000002000000001", Destination: "192.0.2.20:5060", RecvIP: "192.0.2.10"}}, &fakeRTP{}, &fakeInvite{}, &fakeMedia{ready: MediaReady{}}, ServiceConfig{})
	result, err := service.Create(context.Background(), validCreate(now))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = service.Stop(context.Background(), result.Session.ID, "stop") }()
	}
	wg.Wait()
}

func TestPlaybackServiceConcurrentTerminalCountsExactlyOnce(t *testing.T) {
	now := time.Now()
	metrics := &Metrics{}
	rtp, invite := &fakeRTP{}, &fakeInvite{}
	service := NewService(NewRegistry(RegistryConfig{}),
		&fakeNodePicker{node: NodeInfo{ID: "node-1", DeviceID: "device-1", ServerID: "34020000002000000001", Destination: "192.0.2.20:5060", RecvIP: "192.0.2.10"}},
		rtp, invite, &fakeMedia{ready: MediaReady{}}, ServiceConfig{Metrics: metrics})
	request := validCreate(now)
	request.DeviceID = "device-1"
	created, err := service.Create(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, finalize := range []func() error{
		func() error {
			return service.OnPlaybackFileToEnd(context.Background(), created.Session.CallID, "device-1", nil)
		},
		func() error {
			return service.OnPlaybackMediaEnded(context.Background(), created.Session.CallID, "device-1", "bye")
		},
		func() error {
			return service.OnPlaybackStreamEnded(context.Background(), created.Session.StreamID, "offline")
		},
	} {
		wg.Add(1)
		go func(finalize func() error) {
			defer wg.Done()
			<-start
			_ = finalize()
		}(finalize)
	}
	close(start)
	wg.Wait()
	snapshot := service.MetricsSnapshot()
	if snapshot.Ended != 1 || snapshot.Cleaned != 1 || snapshot.Active != 0 {
		t.Fatalf("metrics=%+v", snapshot)
	}
	if invite.teardown.Load() != 1 || rtp.closeCalls.Load() != 1 || rtp.unbindCalls.Load() != 1 {
		t.Fatalf("cleanup teardown=%d close=%d unbind=%d", invite.teardown.Load(), rtp.closeCalls.Load(), rtp.unbindCalls.Load())
	}
}

func TestPlaybackServiceCloseCancelsMediaWaitAndCountsCleanupOnce(t *testing.T) {
	now := time.Now()
	metrics := &Metrics{}
	rtp, invite, media := &fakeRTP{}, &fakeInvite{}, &fakeMedia{wait: make(chan struct{})}
	service := NewService(NewRegistry(RegistryConfig{}),
		&fakeNodePicker{node: NodeInfo{ID: "node-1", ServerID: "34020000002000000001", Destination: "192.0.2.20:5060", RecvIP: "192.0.2.10"}},
		rtp, invite, media, ServiceConfig{Metrics: metrics, MediaWait: time.Minute})
	createDone := make(chan error, 1)
	go func() {
		_, err := service.Create(context.Background(), validCreate(now))
		createDone <- err
	}()
	deadline := time.After(time.Second)
	for media.calls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("media wait did not start")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if err := service.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-createDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("create err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("create did not stop with service close")
	}
	snapshot := service.MetricsSnapshot()
	if snapshot.Cleaned != 1 || snapshot.Failed != 0 || snapshot.Active != 0 {
		t.Fatalf("metrics=%+v", snapshot)
	}
	if invite.teardown.Load() != 1 || rtp.closeCalls.Load() != 1 || rtp.unbindCalls.Load() != 1 {
		t.Fatalf("cleanup teardown=%d close=%d unbind=%d", invite.teardown.Load(), rtp.closeCalls.Load(), rtp.unbindCalls.Load())
	}
}
