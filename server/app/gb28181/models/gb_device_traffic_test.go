package models_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestDeviceTrafficModelsAutoMigrate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbDeviceTrafficSession{},
		&gbmodels.GbDeviceTrafficDaily{},
		&gbmodels.GbDeviceTrafficHourly{},
		&gbmodels.GbDeviceTrafficGap{},
	))

	for _, model := range []interface{}{
		&gbmodels.GbDeviceTrafficSession{},
		&gbmodels.GbDeviceTrafficDaily{},
		&gbmodels.GbDeviceTrafficGap{},
	} {
		require.True(t, db.Migrator().HasTable(model))
	}

	for _, column := range []string{"business_key", "device_code", "channel_code", "last_total_bytes", "settled_total_bytes", "state"} {
		require.True(t, db.Migrator().HasColumn(&gbmodels.GbDeviceTrafficSession{}, column), column)
	}
	for _, column := range []string{"stat_date", "device_code", "channel_code", "upstream_bytes", "downstream_bytes"} {
		require.True(t, db.Migrator().HasColumn(&gbmodels.GbDeviceTrafficDaily{}, column), column)
	}
	for _, column := range []string{"stat_hour", "device_code", "channel_code", "upstream_bytes", "downstream_bytes"} {
		require.True(t, db.Migrator().HasColumn(&gbmodels.GbDeviceTrafficHourly{}, column), column)
	}
	for _, column := range []string{"node_id", "reason", "started_at", "ended_at", "state"} {
		require.True(t, db.Migrator().HasColumn(&gbmodels.GbDeviceTrafficGap{}, column), column)
	}
}

func TestDeviceTrafficSessionBusinessKeyIsUnique(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDeviceTrafficSession{}))

	row := &gbmodels.GbDeviceTrafficSession{BusinessKey: "live:node-1:stream-1:1", DeviceCode: "device-1", ChannelCode: "channel-1"}
	require.NoError(t, db.Create(row).Error)
	duplicate := *row
	duplicate.ID = 0
	require.Error(t, db.Create(&duplicate).Error)
}
