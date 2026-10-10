package playback

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAPIPlaybackCleanupFailureRetainsOwnershipUntilRetry(t *testing.T) {
	now := time.Unix(1700000000, 0)
	resources := &fakeCleanupResources{closeErr: errors.New("isolated RTP close failure")}
	r := NewRegistry(RegistryConfig{Now: func() time.Time { return now }})
	request := playbackRequest(now, "owner", "channel", "record")
	request.DeviceID = "34020000002000100021"
	request.Resources = resources
	created, err := r.Create(context.Background(), request)
	require.NoError(t, err)
	other, err := r.Create(context.Background(), playbackRequest(now, "other", "other-channel", "other-record"))
	require.NoError(t, err)

	require.Error(t, r.Stop(context.Background(), created.Session.ID, "device transferred"))
	require.Equal(t, StateStopping, r.MustGet(created.Session.ID).State, "failed media cleanup is not a terminal session")
	require.EqualValues(t, 0, resources.unbind.Load(), "retain the media binding for a safe retry")
	require.Equal(t, 2, r.ActiveCount(), "failed cleanup must not release the ownership scope")
	require.Equal(t, StateCreating, r.MustGet(other.Session.ID).State)
	r.pruneTerminal(now.Add(time.Hour))
	_, exists := r.Get(created.Session.ID)
	require.True(t, exists, "pending cleanup cannot age out as a terminal session")

	resources.closeErr = nil
	require.NoError(t, r.Stop(context.Background(), created.Session.ID, "device transferred retry"))
	require.Equal(t, StateStopped, r.MustGet(created.Session.ID).State)
	require.EqualValues(t, 2, resources.close.Load())
	require.EqualValues(t, 1, resources.unbind.Load())
	require.Equal(t, 1, r.ActiveCount())
	require.NoError(t, r.Stop(context.Background(), created.Session.ID, "duplicate"))
	require.EqualValues(t, 2, resources.close.Load())
}

func TestOpenAPIPlaybackCleanupRetainsFirstTerminalIntentAndCloseRetries(t *testing.T) {
	for _, terminal := range []State{StateEnded, StateFailed} {
		t.Run(string(terminal), func(t *testing.T) {
			now := time.Unix(1700000000, 0)
			resources := &fakeCleanupResources{closeErr: errors.New("close unavailable")}
			r := NewRegistry(RegistryConfig{Now: func() time.Time { return now }})
			request := playbackRequest(now, "owner", "channel", "record")
			request.Resources = resources
			created, err := r.Create(context.Background(), request)
			require.NoError(t, err)
			settled, err := r.FinalizeContextOnce(context.Background(), created.Session.ID, terminal, "original intent")
			require.Error(t, err)
			require.False(t, settled)
			count, err := r.CloseOnce(context.Background())
			require.Error(t, err)
			require.Zero(t, count)
			require.Equal(t, 1, r.Size())
			require.Equal(t, 1, r.ActiveCount())
			_, err = r.Create(context.Background(), request)
			require.ErrorIs(t, err, ErrRegistryClosed)
			resources.closeErr = nil
			require.NoError(t, r.Stop(context.Background(), created.Session.ID, "retry via ordinary stop"))
			require.Equal(t, terminal, r.MustGet(created.Session.ID).State)
			require.Equal(t, "original intent", r.MustGet(created.Session.ID).EndReason)
			count, err = r.CloseOnce(context.Background())
			require.NoError(t, err)
			require.Zero(t, count, "already terminal session is not counted twice")
			require.Zero(t, r.Size())
		})
	}
}

