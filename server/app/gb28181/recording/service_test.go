package recording

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type fakeLocation struct {
	nodeID int64
	ok     bool
}

func (f fakeLocation) Lookup(string) (int64, bool) { return f.nodeID, f.ok }

type fakeRegistry struct {
	item *node.Node
}

func (f fakeRegistry) Get(id int64) (*node.Node, bool) {
	if f.item == nil || f.item.ID != id {
		return nil, false
	}
	return f.item, true
}

type fakeRecorderClient struct {
	recording  bool
	startErr   error
	stopErr    error
	startCalls atomic.Int32
	stopCalls  atomic.Int32
}

func (f *fakeRecorderClient) IsRecording(context.Context, string, string, string) (bool, error) {
	return f.recording, nil
}
func (f *fakeRecorderClient) StartRecord(context.Context, string, string, string, int) error {
	f.startCalls.Add(1)
	return f.startErr
}
func (f *fakeRecorderClient) StopRecord(context.Context, string, string, string) error {
	f.stopCalls.Add(1)
	return f.stopErr
}
func seedRecordingChannel(t *testing.T, online bool) (*GormRepo, *models.GbChannel) {
	t.Helper()
	db := newRepoTestDB(t)
	channel := &models.GbChannel{DeviceID: "device", ChannelID: "channel", Status: models.ChannelStatusOffline, OnDemandLive: true}
	if online {
		channel.Status = models.ChannelStatusOnline
	}
	require.NoError(t, db.Create(channel).Error)
	return NewGormRepo(db), channel
}

func newRecordingService(repo *GormRepo, client *fakeRecorderClient, location fakeLocation, registry fakeRegistry) *Service {
	return NewService(repo, location, registry, func(*node.Node) RecorderClient {
		return client
	})
}

func TestEnableOnlyWaitsForNextPlayback(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	client := &fakeRecorderClient{}
	service := newRecordingService(repo, client, fakeLocation{nodeID: 2, ok: true}, fakeRegistry{item: &node.Node{ID: 2}})

	got, err := service.Enable(context.Background(), channel.ID)
	require.NoError(t, err)
	require.True(t, got.CloudRecordingEnabled)
	require.Equal(t, models.CloudRecordingStateWaiting, got.CloudRecordingState)
	require.Zero(t, client.startCalls.Load())
}

func TestBeginPlaybackStartsEnabledRecording(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	channel.StreamID = "stream-1"
	require.NoError(t, repo.db.Model(channel).Update("stream_id", channel.StreamID).Error)
	client := &fakeRecorderClient{}
	service := newRecordingService(repo, client, fakeLocation{nodeID: 2, ok: true}, fakeRegistry{item: &node.Node{ID: 2}})
	_, err := service.Enable(context.Background(), channel.ID)
	require.NoError(t, err)

	require.NoError(t, service.BeginPlayback(context.Background(), channel.StreamID))

	require.EqualValues(t, 1, client.startCalls.Load())
	session, err := repo.FindLatestSessionByChannel(context.Background(), channel.ID)
	require.NoError(t, err)
	require.Equal(t, models.RecordingSessionStateRecording, session.State)
}

func TestEnableReusesExistingZLMRecorder(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	channel.StreamID = "stream-1"
	require.NoError(t, repo.db.Model(channel).Update("stream_id", channel.StreamID).Error)
	client := &fakeRecorderClient{recording: true}
	service := newRecordingService(repo, client, fakeLocation{nodeID: 2, ok: true}, fakeRegistry{item: &node.Node{ID: 2}})

	_, err := service.Enable(context.Background(), channel.ID)
	require.NoError(t, err)
	require.NoError(t, service.BeginPlayback(context.Background(), channel.StreamID))
	got, err := repo.GetChannel(context.Background(), channel.ID)
	require.NoError(t, err)
	require.Equal(t, models.CloudRecordingStateRecording, got.CloudRecordingState)
	require.Zero(t, client.startCalls.Load())
}

func TestEnableWaitsWhenNodeBindingMissing(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	channel.StreamID = "stream-1"
	require.NoError(t, repo.db.Model(channel).Update("stream_id", channel.StreamID).Error)
	service := newRecordingService(repo, &fakeRecorderClient{}, fakeLocation{}, fakeRegistry{})

	_, err := service.Enable(context.Background(), channel.ID)
	require.NoError(t, err)
	require.NoError(t, service.BeginPlayback(context.Background(), channel.StreamID))
	got, err := repo.GetChannel(context.Background(), channel.ID)
	require.NoError(t, err)
	require.Equal(t, models.CloudRecordingStateWaiting, got.CloudRecordingState)
}

