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
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

type fakeStarter struct {
	result *play.Result
	err    error
	calls  atomic.Int32
}

func (f *fakeStarter) Start(context.Context, string, string) (*play.Result, error) {
	f.calls.Add(1)
	return f.result, f.err
}

type fakeStopper struct{ calls atomic.Int32 }

func (f *fakeStopper) Stop(context.Context, string) error {
	f.calls.Add(1)
	return nil
}

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
	mediaInfo  *zlm.MediaInfo
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
func (f *fakeRecorderClient) GetMediaInfo(context.Context, string, string) (*zlm.MediaInfo, error) {
	if f.mediaInfo == nil {
		return &zlm.MediaInfo{}, nil
	}
	return f.mediaInfo, nil
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

func newRecordingService(repo *GormRepo, starter *fakeStarter, client *fakeRecorderClient, location fakeLocation, registry fakeRegistry) *Service {
	return NewService(repo, starter, &fakeStopper{}, location, registry, func(*node.Node) RecorderClient {
		return client
	})
}

func TestEnableWaitsWhenDeviceOffline(t *testing.T) {
	repo, channel := seedRecordingChannel(t, false)
	starter := &fakeStarter{err: play.ErrDeviceOffline}
	service := newRecordingService(repo, starter, &fakeRecorderClient{}, fakeLocation{}, fakeRegistry{})

	got, err := service.Enable(context.Background(), channel.ID)
	require.NoError(t, err)
	require.True(t, got.CloudRecordingEnabled)
	require.Equal(t, models.CloudRecordingStateWaiting, got.CloudRecordingState)
}

func TestEnableStartsStreamAndRecording(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	starter := &fakeStarter{result: &play.Result{StreamID: "stream-1"}}
	client := &fakeRecorderClient{}
	service := newRecordingService(repo, starter, client, fakeLocation{nodeID: 2, ok: true}, fakeRegistry{item: &node.Node{ID: 2}})

	got, err := service.Enable(context.Background(), channel.ID)
	require.NoError(t, err)
	require.Equal(t, models.CloudRecordingStateRecording, got.CloudRecordingState)
	require.EqualValues(t, 1, starter.calls.Load())
	require.EqualValues(t, 1, client.startCalls.Load())
	session, err := repo.FindLatestSessionByChannel(context.Background(), channel.ID)
	require.NoError(t, err)
	require.Equal(t, models.RecordingSessionStateRecording, session.State)
}

func TestEnableReusesExistingZLMRecorder(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	client := &fakeRecorderClient{recording: true}
	service := newRecordingService(repo, &fakeStarter{result: &play.Result{StreamID: "stream-1"}}, client, fakeLocation{nodeID: 2, ok: true}, fakeRegistry{item: &node.Node{ID: 2}})

	got, err := service.Enable(context.Background(), channel.ID)
	require.NoError(t, err)
	require.Equal(t, models.CloudRecordingStateRecording, got.CloudRecordingState)
	require.Zero(t, client.startCalls.Load())
}

func TestEnableWaitsWhenNodeBindingMissing(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	service := newRecordingService(repo, &fakeStarter{result: &play.Result{StreamID: "stream-1"}}, &fakeRecorderClient{}, fakeLocation{}, fakeRegistry{})

	got, err := service.Enable(context.Background(), channel.ID)
	require.NoError(t, err)
	require.Equal(t, models.CloudRecordingStateWaiting, got.CloudRecordingState)
}

func TestEnableKeepsDesiredStateWhenStartRecordFails(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	client := &fakeRecorderClient{startErr: errors.New("zlm unavailable")}
	service := newRecordingService(repo, &fakeStarter{result: &play.Result{StreamID: "stream-1"}}, client, fakeLocation{nodeID: 2, ok: true}, fakeRegistry{item: &node.Node{ID: 2}})

	got, err := service.Enable(context.Background(), channel.ID)
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

func TestDisableStopsRecordingButKeepsStreamWithReaders(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	session := seedActiveSession(t, repo, channel)
	client := &fakeRecorderClient{mediaInfo: &zlm.MediaInfo{Online: true, ReaderCount: 2}}
	stopper := &fakeStopper{}
	service := NewService(repo, &fakeStarter{}, stopper, fakeLocation{nodeID: 2, ok: true}, fakeRegistry{item: &node.Node{ID: 2}}, func(*node.Node) RecorderClient { return client })

	got, err := service.Disable(context.Background(), channel.ID)
	require.NoError(t, err)
	require.False(t, got.CloudRecordingEnabled)
	require.Equal(t, models.CloudRecordingStateDisabled, got.CloudRecordingState)
	require.EqualValues(t, 1, client.stopCalls.Load())
	require.Zero(t, stopper.calls.Load())
	stored, err := repo.FindSessionByMedia(context.Background(), 2, session.VHost, session.App, session.Stream)
	require.NoError(t, err)
	require.Equal(t, models.RecordingSessionStateStopped, stored.State)
}

func TestDisableStopsIdleOnDemandStream(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	seedActiveSession(t, repo, channel)
	client := &fakeRecorderClient{mediaInfo: &zlm.MediaInfo{Online: true, ReaderCount: 0}}
	stopper := &fakeStopper{}
	service := NewService(repo, &fakeStarter{}, stopper, fakeLocation{}, fakeRegistry{item: &node.Node{ID: 2}}, func(*node.Node) RecorderClient { return client })

	_, err := service.Disable(context.Background(), channel.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, stopper.calls.Load())
}

func TestDisableKeepsAlwaysOnStreamWithoutReaders(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	channel.OnDemandLive = false
	require.NoError(t, repo.db.Model(channel).Update("on_demand_live", false).Error)
	seedActiveSession(t, repo, channel)
	client := &fakeRecorderClient{mediaInfo: &zlm.MediaInfo{Online: true, ReaderCount: 0}}
	stopper := &fakeStopper{}
	service := NewService(repo, &fakeStarter{}, stopper, fakeLocation{}, fakeRegistry{item: &node.Node{ID: 2}}, func(*node.Node) RecorderClient { return client })

	_, err := service.Disable(context.Background(), channel.ID)
	require.NoError(t, err)
	require.Zero(t, stopper.calls.Load())
}

func TestDisableIsIdempotentWithoutSession(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	service := newRecordingService(repo, &fakeStarter{}, &fakeRecorderClient{}, fakeLocation{}, fakeRegistry{})

	got, err := service.Disable(context.Background(), channel.ID)
	require.NoError(t, err)
	require.Equal(t, models.CloudRecordingStateDisabled, got.CloudRecordingState)
}

func TestDisableRetainsSessionWhenStopRecordFails(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	session := seedActiveSession(t, repo, channel)
	client := &fakeRecorderClient{stopErr: errors.New("zlm unavailable")}
	service := NewService(repo, &fakeStarter{}, &fakeStopper{}, fakeLocation{}, fakeRegistry{item: &node.Node{ID: 2}}, func(*node.Node) RecorderClient { return client })

	got, err := service.Disable(context.Background(), channel.ID)
	require.NoError(t, err)
	require.False(t, got.CloudRecordingEnabled)
	require.Equal(t, models.CloudRecordingStateFailed, got.CloudRecordingState)
	stored, err := repo.FindSessionByMedia(context.Background(), 2, session.VHost, session.App, session.Stream)
	require.NoError(t, err)
	require.NotEqual(t, models.RecordingSessionStateStopped, stored.State)
}

func TestConcurrentEnableStartsRecorderOnce(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	starter := &fakeStarter{result: &play.Result{StreamID: "stream-1"}}
	client := &fakeRecorderClient{}
	service := newRecordingService(repo, starter, client, fakeLocation{nodeID: 2, ok: true}, fakeRegistry{item: &node.Node{ID: 2}})

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
	require.EqualValues(t, 1, starter.calls.Load())
	require.EqualValues(t, 1, client.startCalls.Load())
}

type parallelStarter struct {
	active    atomic.Int32
	maxActive atomic.Int32
}

func (p *parallelStarter) Start(_ context.Context, _, channelID string) (*play.Result, error) {
	active := p.active.Add(1)
	for {
		current := p.maxActive.Load()
		if active <= current || p.maxActive.CompareAndSwap(current, active) {
			break
		}
	}
	time.Sleep(40 * time.Millisecond)
	p.active.Add(-1)
	return &play.Result{StreamID: "stream-" + channelID}, nil
}

func TestDifferentChannelsCanReconcileConcurrently(t *testing.T) {
	db := newRepoTestDB(t)
	repo := NewGormRepo(db)
	first := &models.GbChannel{DeviceID: "device", ChannelID: "first", Status: models.ChannelStatusOnline}
	second := &models.GbChannel{DeviceID: "device", ChannelID: "second", Status: models.ChannelStatusOnline}
	require.NoError(t, db.Create(first).Error)
	require.NoError(t, db.Create(second).Error)
	starter := &parallelStarter{}
	service := NewService(repo, starter, &fakeStopper{}, fakeLocation{nodeID: 2, ok: true}, fakeRegistry{item: &node.Node{ID: 2}}, func(*node.Node) RecorderClient {
		return &fakeRecorderClient{}
	})

	var wg sync.WaitGroup
	for _, id := range []uint{first.ID, second.ID} {
		wg.Add(1)
		go func(channelID uint) {
			defer wg.Done()
			_, err := service.Enable(context.Background(), channelID)
			require.NoError(t, err)
		}(id)
	}
	wg.Wait()
	require.EqualValues(t, 2, starter.maxActive.Load())
}