func TestOpenAPIPlaybackCloseRetriesPendingAndCountsOnlySettled(t *testing.T) {
	now := time.Unix(1700000000, 0)
	resources := &fakeCleanupResources{closeErr: errors.New("close unavailable")}
	r := NewRegistry(RegistryConfig{Now: func() time.Time { return now }})
	request := playbackRequest(now, "owner", "channel", "record")
	request.Resources = resources
	_, err := r.Create(context.Background(), request)
	require.NoError(t, err)
	count, err := r.CloseOnce(context.Background())
	require.Error(t, err)
	require.Zero(t, count)
	require.Equal(t, 1, r.Size())
	resources.closeErr = nil
	count, err = r.CloseOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Zero(t, r.Size())
	count, err = r.CloseOnce(context.Background())
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestOpenAPIPlaybackCleanedMetricDoesNotCountFailedAttempt(t *testing.T) {
	now := time.Unix(1700000000, 0)
	resources := &fakeCleanupResources{closeErr: errors.New("close unavailable")}
	r := NewRegistry(RegistryConfig{Now: func() time.Time { return now }})
	request := playbackRequest(now, "owner", "channel", "record")
	request.Resources = resources
	created, err := r.Create(context.Background(), request)
	require.NoError(t, err)
	service := &Service{registry: r, config: ServiceConfig{Metrics: &Metrics{}}}
	require.Error(t, service.Stop(context.Background(), created.Session.ID, "stop"))
	require.Zero(t, service.MetricsSnapshot().Cleaned)
	resources.closeErr = nil
	require.NoError(t, service.Stop(context.Background(), created.Session.ID, "retry"))
	require.EqualValues(t, 1, service.MetricsSnapshot().Cleaned)
	require.NoError(t, service.Stop(context.Background(), created.Session.ID, "duplicate"))
	require.EqualValues(t, 1, service.MetricsSnapshot().Cleaned)
}

type pendingCleanupResources struct {
	teardownErr, closeErr, unbindErr error
	unbound                          int
}

func (r *pendingCleanupResources) Teardown(context.Context) error { return r.teardownErr }
func (r *pendingCleanupResources) CloseRTP(context.Context) error { return r.closeErr }
func (r *pendingCleanupResources) Unbind(context.Context) error {
	r.unbound++
	return r.unbindErr
}

func TestOpenAPIPlaybackEveryCleanupFailureRemainsRetryable(t *testing.T) {
	for _, stage := range []string{"teardown", "close", "unbind"} {
		t.Run(stage, func(t *testing.T) {
			resources := &pendingCleanupResources{}
			failure := errors.New("isolated cleanup failure")
			switch stage {
			case "teardown":
				resources.teardownErr = failure
			case "close":
				resources.closeErr = failure
			case "unbind":
				resources.unbindErr = failure
			}
			now := time.Unix(1700000000, 0)
			r := NewRegistry(RegistryConfig{Now: func() time.Time { return now }})
			request := playbackRequest(now, "owner", "channel", "record")
			request.Resources = resources
			created, err := r.Create(context.Background(), request)
			require.NoError(t, err)
			settled, err := r.StopOnce(context.Background(), created.Session.ID, "transfer")
			require.ErrorIs(t, err, failure)
			require.False(t, settled)
			require.Equal(t, StateStopping, r.MustGet(created.Session.ID).State)
			if stage != "unbind" {
				require.Zero(t, resources.unbound)
			}
			resources.teardownErr, resources.closeErr, resources.unbindErr = nil, nil, nil
			count, err := r.SweepOnce(context.Background(), now.Add(time.Hour))
			require.NoError(t, err)
			require.Equal(t, 1, count)
			require.Zero(t, r.ActiveCount())
		})
	}
}

type blockedRetryCleanupResources struct {
	entered  chan struct{}
	release  chan struct{}
	once     sync.Once
	calls    atomic.Int32
	firstErr error
}

func (r *blockedRetryCleanupResources) Teardown(context.Context) error { return nil }
func (r *blockedRetryCleanupResources) Unbind(context.Context) error   { return nil }
func (r *blockedRetryCleanupResources) CloseRTP(ctx context.Context) error {
	if r.calls.Add(1) > 1 {
		return nil
	}
	r.once.Do(func() { close(r.entered) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.release:
		return r.firstErr
	}
}

func TestOpenAPIPlaybackCleanupAttemptResultSurvivesSuccessfulRetry(t *testing.T) {
	resources := &blockedRetryCleanupResources{entered: make(chan struct{}), release: make(chan struct{}), firstErr: errors.New("first close failure")}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(resources.release) }) }
	defer release()
	now := time.Unix(1700000000, 0)
	r := NewRegistry(RegistryConfig{Now: func() time.Time { return now }})
	request := playbackRequest(now, "owner", "channel", "record")
	request.Resources = resources
	created, err := r.Create(context.Background(), request)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ownerDone := make(chan error, 1)
	go func() { ownerDone <- r.Stop(ctx, created.Session.ID, "original") }()
	select {
	case <-resources.entered:
	case <-ctx.Done():
		t.Fatal("cleanup did not start")
	}
	record, ok := r.record(created.Session.ID)
	require.True(t, ok)
	record.mu.Lock()
	firstCall := record.stopCall
	record.mu.Unlock()
	require.NotNil(t, firstCall)
	waiterCtx, waiterCancel := context.WithCancel(ctx)
	waiterCancel()
	require.ErrorIs(t, r.Stop(waiterCtx, created.Session.ID, "canceled waiter"), context.Canceled)
	require.EqualValues(t, 1, resources.calls.Load())
	release()
	select {
	case err = <-ownerDone:
	case <-ctx.Done():
		t.Fatal("cleanup did not finish")
	}
	require.ErrorIs(t, err, resources.firstErr)
	require.NoError(t, r.Stop(ctx, created.Session.ID, "successful retry"))
	<-firstCall.done
	require.ErrorIs(t, firstCall.err, resources.firstErr, "retry cannot rewrite an earlier waiter's result")
	require.EqualValues(t, 2, resources.calls.Load())
}
