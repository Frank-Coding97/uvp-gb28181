package models_test

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestVideoShareModelIsUniquePerChannelAndPersistsPendingState(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbChannelVideoShare{}, &gbmodels.GbDeviceVideoSharePolicy{}))

	device := gbmodels.GbDevice{DeviceID: "34020000002000000001"}
	require.NoError(t, db.Create(&device).Error)
	channel := gbmodels.GbChannel{ChannelID: "34020000001320000001", DeviceID: device.DeviceID}
	require.NoError(t, db.Create(&channel).Error)
	policy := gbmodels.GbDeviceVideoSharePolicy{DevicePK: device.ID, DefaultEnabled: true, Source: "global", PolicyRevision: 1, InitializedAt: time.Now().UTC()}
	require.NoError(t, db.Create(&policy).Error)

	share := gbmodels.GbChannelVideoShare{
		ChannelPK: channel.ID, PublicID: "public-1", Generation: "generation-1", Enabled: true,
		Source: "auto", PolicyRevision: 1, KeyID: "key-1", Revision: 1,
	}
	require.NoError(t, db.Create(&share).Error)
	require.Error(t, db.Create(&gbmodels.GbChannelVideoShare{
		ChannelPK: channel.ID, PublicID: "public-2", Generation: "generation-2", Enabled: true,
		Source: "manual", KeyID: "key-1", Revision: 1,
	}).Error, "one share row must represent a real channel across all directory mounts")

	var loaded gbmodels.GbChannelVideoShare
	require.NoError(t, db.Where("channel_pk = ?", channel.ID).Take(&loaded).Error)
	require.Nil(t, loaded.ActivatedAt, "automatically enabled shares stay pending before link acquisition")
	require.Nil(t, loaded.ExpiresAt)
	require.Nil(t, loaded.CreatedBy)
	require.Equal(t, "auto", loaded.Source)

	now := time.Now().UTC()
	loaded.ActivatedAt = &now
	loaded.ExpiresAt = &now
	loaded.CreatedBy = ptrUint(1)
	require.NoError(t, db.Save(&loaded).Error)
}

func ptrUint(v uint) *uint { return &v }
