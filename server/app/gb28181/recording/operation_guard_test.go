package recording

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/workrecording"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func TestOperationGuardRejectsLegacyPathsBeforeStateChanges(t *testing.T) {
	repo, channel := seedRecordingChannel(t, true)
	require.NoError(t, repo.db.Model(channel).Updates(map[string]any{"stream_id": "guard-stream", "cloud_recording_enabled": true}).Error)
	client := &fakeRecorderClient{}
	service := newRecordingService(repo, client, fakeLocation{nodeID: 2, ok: true}, fakeRegistry{item: &node.Node{ID: 2}})
	service.SetOperationGuard(func(_ context.Context, id uint, _ string, _ func(context.Context) error) error {
		require.Equal(t, channel.ID, id)
		return workrecording.ErrOwnerConflict
	})
	_, err := service.Enable(context.Background(), channel.ID)
	require.ErrorIs(t, err, workrecording.ErrOwnerConflict)
	_, err = service.Disable(context.Background(), channel.ID)
	require.ErrorIs(t, err, workrecording.ErrOwnerConflict)
	_, err = service.StopSession(context.Background(), channel.ID, 1)
	require.ErrorIs(t, err, workrecording.ErrOwnerConflict)
	require.ErrorIs(t, service.ReconcileChannel(context.Background(), channel.ID), workrecording.ErrOwnerConflict)
	require.ErrorIs(t, service.BeginPlayback(context.Background(), "guard-stream"), workrecording.ErrOwnerConflict)
	require.ErrorIs(t, service.EndPlayback(context.Background(), "guard-stream"), workrecording.ErrOwnerConflict)
	current, err := repo.GetChannel(context.Background(), channel.ID)
	require.NoError(t, err)
	require.True(t, current.CloudRecordingEnabled)
	var count int64
	require.NoError(t, repo.db.Model(&models.GbRecordingSession{}).Count(&count).Error)
	require.Zero(t, count)
	require.Zero(t, client.startCalls.Load())
	require.Zero(t, client.stopCalls.Load())
}
