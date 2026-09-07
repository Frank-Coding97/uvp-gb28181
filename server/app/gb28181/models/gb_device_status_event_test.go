package models_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestDeviceStatusEvent_AutoMigrate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDeviceStatusEvent{}))

	migrator := db.Migrator()
	require.True(t, migrator.HasTable(&gbmodels.GbDeviceStatusEvent{}))
	for _, column := range []string{
		"device_id", "device_code", "event_type", "from_status",
		"to_status", "occurred_at", "source", "detail",
	} {
		assert.True(t, migrator.HasColumn(&gbmodels.GbDeviceStatusEvent{}, column), column)
	}
	assert.True(t, migrator.HasIndex(&gbmodels.GbDeviceStatusEvent{}, "idx_device_occurred"))
	assert.True(t, migrator.HasIndex(&gbmodels.GbDeviceStatusEvent{}, "idx_event_type_occurred"))
	assert.True(t, migrator.HasIndex(&gbmodels.GbDeviceStatusEvent{}, "idx_device_code"))
}

func TestDeviceStatusEventType_Valid(t *testing.T) {
	valid := []gbmodels.DeviceStatusEventType{
		gbmodels.DeviceEventRegisterOnline,
		gbmodels.DeviceEventUnregisterOffline,
		gbmodels.DeviceEventHeartbeatTimeout,
		gbmodels.DeviceEventHeartbeatRecovered,
		gbmodels.DeviceEventRegisterRenewed,
	}
	for _, eventType := range valid {
		assert.True(t, eventType.Valid(), string(eventType))
	}
	assert.False(t, gbmodels.DeviceStatusEventType("unknown").Valid())
}
