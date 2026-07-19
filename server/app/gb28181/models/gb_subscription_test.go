package models_test

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func newSubscriptionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbDeviceSubscription{},
		&gbmodels.GbMobilePositionLatest{},
		&gbmodels.GbAlarmEvent{},
	))
	return db
}

func TestSubscriptionModels_UniqueKeys(t *testing.T) {
	db := newSubscriptionTestDB(t)
	sub := gbmodels.GbDeviceSubscription{DeviceID: 1, Kind: gbmodels.SubscriptionKindCatalog, Enabled: true, Status: gbmodels.SubscriptionStatusPending}
	require.NoError(t, db.Create(&sub).Error)
	require.Error(t, db.Create(&gbmodels.GbDeviceSubscription{DeviceID: 1, Kind: gbmodels.SubscriptionKindCatalog}).Error)

	now := time.Now()
	pos := gbmodels.GbMobilePositionLatest{DeviceID: 1, SourceCode: "34020000001320000001", EventTime: now, ReceivedAt: now, Longitude: 116.4, Latitude: 39.9}
	require.NoError(t, db.Create(&pos).Error)
	require.Error(t, db.Create(&gbmodels.GbMobilePositionLatest{DeviceID: 1, SourceCode: pos.SourceCode, EventTime: now, ReceivedAt: now}).Error)

	alarm := gbmodels.GbAlarmEvent{DeviceID: 1, SourceCode: pos.SourceCode, DedupeKey: "call-id:1:1"}
	require.NoError(t, db.Create(&alarm).Error)
	require.Error(t, db.Create(&gbmodels.GbAlarmEvent{DeviceID: 1, SourceCode: pos.SourceCode, DedupeKey: alarm.DedupeKey}).Error)
}

func TestSubscriptionKinds_Validate(t *testing.T) {
	for _, kind := range []gbmodels.SubscriptionKind{
		gbmodels.SubscriptionKindCatalog,
		gbmodels.SubscriptionKindMobilePosition,
		gbmodels.SubscriptionKindAlarm,
	} {
		require.True(t, kind.Valid())
	}
	require.False(t, gbmodels.SubscriptionKind("unexpected").Valid())
}
