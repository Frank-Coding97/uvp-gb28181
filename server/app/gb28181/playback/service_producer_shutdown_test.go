package playback

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type heldPlaybackRTP struct {
	entered chan struct{}
	release chan struct{}
	closed  atomic.Int32
}

func (r *heldPlaybackRTP) Open(context.Context, RTPRequest) (RTPAllocation, error) {
	close(r.entered)
	<-r.release // A started external call need not obey cancellation.
	return RTPAllocation{Port: 30000, Close: func(context.Context) error {
		r.closed.Add(1)
		return nil
	}}, nil
}

func TestPlaybackServiceCloseRetainsCreatingResource(t *testing.T) {
	rtp := &heldPlaybackRTP{entered: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(rtp.release) }) }
	defer release()
	inviter := &fakeInvite{}
	registry := NewRegistry(RegistryConfig{})
	s := NewService(registry, &fakeNodePicker{node: NodeInfo{ID: "1", RecvIP: "192.0.2.1"}},
		rtp, inviter, &fakeMedia{}, ServiceConfig{})
	done := make(chan error, 1)
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		_, err := s.Create(context.Background(), validCreate(time.Now()))
		done <- err
	}()
	defer func() { release(); <-returned }()
	select {
	case <-rtp.entered:
	case <-time.After(time.Second):
		t.Fatal("Create did not enter RTP allocation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := s.Close(ctx)
	// Keep the original owner even after Close's caller gives up.
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, 1, registry.ActiveCount())
	require.Zero(t, rtp.closed.Load())
	_, err = s.Create(context.Background(), validCreate(time.Now()))
	require.ErrorIs(t, err, ErrRegistryClosed)
	release()
	select {
	case err = <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("Create did not return after RTP release")
	}
	require.NoError(t, s.Close(context.Background()))
	require.EqualValues(t, 1, rtp.closed.Load())
	require.Zero(t, inviter.calls.Load(), "cancellation must not start the next SIP stage")
	require.Zero(t, registry.ActiveCount())
}

type heldPlaybackStage struct {
	stage                                                       string
	entered                                                     chan struct{}
	release                                                     chan struct{}
	node, open, bind, invite, media, action, teardown, closeRTP atomic.Int32
}

func (h *heldPlaybackStage) hold(stage string) {
	if h.stage == stage {
		close(h.entered)
		<-h.release
	}
}
func (h *heldPlaybackStage) Pick(context.Context, PickRequest) (NodeInfo, error) {
	h.node.Add(1)
	h.hold("node")
	return NodeInfo{ID: "1", ServerID: "34020000002000000001", RecvIP: "192.0.2.1"}, nil
}
func (h *heldPlaybackStage) Open(context.Context, RTPRequest) (RTPAllocation, error) {
	h.open.Add(1)
	h.hold("open")
	return RTPAllocation{Port: 30000, Bind: func() error {
		h.bind.Add(1)
		h.hold("bind")
		return nil
	}, Close: func(context.Context) error { h.closeRTP.Add(1); return nil }}, nil
}
func (h *heldPlaybackStage) Invite(context.Context, UACInvite) (DialogInfo, error) {
	h.invite.Add(1)
	h.hold("invite")
	return DialogInfo{CallID: "held-call"}, nil
}
func (h *heldPlaybackStage) Teardown(context.Context, string) error {
	h.teardown.Add(1)
	return nil
}
func (h *heldPlaybackStage) Wait(context.Context, string) (MediaReady, error) {
	h.media.Add(1)
	h.hold("media")
	return MediaReady{}, nil
}
func (h *heldPlaybackStage) Action(context.Context, string, string, float64, float64, time.Duration) (float64, float64, error) {
	h.action.Add(1)
	h.hold("action")
	return 0, 1, nil
}

