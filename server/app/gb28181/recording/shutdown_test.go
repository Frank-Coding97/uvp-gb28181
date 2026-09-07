package recording

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type shutdownRecorderClient struct {
	mu                 sync.Mutex
	recording          bool
	keepRecording      bool
	stopErr            error
	isRecordingErr     error
	isRecordingEntered chan struct{}
	isRecordingRelease chan struct{}
	enteredOnce        sync.Once
	stopCalls          atomic.Int32
	isRecordingCalls   atomic.Int32
}

func (c *shutdownRecorderClient) IsRecording(ctx context.Context, _, _, _ string) (bool, error) {
	c.isRecordingCalls.Add(1)
	if c.isRecordingEntered != nil {
		c.enteredOnce.Do(func() { close(c.isRecordingEntered) })
	}
	if c.isRecordingRelease != nil {
		select {
		case <-c.isRecordingRelease:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	if c.isRecordingErr != nil {
		return false, c.isRecordingErr
	}
	c.mu.Lock()
	recording := c.recording
	c.mu.Unlock()
	return recording, nil
}

func (c *shutdownRecorderClient) StartRecord(context.Context, string, string, string, int) error {
	c.mu.Lock()
	c.recording = true
	c.mu.Unlock()
	return nil
}

func (c *shutdownRecorderClient) StopRecord(ctx context.Context, _, _, _ string) error {
	c.stopCalls.Add(1)
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.stopErr != nil {
		return c.stopErr
	}
	c.mu.Lock()
	if !c.keepRecording {
		c.recording = false
	}
	c.mu.Unlock()
	return nil
}

type shutdownTestRepo struct {
	*GormRepo
	markSessionStoppedErr error
	markStateUpdated      *bool
}

func (r *shutdownTestRepo) MarkSessionStopped(ctx context.Context, sessionID uint64, message string) error {
	if r.markSessionStoppedErr != nil {
		return r.markSessionStoppedErr
	}
	return r.GormRepo.MarkSessionStopped(ctx, sessionID, message)
}

func (r *shutdownTestRepo) MarkState(ctx context.Context, channelID uint, expectedEnabled bool, state, message string) (bool, error) {
	if r.markStateUpdated != nil {
		return *r.markStateUpdated, nil
	}
	return r.GormRepo.MarkState(ctx, channelID, expectedEnabled, state, message)
}

func newShutdownFixture(t *testing.T) (*GormRepo, *models.GbChannel, *models.GbRecordingSession) {
	t.Helper()
	db, _ := newRecordingSQLiteBaselineDB(t)
	repo := NewGormRepo(db)
	channel := &models.GbChannel{
		DeviceID: "shutdown-device", ChannelID: "shutdown-channel", StreamID: "shutdown-stream",
		Status: models.ChannelStatusOnline, OnDemandLive: true,
	}
	require.NoError(t, db.Create(channel).Error)
	_, err := repo.SetDesired(context.Background(), channel.ID, true)
	require.NoError(t, err)
	session := &models.GbRecordingSession{
		ChannelID: channel.ID, DeviceID: channel.DeviceID, NodeID: 2,
		VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp,
		Stream: channel.StreamID, State: models.RecordingSessionStateRecording,
	}
	require.NoError(t, repo.UpsertSession(context.Background(), session))
	_, err = repo.MarkState(context.Background(), channel.ID, true, models.CloudRecordingStateRecording, "")
	require.NoError(t, err)
	return repo, channel, session
}

func newShutdownService(repo Repository, client RecorderClient) *Service {
	return NewService(repo, fakeLocation{nodeID: 2, ok: true}, fakeRegistry{item: &node.Node{ID: 2}}, func(*node.Node) RecorderClient {
		return client
	})
}

func TestShutdownStopsSessionsAndPreservesEnabledIntent(t *testing.T) {
	repo, channel, session := newShutdownFixture(t)
	client := &shutdownRecorderClient{recording: true}
	service := newShutdownService(repo, client)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, service.Shutdown(ctx))
	require.EqualValues(t, 1, client.stopCalls.Load())

	storedSession, err := repo.FindSessionByID(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, models.RecordingSessionStateStopped, storedSession.State)
	storedChannel, err := repo.GetChannel(ctx, channel.ID)
	require.NoError(t, err)
	require.True(t, storedChannel.CloudRecordingEnabled)
	require.Equal(t, models.CloudRecordingStateWaiting, storedChannel.CloudRecordingState)

	require.NoError(t, service.Shutdown(ctx))
	require.EqualValues(t, 1, client.stopCalls.Load(), "repeated shutdown must not stop the same session again")
}

func TestShutdownStopsEveryUnfinishedSession(t *testing.T) {
	repo, firstChannel, firstSession := newShutdownFixture(t)
	secondChannel := &models.GbChannel{
		DeviceID: "shutdown-device-2", ChannelID: "shutdown-channel-2", StreamID: "shutdown-stream-2",
		Status: models.ChannelStatusOnline, OnDemandLive: true,
	}
	require.NoError(t, repo.db.Create(secondChannel).Error)
	_, err := repo.SetDesired(context.Background(), secondChannel.ID, true)
	require.NoError(t, err)
	secondSession := &models.GbRecordingSession{
		ChannelID: secondChannel.ID, DeviceID: secondChannel.DeviceID, NodeID: 2,
		VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp,
		Stream: secondChannel.StreamID, State: models.RecordingSessionStateRecording,
	}
	require.NoError(t, repo.UpsertSession(context.Background(), secondSession))
	_, err = repo.MarkState(context.Background(), secondChannel.ID, true, models.CloudRecordingStateRecording, "")
	require.NoError(t, err)

	client := &shutdownRecorderClient{recording: true}
	service := newShutdownService(repo, client)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, service.Shutdown(ctx))
	require.EqualValues(t, 2, client.stopCalls.Load())

	for _, sessionID := range []uint64{firstSession.ID, secondSession.ID} {
		stored, findErr := repo.FindSessionByID(ctx, sessionID)
		require.NoError(t, findErr)
		require.Equal(t, models.RecordingSessionStateStopped, stored.State)
	}
	for _, channelID := range []uint{firstChannel.ID, secondChannel.ID} {
		stored, getErr := repo.GetChannel(ctx, channelID)
		require.NoError(t, getErr)
		require.True(t, stored.CloudRecordingEnabled)
		require.Equal(t, models.CloudRecordingStateWaiting, stored.CloudRecordingState)
	}
}

