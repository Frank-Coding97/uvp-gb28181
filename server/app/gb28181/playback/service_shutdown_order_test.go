package playback

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Service.Close cancels lifecycle before Registry.CloseOnce. Force Create to
// observe that cancellation first: scheduler order must not create a failure.
func TestPlaybackServiceShutdownCancellationBeforeRegistryClose(t *testing.T) {
	metrics := &Metrics{}
	media := &fakeMedia{wait: make(chan struct{})}
	rtp, invite := &fakeRTP{}, &fakeInvite{}
	s := NewService(NewRegistry(RegistryConfig{}),
		&fakeNodePicker{node: NodeInfo{ID: "node-1", ServerID: "34020000002000000001", Destination: "192.0.2.20:5060", RecvIP: "192.0.2.10"}},
		rtp, invite, media, ServiceConfig{Metrics: metrics, MediaWait: time.Minute})
	result, returned := make(chan error, 1), make(chan struct{})
	go func() {
		_, err := s.Create(context.Background(), validCreate(time.Now()))
		result <- err
		close(returned)
	}()
	require.Eventually(t, func() bool { return media.calls.Load() != 0 }, time.Second, time.Millisecond)
	stop := s.lifecycleStop
	s.lifecycleStop = func() {
		stop()
		select {
		case <-returned:
		case <-time.After(time.Second):
			t.Fatal("Create did not observe lifecycle cancellation")
		}
	}
	require.NoError(t, s.Close(context.Background()))
	require.True(t, errors.Is(<-result, context.Canceled))
	require.EqualValues(t, 0, metrics.Failed.Load())
	require.EqualValues(t, 1, metrics.Cleaned.Load())
	require.EqualValues(t, 0, s.MetricsSnapshot().Active)
	require.EqualValues(t, 1, invite.teardown.Load())
	require.EqualValues(t, 1, rtp.closeCalls.Load())
	require.EqualValues(t, 1, rtp.unbindCalls.Load())
}

func TestPlaybackServiceShutdownDoesNotHideIndependentFailure(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		metrics := &Metrics{}
		registry := NewRegistry(RegistryConfig{})
		s := NewService(registry, nil, nil, nil, nil, ServiceConfig{Metrics: metrics})
		session, err := registry.Create(context.Background(), validCreate(time.Now()))
		require.NoError(t, err)
		cause := context.Canceled
		if shutdown {
			s.lifecycleStop()
			cause = errors.New("independent media failure")
		}
		err = s.fail(context.Background(), session.Session.ID, "media_wait", "failed", cause, nil)
		require.ErrorIs(t, err, cause)
		require.EqualValues(t, 1, metrics.Failed.Load())
		require.EqualValues(t, 1, metrics.Cleaned.Load())
		stored, ok := registry.Get(session.Session.ID)
		require.True(t, ok)
		require.Equal(t, StateFailed, stored.State)
		require.NoError(t, s.Close(context.Background()))
	}
}
