package playback

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPlaybackPolicyLockRejectsCreateAndActionButAllowsStop(t *testing.T) {
	var locked atomic.Bool
	h := &heldPlaybackStage{}
	s := NewService(NewRegistry(RegistryConfig{}), h, h, h, h, ServiceConfig{RequireIntents: locked.Load})
	req := validCreate(time.Now())
	created, err := s.Create(context.Background(), req)
	require.NoError(t, err)
	locked.Store(true)
	_, err = s.Create(context.Background(), req)
	require.ErrorIs(t, err, ErrRTPUnavailable, "even an idempotent legacy reuse is denied")
	_, err = s.Action(context.Background(), created.Session.ID, req.OwnerID, ActionRequest{Action: "pause"})
	require.ErrorIs(t, err, ErrRTPUnavailable)
	require.EqualValues(t, 1, h.open.Load())
	require.EqualValues(t, 1, h.invite.Load())
	require.Zero(t, h.action.Load())
	require.NoError(t, s.Stop(context.Background(), created.Session.ID, "policy reload"))
	require.NoError(t, s.Close(context.Background()))
	require.EqualValues(t, 1, h.closeRTP.Load())
	require.EqualValues(t, 1, h.teardown.Load())
}

func TestPlaybackPolicyLockStillJoinsPreviouslyAdmittedAction(t *testing.T) {
	var locked atomic.Bool
	h := &heldPlaybackStage{stage: "action", entered: make(chan struct{}), release: make(chan struct{})}
	s := NewService(NewRegistry(RegistryConfig{}), h, h, h, h, ServiceConfig{RequireIntents: locked.Load})
	req := validCreate(time.Now())
	created, err := s.Create(context.Background(), req)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, err := s.Action(context.Background(), created.Session.ID, req.OwnerID, ActionRequest{Action: "pause"})
		done <- err
	}()
	defer func() { close(h.release); <-done; require.NoError(t, s.Close(context.Background())) }()
	select {
	case <-h.entered:
	case <-time.After(time.Second):
		t.Fatal("action not admitted")
	}
	locked.Store(true)
	_, err = s.Action(context.Background(), created.Session.ID, req.OwnerID, ActionRequest{Action: "pause"})
	require.ErrorIs(t, err, ErrRTPUnavailable)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, s.Close(ctx), context.DeadlineExceeded)
	require.EqualValues(t, 1, h.action.Load())
	require.Zero(t, h.closeRTP.Load(), "old resources must remain until the actual producer returns")
}