func TestShutdownDisablesAlreadyDisabledIntent(t *testing.T) {
	repo, channel, session := newShutdownFixture(t)
	_, err := repo.SetDesired(context.Background(), channel.ID, false)
	require.NoError(t, err)
	client := &shutdownRecorderClient{recording: true}
	service := newShutdownService(repo, client)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, service.Shutdown(ctx))
	storedSession, err := repo.FindSessionByID(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, models.RecordingSessionStateStopped, storedSession.State)
	storedChannel, err := repo.GetChannel(ctx, channel.ID)
	require.NoError(t, err)
	require.False(t, storedChannel.CloudRecordingEnabled)
	require.Equal(t, models.CloudRecordingStateDisabled, storedChannel.CloudRecordingState)
}

func TestShutdownReturnsStopErrorAndLeavesFailedRecoveryState(t *testing.T) {
	repo, channel, session := newShutdownFixture(t)
	stopErr := errors.New("stop recorder failed")
	client := &shutdownRecorderClient{recording: true, stopErr: stopErr}
	service := newShutdownService(repo, client)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := service.Shutdown(ctx)
	require.ErrorIs(t, err, stopErr)
	storedSession, findErr := repo.FindSessionByID(ctx, session.ID)
	require.NoError(t, findErr)
	require.Equal(t, models.RecordingSessionStateFailed, storedSession.State)
	storedChannel, findErr := repo.GetChannel(ctx, channel.ID)
	require.NoError(t, findErr)
	require.True(t, storedChannel.CloudRecordingEnabled)
	require.Equal(t, models.CloudRecordingStateFailed, storedChannel.CloudRecordingState)
}

func TestShutdownRejectsStillRecordingAndDoesNotMarkStopped(t *testing.T) {
	repo, channel, session := newShutdownFixture(t)
	client := &shutdownRecorderClient{recording: true, keepRecording: true}
	service := newShutdownService(repo, client)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := service.Shutdown(ctx)
	require.Error(t, err)
	storedSession, findErr := repo.FindSessionByID(ctx, session.ID)
	require.NoError(t, findErr)
	require.Equal(t, models.RecordingSessionStateFailed, storedSession.State)
	require.NotEqual(t, models.RecordingSessionStateStopped, storedSession.State)
	storedChannel, findErr := repo.GetChannel(ctx, channel.ID)
	require.NoError(t, findErr)
	require.True(t, storedChannel.CloudRecordingEnabled)
	require.Equal(t, models.CloudRecordingStateFailed, storedChannel.CloudRecordingState)
}

func TestShutdownReturnsIsRecordingDeadlineAndLeavesSessionUnstopped(t *testing.T) {
	repo, channel, session := newShutdownFixture(t)
	client := &shutdownRecorderClient{recording: true, isRecordingRelease: make(chan struct{})}
	service := newShutdownService(repo, client)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := service.Shutdown(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(start), time.Second)
	require.EqualValues(t, 1, client.stopCalls.Load())
	storedSession, findErr := repo.FindSessionByID(context.Background(), session.ID)
	require.NoError(t, findErr)
	require.NotEqual(t, models.RecordingSessionStateStopped, storedSession.State)
	storedChannel, findErr := repo.GetChannel(context.Background(), channel.ID)
	require.NoError(t, findErr)
	require.Equal(t, models.CloudRecordingStateRecording, storedChannel.CloudRecordingState)
}

