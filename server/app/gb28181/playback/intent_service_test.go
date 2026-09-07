package playback

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

type parentServiceRTP struct {
	parentTestRTP
	entered, release chan struct{}
}

func (c *parentServiceRTP) Open(context.Context) (RTPAllocation, error) {
	close(c.entered)
	<-c.release
	return RTPAllocation{Port: 30000}, nil
}

type parentServiceRTPFactory struct {
	RTPOpener
	child   *parentServiceRTP
	t       *testing.T
	barrier *playauth.DeviceOperationBarrier
}

func (f *parentServiceRTPFactory) PrepareIntent(ctx context.Context, store *playauth.DeviceOperationIntentStore, id playauth.DeviceOperationIntentIdentity, version int64, _ NodeInfo, _ RTPRequest) (IntentRTPChild, error) {
	loaded, err := store.LoadRTPResourceSteps(ctx, id)
	require.NoError(f.t, err)
	require.Equal(f.t, playauth.IntentDispatched, loaded.Intent.State)
	require.Equal(f.t, loaded.Intent.RowVersion, version)
	wait, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(f.t, f.barrier.WaitBefore(wait, uint(id.DevicePK), id.DeviceEpoch+1), context.DeadlineExceeded)
	return f.child, nil
}

type parentServiceSIPFactory struct{ PlaybackInviter }

func (f *parentServiceSIPFactory) PrepareIntent(context.Context, *playauth.DeviceOperationIntentStore, *playauth.DeviceOperationBarrier, playauth.DeviceOperationLease, playauth.DeviceOperationIntentIdentity, int64, string, UACInvite) (IntentSIPChild, error) {
	return &parentTestSIP{local: true}, nil
}

func TestPlaybackIntentServiceStopJoinsPartiallyCreatedResource(t *testing.T) {
	db, barrier, req := playbackEpochFixture(t)
	require.NoError(t, db.Exec("CREATE TABLE gb_channel (id INTEGER PRIMARY KEY, device_id TEXT, channel_id TEXT, deleted_at DATETIME)").Error)
	require.NoError(t, db.Exec("INSERT INTO gb_channel VALUES(2,?,?,NULL)", req.DeviceID, req.SIPChannelID).Error)
	require.NoError(t, db.Exec("ALTER TABLE gb_device_operation_intent ADD COLUMN rtp_steps_json TEXT NULL").Error)
	h := &heldPlaybackStage{}
	child := &parentServiceRTP{parentTestRTP: parentTestRTP{local: true}, entered: make(chan struct{}), release: make(chan struct{})}
	rtp := &parentServiceRTPFactory{RTPOpener: h, child: child, t: t, barrier: barrier}
	s := NewService(NewRegistry(RegistryConfig{}), h, rtp, &parentServiceSIPFactory{PlaybackInviter: h}, h,
		ServiceConfig{DeviceOperations: barrier, Intents: playauth.NewDeviceOperationIntentStore(db)})
	done := make(chan error, 1)
	go func() { _, err := s.Create(context.Background(), req); done <- err }()
	select {
	case <-child.entered:
	case <-time.After(time.Second):
		t.Fatal("actual Service never reached persistent RTP child")
	}
	s.registry.mu.RLock()
	var sessionID string
	for id := range s.registry.sessions {
		sessionID = id
	}
	s.registry.mu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, s.Stop(ctx, sessionID, "fixture concurrent stop"), context.DeadlineExceeded)
	session, ok := s.registry.Get(sessionID)
	require.True(t, ok)
	require.False(t, session.State.IsTerminal())
	wait, stop := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer stop()
	require.ErrorIs(t, barrier.WaitBefore(wait, 1, 2), context.DeadlineExceeded)
	close(child.release)
	require.Error(t, <-done)
	require.NoError(t, s.Stop(context.Background(), sessionID, "fixture retry"))
	require.NoError(t, barrier.WaitBefore(context.Background(), 1, 2))
	require.Zero(t, h.open.Load(), "persistent path must never call legacy RTP")
	require.Zero(t, h.invite.Load(), "cancelled initialization must never reach legacy SIP")
	require.NoError(t, s.Close(context.Background()))
}

func TestPlaybackIntentServiceLifecycleCancellationIsStoppedAndCountedOnce(t *testing.T) {
	db, barrier, req := playbackEpochFixture(t)
	require.NoError(t, db.Exec("CREATE TABLE gb_channel (id INTEGER PRIMARY KEY, device_id TEXT, channel_id TEXT, deleted_at DATETIME)").Error)
	require.NoError(t, db.Exec("INSERT INTO gb_channel VALUES(2,?,?,NULL)", req.DeviceID, req.SIPChannelID).Error)
	require.NoError(t, db.Exec("ALTER TABLE gb_device_operation_intent ADD COLUMN rtp_steps_json TEXT NULL").Error)
	h := &heldPlaybackStage{}
	child := &parentServiceRTP{parentTestRTP: parentTestRTP{local: true}, entered: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(child.release) }) }
	defer release()
	metrics := &Metrics{}
	s := NewService(NewRegistry(RegistryConfig{}), h, &parentServiceRTPFactory{RTPOpener: h, child: child, t: t, barrier: barrier},
		&parentServiceSIPFactory{PlaybackInviter: h}, h,
		ServiceConfig{DeviceOperations: barrier, Intents: playauth.NewDeviceOperationIntentStore(db), Metrics: metrics})
	done := make(chan error, 1)
	go func() { _, err := s.Create(context.Background(), req); done <- err }()
	select {
	case <-child.entered:
	case <-time.After(time.Second):
		t.Fatal("persistent RTP did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, s.Close(ctx), context.DeadlineExceeded)
	require.Zero(t, metrics.Cleaned.Load(), "cancellation did not join the actual call")
	release()
	require.ErrorIs(t, <-done, context.Canceled)
	// The deferred finalizer and cancellation watcher race to own cleanup.
	// Both must preserve the same terminal meaning and exactly one metric.
	s.registry.mu.RLock()
	var id string
	for key := range s.registry.sessions {
		id = key
	}
	s.registry.mu.RUnlock()
	session, ok := s.registry.Get(id)
	require.True(t, ok)
	require.Equal(t, StateStopped, session.State)
	require.NoError(t, s.Close(context.Background()))
	require.EqualValues(t, 1, metrics.Created.Load())
	require.Zero(t, metrics.Failed.Load())
	require.EqualValues(t, 1, metrics.Cleaned.Load())
}
