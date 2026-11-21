package catalog_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/catalog"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestNewCatalogChannelGetsPendingShareFromRegistrationSnapshot(t *testing.T) {
	db := newPipelineTestDB(t)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDeviceVideoSharePolicy{}, &gbmodels.GbChannelVideoShare{}))
	require.NoError(t, db.Exec("ALTER TABLE gb_device ADD COLUMN access_epoch INTEGER NOT NULL DEFAULT 1").Error)
	device := gbmodels.GbDevice{DeviceID: "37011200001170000001"}
	require.NoError(t, db.Create(&device).Error)
	require.NoError(t, db.Create(&gbmodels.GbDeviceVideoSharePolicy{
		DevicePK: device.ID, DefaultEnabled: true, Source: "config", InitializedAt: device.CreatedAt,
	}).Error)

	items := []catalog.CatalogItem{
		{DeviceID: device.DeviceID, Name: "device", CivilCode: "370112"},
		{DeviceID: "37011200001320000001", Name: "channel", CivilCode: "370112", ParentID: device.DeviceID},
	}
	pipeline := catalog.New(db)
	require.NoError(t, pipeline.Ingest(context.Background(), catalog.Sender{OwnerDeptID: 1, SourceDeviceID: device.DeviceID}, items))

	var share gbmodels.GbChannelVideoShare
	require.NoError(t, db.Take(&share).Error)
	require.True(t, share.Enabled)
	require.Equal(t, "auto", share.Source)
	require.Nil(t, share.ActivatedAt)
	require.Nil(t, share.ExpiresAt)
	require.Nil(t, share.CreatedBy)

	share.Enabled = false
	require.NoError(t, db.Save(&share).Error)
	require.NoError(t, pipeline.Ingest(context.Background(), catalog.Sender{OwnerDeptID: 1, SourceDeviceID: device.DeviceID}, items))
	var afterResync gbmodels.GbChannelVideoShare
	require.NoError(t, db.Take(&afterResync).Error)
	require.False(t, afterResync.Enabled, "Catalog resync must not reopen a manually closed share")
}