func TestShutdownReturnsDatabaseStopMarkError(t *testing.T) {
	baseRepo, channel, session := newShutdownFixture(t)
	markErr := errors.New("mark stopped failed")
	repo := &shutdownTestRepo{GormRepo: baseRepo, markSessionStoppedErr: markErr}
	client := &shutdownRecorderClient{recording: true}
	service := newShutdownService(repo, client)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := service.Shutdown(ctx)
	require.ErrorIs(t, err, markErr)
	storedSession, findErr := baseRepo.FindSessionByID(ctx, session.ID)
	require.NoError(t, findErr)
	require.NotEqual(t, models.RecordingSessionStateStopped, storedSession.State)
	storedChannel, findErr := baseRepo.GetChannel(ctx, channel.ID)
	require.NoError(t, findErr)
	require.Equal(t, models.CloudRecordingStateRecording, storedChannel.CloudRecordingState)
}

func TestShutdownReturnsChannelStateNoopAfterSessionStop(t *testing.T) {
	baseRepo, channel, session := newShutdownFixture(t)
	updated := false
	repo := &shutdownTestRepo{GormRepo: baseRepo, markStateUpdated: &updated}
	service := newShutdownService(repo, &shutdownRecorderClient{recording: true})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := service.Shutdown(ctx)
	require.ErrorContains(t, err, "录像通道状态未更新")
	storedSession, findErr := baseRepo.FindSessionByID(ctx, session.ID)
	require.NoError(t, findErr)
	require.Equal(t, models.RecordingSessionStateStopped, storedSession.State)
	storedChannel, findErr := baseRepo.GetChannel(ctx, channel.ID)
	require.NoError(t, findErr)
	require.Equal(t, models.CloudRecordingStateRecording, storedChannel.CloudRecordingState)
}

func TestShutdownDeadlineLeavesAcceptedBeginAndRejectsNewStarts(t *testing.T) {
	repo, channel, session := newShutdownFixture(t)
	client := &shutdownRecorderClient{
		recording:          false,
		isRecordingEntered: make(chan struct{}),
		isRecordingRelease: make(chan struct{}),
	}
	service := newShutdownService(repo, client)

	beginDone := make(chan error, 1)
	go func() { beginDone <- service.BeginPlayback(context.Background(), channel.StreamID) }()
	select {
	case <-client.isRecordingEntered:
	case <-time.After(time.Second):
		t.Fatal("BeginPlayback did not reach recorder")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := service.Shutdown(shutdownCtx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(start), time.Second)
	_, enableErr := service.Enable(context.Background(), channel.ID)
	require.ErrorIs(t, enableErr, ErrServiceShuttingDown)
	require.Zero(t, client.stopCalls.Load())

	close(client.isRecordingRelease)
	require.NoError(t, <-beginDone)
	storedSession, findErr := repo.FindSessionByID(context.Background(), session.ID)
	require.NoError(t, findErr)
	require.Equal(t, models.RecordingSessionStateRecording, storedSession.State)
}

func TestShutdownWaitsForAcceptedBeginAndStopsIt(t *testing.T) {
	repo, channel, session := newShutdownFixture(t)
	client := &shutdownRecorderClient{
		recording:          false,
		isRecordingEntered: make(chan struct{}),
		isRecordingRelease: make(chan struct{}),
	}
	service := newShutdownService(repo, client)

	beginDone := make(chan error, 1)
	go func() { beginDone <- service.BeginPlayback(context.Background(), channel.StreamID) }()
	select {
	case <-client.isRecordingEntered:
	case <-time.After(time.Second):
		t.Fatal("BeginPlayback did not reach recorder")
	}

	shutdownDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		shutdownDone <- service.Shutdown(ctx)
	}()
	require.Eventually(t, func() bool {
		_, enableErr := service.Enable(context.Background(), 999999)
		return errors.Is(enableErr, ErrServiceShuttingDown)
	}, time.Second, time.Millisecond)
	require.ErrorIs(t, service.BeginPlayback(context.Background(), channel.StreamID), ErrServiceShuttingDown)

	close(client.isRecordingRelease)
	require.NoError(t, <-beginDone)
	require.NoError(t, <-shutdownDone)
	require.EqualValues(t, 1, client.stopCalls.Load())
	storedSession, err := repo.FindSessionByID(context.Background(), session.ID)
	require.NoError(t, err)
	require.Equal(t, models.RecordingSessionStateStopped, storedSession.State)
	storedChannel, err := repo.GetChannel(context.Background(), channel.ID)
	require.NoError(t, err)
	require.True(t, storedChannel.CloudRecordingEnabled)
	require.Equal(t, models.CloudRecordingStateWaiting, storedChannel.CloudRecordingState)
}