func TestEnableKeepsDesiredStateWhenStartRecordFails(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	channel.StreamID = "stream-1"
	require.NoError(t, repo.db.Model(channel).Update("stream_id", channel.StreamID).Error)
	client := &fakeRecorderClient{startErr: errors.New("zlm unavailable")}
	service := newRecordingService(repo, client, fakeLocation{nodeID: 2, ok: true}, fakeRegistry{item: &node.Node{ID: 2}})

	_, err := service.Enable(context.Background(), channel.ID)
	require.NoError(t, err)
	require.NoError(t, service.BeginPlayback(context.Background(), channel.StreamID))
	got, err := repo.GetChannel(context.Background(), channel.ID)
	require.NoError(t, err)
	require.True(t, got.CloudRecordingEnabled)
	require.Equal(t, models.CloudRecordingStateFailed, got.CloudRecordingState)
	require.Contains(t, got.CloudRecordingError, "启动录像失败")
}

func seedActiveSession(t *testing.T, repo *GormRepo, channel *models.GbChannel) *models.GbRecordingSession {
	t.Helper()
	_, err := repo.SetDesired(context.Background(), channel.ID, true)
	require.NoError(t, err)
	session := &models.GbRecordingSession{
		ChannelID: channel.ID, DeviceID: channel.DeviceID, NodeID: 2,
		VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp,
		Stream: "stream-1", State: models.RecordingSessionStateRecording,
	}
	require.NoError(t, repo.UpsertSession(context.Background(), session))
	return session
}

func TestDisableStopsRecordingAndMarksSessionStopped(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	session := seedActiveSession(t, repo, channel)
	client := &fakeRecorderClient{}
	service := NewService(repo, fakeLocation{nodeID: 2, ok: true}, fakeRegistry{item: &node.Node{ID: 2}}, func(*node.Node) RecorderClient { return client })

	got, err := service.Disable(context.Background(), channel.ID)
	require.NoError(t, err)
	require.False(t, got.CloudRecordingEnabled)
	require.Equal(t, models.CloudRecordingStateDisabled, got.CloudRecordingState)
	require.EqualValues(t, 1, client.stopCalls.Load())
	stored, err := repo.FindSessionByMedia(context.Background(), 2, session.VHost, session.App, session.Stream)
	require.NoError(t, err)
	require.Equal(t, models.RecordingSessionStateStopped, stored.State)
}

func TestDisableIsIdempotentWithoutSession(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	service := newRecordingService(repo, &fakeRecorderClient{}, fakeLocation{}, fakeRegistry{})

	got, err := service.Disable(context.Background(), channel.ID)
	require.NoError(t, err)
	require.Equal(t, models.CloudRecordingStateDisabled, got.CloudRecordingState)
}

func TestDisableRetainsSessionWhenStopRecordFails(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	session := seedActiveSession(t, repo, channel)
	client := &fakeRecorderClient{stopErr: errors.New("zlm unavailable")}
	service := NewService(repo, fakeLocation{}, fakeRegistry{item: &node.Node{ID: 2}}, func(*node.Node) RecorderClient { return client })

	got, err := service.Disable(context.Background(), channel.ID)
	require.NoError(t, err)
	require.False(t, got.CloudRecordingEnabled)
	require.Equal(t, models.CloudRecordingStateFailed, got.CloudRecordingState)
	stored, err := repo.FindSessionByMedia(context.Background(), 2, session.VHost, session.App, session.Stream)
	require.NoError(t, err)
	require.NotEqual(t, models.RecordingSessionStateStopped, stored.State)
}

func TestConcurrentEnableDoesNotStartRecorder(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	client := &fakeRecorderClient{}
	service := newRecordingService(repo, client, fakeLocation{nodeID: 2, ok: true}, fakeRegistry{item: &node.Node{ID: 2}})

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.Enable(context.Background(), channel.ID)
			require.NoError(t, err)
		}()
	}
	wg.Wait()
	require.Zero(t, client.startCalls.Load())
}

