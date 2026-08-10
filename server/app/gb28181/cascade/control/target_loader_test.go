package control

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

func TestGormTargetLoaderLoadsSourceDeviceAndChannel(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}))

	device := gbmodels.GbDevice{
		DeviceID: "source-device", IP: "192.0.2.10", Port: 5060, Transport: "TCP",
		Status: gbmodels.DeviceStatusOnline, EffectiveVersion: gbmodels.ProtocolVersion2022,
	}
	require.NoError(t, db.Create(&device).Error)
	channel := gbmodels.GbChannel{DeviceID: device.DeviceID, ChannelID: "source-channel", Status: gbmodels.ChannelStatusOnline}
	require.NoError(t, db.Create(&channel).Error)

	target, err := NewGormTargetLoader(db).Load(context.Background(), uint64(device.ID), uint64(channel.ID))
	require.NoError(t, err)
	require.Equal(t, device.ID, target.DeviceID)
	require.Equal(t, channel.ID, target.ChannelID)
	require.Equal(t, "source-device", target.DeviceCode)
	require.Equal(t, "source-channel", target.ChannelCode)
	require.Equal(t, "192.0.2.10", target.IP)
	require.Equal(t, 5060, target.Port)
	require.Equal(t, "TCP", target.Transport)
	require.True(t, target.DeviceOnline)
	require.True(t, target.ChannelOnline)
	require.Equal(t, protocol.Version(protocol.Version2022), target.Profile.Version)
}

func TestGormTargetLoaderRejectsMismatchedSourceRelation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}))

	device := gbmodels.GbDevice{DeviceID: "source-device", EffectiveVersion: gbmodels.ProtocolVersion2016}
	other := gbmodels.GbDevice{DeviceID: "other-device", EffectiveVersion: gbmodels.ProtocolVersion2016}
	require.NoError(t, db.Create(&device).Error)
	require.NoError(t, db.Create(&other).Error)
	channel := gbmodels.GbChannel{DeviceID: other.DeviceID, ChannelID: "source-channel"}
	require.NoError(t, db.Create(&channel).Error)

	_, err = NewGormTargetLoader(db).Load(context.Background(), uint64(device.ID), uint64(channel.ID))
	require.ErrorIs(t, err, ErrSourceTargetNotFound)
}
