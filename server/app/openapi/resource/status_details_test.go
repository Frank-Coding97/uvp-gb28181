package resource

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStatusDetailsKeepUnknownSeparateFromExplicitFalseAndScopeFacts(t *testing.T) {
	db := newResourceTestDB(t)
	svc := New(db)
	ctx := context.Background()
	scope := DepartmentScope{OwnerDeptID: resourceOwnerDept, DataScope: DataScopeDepartment}
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, db.Table("gb_device").Where("device_id = ?", resourceDevice).Updates(map[string]any{"register_time": now, "keepalive_time": now, "keepalive_interval": 30, "effective_version": "2022"}).Error)
	status, err := svc.GetDeviceStatusInScope(ctx, scope, resourceDevice)
	require.NoError(t, err)
	require.NotNil(t, status.Online)
	require.True(t, *status.Online)
	require.Equal(t, now, *status.KeepaliveTime)
	require.Equal(t, 30, status.KeepaliveInterval)
	require.Equal(t, "2022", status.ProtocolVersion)
	require.NoError(t, db.Table("gb_channel").Where("device_id = ? AND channel_id = ?", resourceDevice, resourceChannel).Updates(map[string]any{"status": 0, "stream_id": "stream-123", "audio_enabled": false, "cloud_recording_state": "recording", "cloud_recording_enabled": true, "cloud_recording_updated_at": now}).Error)
	channelStatus, err := svc.GetChannelStatusInScope(ctx, scope, resourceDevice, resourceChannel)
	require.NoError(t, err)
	require.NotNil(t, channelStatus.Online)
	require.False(t, *channelStatus.Online)
	require.False(t, channelStatus.AudioEnabled)
	require.Equal(t, "stream-123", channelStatus.StreamID)
	require.Equal(t, "recording", channelStatus.CloudRecordingState)
	require.Equal(t, now, *channelStatus.CloudRecordingUpdatedAt)
	require.NoError(t, db.Table("gb_channel").Where("device_id = ? AND channel_id = ?", resourceDevice, resourceChannel).Update("status", nil).Error)
	channelStatus, err = svc.GetChannelStatusInScope(ctx, scope, resourceDevice, resourceChannel)
	require.NoError(t, err)
	require.Nil(t, channelStatus.Online)
	_, err = svc.GetChannelStatusInScope(ctx, scope, resourceOther, "37011200001310000020")
	require.ErrorIs(t, err, ErrResourceNotFound)
}