func TestObserveRegisteredStreamStartsEnabledRecordingOnce(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	channel.StreamID = "stream-observed"
	require.NoError(t, repo.db.Model(channel).Update("stream_id", channel.StreamID).Error)
	_, err := repo.SetDesired(context.Background(), channel.ID, true)
	require.NoError(t, err)
	client := &fakeRecorderClient{}
	service := newRecordingService(repo, client, fakeLocation{nodeID: 2, ok: true}, fakeRegistry{item: &node.Node{ID: 2}})

	require.NoError(t, service.ObserveStream(context.Background(), channel.StreamID, true))
	require.NoError(t, service.ObserveStream(context.Background(), channel.StreamID, true))
	require.Zero(t, client.startCalls.Load())
	stored, err := repo.GetChannel(context.Background(), channel.ID)
	require.NoError(t, err)
	require.Equal(t, models.CloudRecordingStateWaiting, stored.CloudRecordingState)
}

func TestEndPlaybackStopsRecordingAndKeepsEnabledForNextPlayback(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	channel.StreamID = "stream-1"
	require.NoError(t, repo.db.Model(channel).Update("stream_id", channel.StreamID).Error)
	session := seedActiveSession(t, repo, channel)
	client := &fakeRecorderClient{}
	service := newRecordingService(repo, client, fakeLocation{nodeID: 2, ok: true}, fakeRegistry{item: &node.Node{ID: 2}})

	require.NoError(t, service.EndPlayback(context.Background(), channel.StreamID))

	require.EqualValues(t, 1, client.stopCalls.Load())
	stored, err := repo.GetChannel(context.Background(), channel.ID)
	require.NoError(t, err)
	require.True(t, stored.CloudRecordingEnabled)
	require.Equal(t, models.CloudRecordingStateWaiting, stored.CloudRecordingState)
	storedSession, err := repo.FindSessionByMedia(context.Background(), session.NodeID, session.VHost, session.App, session.Stream)
	require.NoError(t, err)
	require.Equal(t, models.RecordingSessionStateStopped, storedSession.State)
}

func TestObserveRegisteredStreamIgnoresDisabledChannel(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	channel.StreamID = "stream-disabled"
	require.NoError(t, repo.db.Model(channel).Update("stream_id", channel.StreamID).Error)
	client := &fakeRecorderClient{}
	service := newRecordingService(repo, client, fakeLocation{nodeID: 2, ok: true}, fakeRegistry{item: &node.Node{ID: 2}})

	require.NoError(t, service.ObserveStream(context.Background(), channel.StreamID, true))
	require.Zero(t, client.startCalls.Load())
}

func TestObserveUnregisteredStreamMovesEnabledChannelToWaiting(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	channel.StreamID = "stream-lost"
	require.NoError(t, repo.db.Model(channel).Update("stream_id", channel.StreamID).Error)
	session := seedActiveSession(t, repo, channel)
	session.Stream = channel.StreamID
	require.NoError(t, repo.UpsertSession(context.Background(), session))
	service := newRecordingService(repo, &fakeRecorderClient{}, fakeLocation{}, fakeRegistry{})

	require.NoError(t, service.ObserveStream(context.Background(), channel.StreamID, false))
	stored, err := repo.GetChannel(context.Background(), channel.ID)
	require.NoError(t, err)
	require.True(t, stored.CloudRecordingEnabled)
	require.Equal(t, models.CloudRecordingStateWaiting, stored.CloudRecordingState)
	storedSession, err := repo.FindSessionByMedia(context.Background(), session.NodeID, session.VHost, session.App, session.Stream)
	require.NoError(t, err)
	require.Equal(t, models.RecordingSessionStateStopped, storedSession.State)
}

func TestObserveUnregisteredStreamKeepsDisabledChannelDisabled(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	channel.StreamID = "stream-disabled-lost"
	require.NoError(t, repo.db.Model(channel).Update("stream_id", channel.StreamID).Error)
	session := &models.GbRecordingSession{
		ChannelID: channel.ID, DeviceID: channel.DeviceID, NodeID: 2,
		VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp,
		Stream: channel.StreamID, State: models.RecordingSessionStateRecording,
	}
	require.NoError(t, repo.UpsertSession(context.Background(), session))
	service := newRecordingService(repo, &fakeRecorderClient{}, fakeLocation{}, fakeRegistry{})

	require.NoError(t, service.ObserveStream(context.Background(), channel.StreamID, false))
	stored, err := repo.GetChannel(context.Background(), channel.ID)
	require.NoError(t, err)
	require.False(t, stored.CloudRecordingEnabled)
	require.Equal(t, models.CloudRecordingStateDisabled, stored.CloudRecordingState)
	storedSession, err := repo.FindSessionByMedia(context.Background(), session.NodeID, session.VHost, session.App, session.Stream)
	require.NoError(t, err)
	require.Equal(t, models.RecordingSessionStateStopped, storedSession.State)
}
