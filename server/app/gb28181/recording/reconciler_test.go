package recording

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func TestReconcilerRunOnceStartsEnabledRecording(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	_, err := repo.SetDesired(context.Background(), channel.ID, true)
	require.NoError(t, err)
	starter := &fakeStarter{result: &play.Result{StreamID: "stream-reconcile"}}
	client := &fakeRecorderClient{}
	service := newRecordingService(repo, starter, client, fakeLocation{nodeID: 2, ok: true}, fakeRegistry{item: &node.Node{ID: 2}})
	reconciler := NewReconciler(repo, service, time.Minute, time.Second)

	require.NoError(t, reconciler.RunOnce(context.Background()))
	require.EqualValues(t, 1, starter.calls.Load())
	require.EqualValues(t, 1, client.startCalls.Load())
	stored, err := repo.GetChannel(context.Background(), channel.ID)
	require.NoError(t, err)
	require.Equal(t, models.CloudRecordingStateRecording, stored.CloudRecordingState)
}

func TestReconcilerRunOnceStopsDisabledResidualSession(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	session := &models.GbRecordingSession{
		ChannelID: channel.ID, DeviceID: channel.DeviceID, NodeID: 2,
		VHost: models.DefaultRecordingVHost, App: models.DefaultRecordingApp,
		Stream: "stream-residual", State: models.RecordingSessionStateRecording,
	}
	require.NoError(t, repo.UpsertSession(context.Background(), session))
	client := &fakeRecorderClient{}
	service := newRecordingService(repo, &fakeStarter{}, client, fakeLocation{}, fakeRegistry{item: &node.Node{ID: 2}})
	reconciler := NewReconciler(repo, service, time.Minute, time.Second)

	require.NoError(t, reconciler.RunOnce(context.Background()))
	require.EqualValues(t, 1, client.stopCalls.Load())
	stored, err := repo.FindSessionByMedia(context.Background(), session.NodeID, session.VHost, session.App, session.Stream)
	require.NoError(t, err)
	require.Equal(t, models.RecordingSessionStateStopped, stored.State)
}

func TestReconcilerStopPreventsFurtherCalls(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	_, err := repo.SetDesired(context.Background(), channel.ID, true)
	require.NoError(t, err)
	starter := &fakeStarter{result: &play.Result{StreamID: "stream-periodic"}}
	client := &fakeRecorderClient{recording: true}
	service := newRecordingService(repo, starter, client, fakeLocation{nodeID: 2, ok: true}, fakeRegistry{item: &node.Node{ID: 2}})
	reconciler := NewReconciler(repo, service, 15*time.Millisecond, time.Second)

	reconciler.Start(context.Background())
	require.Eventually(t, func() bool { return starter.calls.Load() >= 2 }, time.Second, 10*time.Millisecond)
	reconciler.Stop()
	callsAfterStop := starter.calls.Load()
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, callsAfterStop, starter.calls.Load())
}
