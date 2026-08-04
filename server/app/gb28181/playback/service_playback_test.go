package playback

import (
	"context"
	"errors"
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

func (f *fakeRTP) Open(context.Context, RTPRequest) (RTPAllocation, error) {
	f.openCalls.Add(1)
	if f.openErr != nil {
		return RTPAllocation{}, f.openErr
	}
	return RTPAllocation{StreamID: "pb-stream-1", SSRC: "1000000001", Port: 30000,
		Close:  func(context.Context) error { f.closeCalls.Add(1); return nil },
		Bind:   func() error { f.bindCalls.Add(1); f.bound = true; return nil },
		Unbind: func() error { f.unbindCalls.Add(1); f.bound = false; return nil }}, nil
}

type fakeInvite struct {
	err      error
	calls    atomic.Int32
	last     UACInvite
	teardown atomic.Int32
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

type fakeMedia struct {
	ready MediaReady
	err   error
	calls atomic.Int32
}

func (f *fakeMedia) Wait(context.Context, string) (MediaReady, error) {
	f.calls.Add(1)
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
	result, err := service.Create(context.Background(), validCreate(now))
	if err != nil || result.Session.State != StatePlaying || !result.Session.HasAudio || result.Session.MediaURLs["wsFlv"] == "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if invite.calls.Load() != 1 || rtp.openCalls.Load() != 1 || rtp.bindCalls.Load() != 1 || media.calls.Load() != 1 {
		t.Fatalf("calls invite=%d open=%d bind=%d media=%d", invite.calls.Load(), rtp.openCalls.Load(), rtp.bindCalls.Load(), media.calls.Load())
	}
	if invite.last.SDP == "" || invite.last.SDP[:1] != "v" {
		t.Fatalf("sdp=%q", invite.last.SDP)
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
