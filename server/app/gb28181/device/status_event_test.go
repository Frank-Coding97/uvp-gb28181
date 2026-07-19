package device

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
)

func TestHandleRegister_StatusEventSemantics(t *testing.T) {
	db := newStatusEventTestDB(t)

	ctx := context.Background()
	info := RegisterInfo{DeviceID: "34020000002000000031", Transport: "TCP", IP: "192.0.2.31", Port: 5060, Expires: 3600}

	first, err := HandleRegister(ctx, info, 60)
	require.NoError(t, err)
	assert.True(t, first)
	assertStatusEvents(t, db, []gbmodels.DeviceStatusEventType{gbmodels.DeviceEventRegisterOnline})

	second, err := HandleRegister(ctx, info, 3600)
	require.NoError(t, err)
	assert.False(t, second)
	assertStatusEvents(t, db, []gbmodels.DeviceStatusEventType{gbmodels.DeviceEventRegisterOnline, gbmodels.DeviceEventRegisterRenewed})

	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("device_id = ?", info.DeviceID).Update("status", gbmodels.DeviceStatusOffline).Error)
	restored, err := HandleRegister(ctx, info, 3600)
	require.NoError(t, err)
	assert.True(t, restored)
	assertStatusEvents(t, db, []gbmodels.DeviceStatusEventType{
		gbmodels.DeviceEventRegisterOnline,
		gbmodels.DeviceEventRegisterRenewed,
		gbmodels.DeviceEventRegisterOnline,
	})
}

func TestOfflineAndKeepalive_StatusEventSemantics(t *testing.T) {
	db := newStatusEventTestDB(t)
	ctx := context.Background()
	info := RegisterInfo{DeviceID: "34020000002000000032", Transport: "UDP", IP: "192.0.2.32", Port: 5060, Expires: 3600}
	_, err := HandleRegister(ctx, info, 60)
	require.NoError(t, err)

	require.NoError(t, HandleUnregister(ctx, info.DeviceID))
	require.NoError(t, HandleUnregister(ctx, info.DeviceID))
	assertStatusEvents(t, db, []gbmodels.DeviceStatusEventType{
		gbmodels.DeviceEventRegisterOnline,
		gbmodels.DeviceEventUnregisterOffline,
	})

	restored, err := Keepalive(ctx, info.DeviceID)
	require.NoError(t, err)
	assert.True(t, restored)
	assertStatusEvents(t, db, []gbmodels.DeviceStatusEventType{
		gbmodels.DeviceEventRegisterOnline,
		gbmodels.DeviceEventUnregisterOffline,
		gbmodels.DeviceEventHeartbeatRecovered,
	})

	restored, err = Keepalive(ctx, info.DeviceID)
	require.NoError(t, err)
	assert.False(t, restored)
	assertStatusEvents(t, db, []gbmodels.DeviceStatusEventType{
		gbmodels.DeviceEventRegisterOnline,
		gbmodels.DeviceEventUnregisterOffline,
		gbmodels.DeviceEventHeartbeatRecovered,
	})

	require.NoError(t, gbmodels.MarkOffline(ctx, info.DeviceID))
	require.NoError(t, gbmodels.MarkOffline(ctx, info.DeviceID))
	assertStatusEvents(t, db, []gbmodels.DeviceStatusEventType{
		gbmodels.DeviceEventRegisterOnline,
		gbmodels.DeviceEventUnregisterOffline,
		gbmodels.DeviceEventHeartbeatRecovered,
		gbmodels.DeviceEventHeartbeatTimeout,
	})
}

func newStatusEventTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	prevDB, prevConfig := app.GormDbMysql, app.ConfigYml
	t.Cleanup(func() { app.GormDbMysql, app.ConfigYml = prevDB, prevConfig })

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbDevice{},
		&gbmodels.GbChannel{},
		&gbmodels.GbDeviceStatusEvent{},
		&basemodels.SysDepartment{},
	))
	app.GormDbMysql = db
	app.ConfigYml = testConfig{}
	require.NoError(t, db.Create(&basemodels.SysDepartment{BaseModel: basemodels.BaseModel{ID: 1}, Name: "接入池"}).Error)
	return db
}

func assertStatusEvents(t *testing.T, db *gorm.DB, expected []gbmodels.DeviceStatusEventType) {
	t.Helper()
	var events []gbmodels.GbDeviceStatusEvent
	require.NoError(t, db.Order("id").Find(&events).Error)
	require.Len(t, events, len(expected))
	for i, eventType := range expected {
		assert.Equal(t, eventType, events[i].EventType)
	}
}
