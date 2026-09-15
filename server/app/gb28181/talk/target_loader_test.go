package talk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestGormTargetLoaderRequiresMatchingChannelAndDevice(t *testing.T) {
	db := newTalkRepoTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.GbChannel{}, &models.GbDevice{}))
	device := &models.GbDevice{DeviceID: "device-1", Status: models.DeviceStatusOnline}
	channel := &models.GbChannel{DeviceID: device.DeviceID, ChannelID: "channel-1", Status: models.ChannelStatusOnline}
	require.NoError(t, db.Create(device).Error)
	require.NoError(t, db.Create(channel).Error)
	loader := NewGormTargetLoader(db)

	loadedChannel, loadedDevice, err := loader.LoadTalkTarget(context.Background(), channel.ID, device.DeviceID)
	require.NoError(t, err)
	require.Equal(t, channel.ChannelID, loadedChannel.ChannelID)
	require.Equal(t, device.DeviceID, loadedDevice.DeviceID)

	loadedChannel, loadedDevice, err = loader.LoadTalkTarget(context.Background(), channel.ID, "other-device")
	require.NoError(t, err)
	require.Nil(t, loadedChannel)
	require.Nil(t, loadedDevice)
}