func TestPlaybackServiceCloseJoinsEveryProducerStage(t *testing.T) {
	for _, stage := range []string{"node", "open", "bind", "invite", "media", "observer", "action"} {
		t.Run(stage, func(t *testing.T) {
			h := &heldPlaybackStage{stage: stage, entered: make(chan struct{}), release: make(chan struct{})}
			var once sync.Once
			release := func() { once.Do(func() { close(h.release) }) }
			defer release()
			registry := NewRegistry(RegistryConfig{})
			s := NewService(registry, h, h, h, h, ServiceConfig{})
			s.SetMediaReadyObserver(func(Session) { h.hold("observer") })
			var sessionID string
			if stage == "action" {
				created, err := s.Create(context.Background(), validCreate(time.Now()))
				require.NoError(t, err)
				sessionID = created.Session.ID
			}
			done := make(chan struct{})
			result := make(chan error, 1)
			go func() {
				defer close(done)
				if stage == "action" {
					_, err := s.Action(context.Background(), sessionID, validCreate(time.Now()).OwnerID, ActionRequest{Action: "pause"})
					result <- err
				} else {
					_, err := s.Create(context.Background(), validCreate(time.Now()))
					result <- err
				}
			}()
			defer func() { release(); <-done }()
			select {
			case <-h.entered:
			case <-time.After(time.Second):
				t.Fatal("producer did not enter " + stage)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()
			require.ErrorIs(t, s.Close(ctx), context.DeadlineExceeded)
			require.Equal(t, 1, registry.ActiveCount())
			require.Zero(t, h.closeRTP.Load())
			require.Zero(t, h.teardown.Load())
			_, err := s.Action(context.Background(), sessionID, "1", ActionRequest{Action: "pause"})
			require.ErrorIs(t, err, ErrRegistryClosed)
			release()
			<-done
			require.ErrorIs(t, <-result, context.Canceled)
			require.NoError(t, s.Close(context.Background()))
			require.Zero(t, registry.ActiveCount())
			switch stage {
			case "node":
				require.Zero(t, h.open.Load())
			case "open":
				require.Zero(t, h.bind.Load())
			case "bind":
				require.Zero(t, h.invite.Load())
			case "invite":
				require.Zero(t, h.media.Load())
			}
			require.Equal(t, h.open.Load(), h.closeRTP.Load())
			require.Equal(t, h.invite.Load(), h.teardown.Load())
		})
	}
}

func TestPlaybackServiceConcurrentSweeperAndCloseAdmission(t *testing.T) {
	h := &heldPlaybackStage{}
	s := NewService(NewRegistry(RegistryConfig{}), h, h, h, h, ServiceConfig{})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); s.StartSweeper(context.Background(), time.Millisecond) }()
		go func() { defer wg.Done(); _ = s.Close(context.Background()) }()
		go func() { defer wg.Done(); _, _ = s.Create(context.Background(), validCreate(time.Now())) }()
	}
	wg.Wait()
	require.NoError(t, s.Close(context.Background()))
	s.producerMu.Lock()
	defer s.producerMu.Unlock()
	require.True(t, s.closing)
	require.Zero(t, s.producers)
	require.Equal(t, h.open.Load(), h.closeRTP.Load())
	require.Equal(t, h.invite.Load(), h.teardown.Load())
}

type heldPlaybackSweepCleanup struct {
	entered chan struct{}
	release chan struct{}
}

func (h *heldPlaybackSweepCleanup) Teardown(context.Context) error { return nil }
func (h *heldPlaybackSweepCleanup) CloseRTP(context.Context) error {
	close(h.entered)
	<-h.release
	return nil
}
func (h *heldPlaybackSweepCleanup) Unbind(context.Context) error { return nil }

func TestPlaybackServiceCloseJoinsBlockedSweeper(t *testing.T) {
	h := &heldPlaybackSweepCleanup{entered: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	release := func() { once.Do(func() { close(h.release) }) }
	defer release()
	now := time.Now().Add(-time.Hour)
	registry := NewRegistry(RegistryConfig{Now: func() time.Time { return now }})
	request := validCreate(now)
	request.Resources = h
	_, err := registry.Create(context.Background(), request)
	require.NoError(t, err)
	s := NewService(registry, nil, nil, nil, nil, ServiceConfig{})
	s.StartSweeper(context.Background(), time.Millisecond)
	defer func() { release(); _ = s.Close(context.Background()) }()
	select {
	case <-h.entered:
	case <-time.After(time.Second):
		t.Fatal("sweeper did not enter cleanup")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, s.Close(ctx), context.DeadlineExceeded)
	require.Equal(t, 1, registry.ActiveCount())
	release()
	require.NoError(t, s.Close(context.Background()))
	require.Zero(t, registry.ActiveCount())
}
