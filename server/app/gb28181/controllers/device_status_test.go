package controllers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestDeviceMgmtGetDeviceStatusReadsCurrentDeviceCacheOnly(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbDeviceControlState{},
		&gbmodels.GbAlarmResource{}, &gbmodels.GbAlarmResourceParent{}, &gbmodels.GbAlarmBinding{},
	))

	firstDevice := &gbmodels.GbDevice{DeviceID: "D1", Status: gbmodels.DeviceStatusOnline}
	secondDevice := &gbmodels.GbDevice{DeviceID: "D2", Status: gbmodels.DeviceStatusOnline}
	require.NoError(t, db.Create(firstDevice).Error)
	require.NoError(t, db.Create(secondDevice).Error)
	firstChannel := &gbmodels.GbChannel{DeviceID: firstDevice.DeviceID, ChannelID: "C", Status: gbmodels.ChannelStatusOnline}
	secondChannel := &gbmodels.GbChannel{DeviceID: secondDevice.DeviceID, ChannelID: "C", Status: gbmodels.ChannelStatusOnline}
	require.NoError(t, db.Create(firstChannel).Error)
	require.NoError(t, db.Create(secondChannel).Error)
	now := time.Now()
	require.NoError(t, db.Create(&gbmodels.GbDeviceControlState{
		DeviceID: firstDevice.ID, ChannelID: firstChannel.ID,
		TargetScope: gbmodels.ControlTargetScopeChannel, TargetCode: "C",
		RecordState: gbmodels.ControlStateOn, ObservedAt: now,
	}).Error)
	require.NoError(t, db.Create(&gbmodels.GbDeviceControlState{
		DeviceID: secondDevice.ID, ChannelID: secondChannel.ID,
		TargetScope: gbmodels.ControlTargetScopeChannel, TargetCode: "C",
		RecordState: gbmodels.ControlStateOff, ObservedAt: now,
	}).Error)

	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	router := gin.New()
	router.GET("/channel/:id/device-status", controller.GetDeviceStatus)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/channel/"+uintStr(secondChannel.ID)+"/device-status", nil))

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"recordState":"off"`)
	require.NotContains(t, response.Body.String(), `"recordState":"on"`)
}

func TestDeviceMgmtGetDeviceStatusComposesChannelRecordAndResolvedAlarmFact(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbDeviceControlState{},
		&gbmodels.GbAlarmResource{}, &gbmodels.GbAlarmResourceParent{}, &gbmodels.GbAlarmBinding{},
	))
	device := &gbmodels.GbDevice{DeviceID: "D", Status: gbmodels.DeviceStatusOnline}
	require.NoError(t, db.Create(device).Error)
	channel := &gbmodels.GbChannel{DeviceID: "D", ChannelID: "C", Status: gbmodels.ChannelStatusOnline}
	require.NoError(t, db.Create(channel).Error)
	alarm := &gbmodels.GbAlarmResource{
		OwnerDeptID: 1, DeviceID: device.ID, DeviceCode: "D", AlarmCode: "A",
		ResourceType: gbmodels.AlarmResourceInput, TypeCode: "134", Name: "门磁",
	}
	require.NoError(t, db.Create(alarm).Error)
	require.NoError(t, db.Create(&gbmodels.GbAlarmResourceParent{AlarmResourceID: alarm.ID, ParentCode: "C"}).Error)
	now := time.Now()
	require.NoError(t, db.Create(&gbmodels.GbDeviceControlState{
		DeviceID: device.ID, ChannelID: channel.ID, TargetScope: gbmodels.ControlTargetScopeChannel, TargetCode: "C",
		RecordState: gbmodels.ControlStateOn, GuardState: gbmodels.ControlStateUnknown, Freshness: gbmodels.ControlStateFresh, ObservedAt: now,
	}).Error)
	require.NoError(t, db.Create(&gbmodels.GbDeviceControlState{
		DeviceID: device.ID, TargetScope: gbmodels.ControlTargetScopeAlarm, TargetCode: "A",
		RecordState: gbmodels.ControlStateUnknown, GuardState: gbmodels.ControlStateAlarm, Freshness: gbmodels.ControlStateFresh, ObservedAt: now,
	}).Error)

	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	router := gin.New()
	router.GET("/channel/:id/device-status", controller.GetDeviceStatus)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/channel/"+uintStr(channel.ID)+"/device-status", nil))

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"recordState":"on"`)
	require.Contains(t, response.Body.String(), `"guardState":"alarm"`)
	require.Contains(t, response.Body.String(), `"status":"resolved"`)
	require.Contains(t, response.Body.String(), `"targetCode":"A"`)
	require.Contains(t, response.Body.String(), `"completeness":"complete"`)
}

func TestDeviceMgmtGetDeviceStatusReportsAmbiguousAlarmTargets(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbDeviceControlState{},
		&gbmodels.GbAlarmResource{}, &gbmodels.GbAlarmResourceParent{}, &gbmodels.GbAlarmBinding{},
	))
	device := &gbmodels.GbDevice{DeviceID: "D", Status: gbmodels.DeviceStatusOnline}
	require.NoError(t, db.Create(device).Error)
	channel := &gbmodels.GbChannel{DeviceID: "D", ChannelID: "C", Status: gbmodels.ChannelStatusOnline}
	require.NoError(t, db.Create(channel).Error)
	now := time.Now()
	for index, code := range []string{"A1", "A2"} {
		alarm := &gbmodels.GbAlarmResource{
			OwnerDeptID: 1, DeviceID: device.ID, DeviceCode: "D", AlarmCode: code,
			ResourceType: gbmodels.AlarmResourceInput, TypeCode: "134", Name: code,
		}
		require.NoError(t, db.Create(alarm).Error)
		require.NoError(t, db.Create(&gbmodels.GbAlarmResourceParent{AlarmResourceID: alarm.ID, ParentCode: "C"}).Error)
		guardState := gbmodels.ControlStateOn
		if index == 1 {
			guardState = gbmodels.ControlStateAlarm
		}
		require.NoError(t, db.Create(&gbmodels.GbDeviceControlState{
			DeviceID: device.ID, TargetScope: gbmodels.ControlTargetScopeAlarm, TargetCode: code,
			RecordState: gbmodels.ControlStateUnknown, GuardState: guardState, Freshness: gbmodels.ControlStateFresh, ObservedAt: now,
		}).Error)
	}

	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	router := gin.New()
	router.GET("/channel/:id/device-status", controller.GetDeviceStatus)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/channel/"+uintStr(channel.ID)+"/device-status", nil))

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"status":"ambiguous"`)
	require.Contains(t, response.Body.String(), `"code":"A1"`)
	require.Contains(t, response.Body.String(), `"code":"A2"`)
	require.Contains(t, response.Body.String(), `"guardState":"unknown"`)
	require.Contains(t, response.Body.String(), `"completeness":"partial"`)
	require.Contains(t, response.Body.String(), `"alarmFacts"`)
	require.Contains(t, response.Body.String(), `"guardState":"on"`)
	require.Contains(t, response.Body.String(), `"guardState":"alarm"`)
}

func TestDeviceMgmtGetDeviceStatusUsesUnknownFreshnessWhenResolvedAlarmHasNoFact(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbDeviceControlState{},
		&gbmodels.GbAlarmResource{}, &gbmodels.GbAlarmResourceParent{}, &gbmodels.GbAlarmBinding{},
	))
	device := &gbmodels.GbDevice{DeviceID: "D", Status: gbmodels.DeviceStatusOnline}
	require.NoError(t, db.Create(device).Error)
	channel := &gbmodels.GbChannel{DeviceID: "D", ChannelID: "C", Status: gbmodels.ChannelStatusOnline}
	require.NoError(t, db.Create(channel).Error)
	alarm := &gbmodels.GbAlarmResource{
		OwnerDeptID: 1, DeviceID: device.ID, DeviceCode: "D", AlarmCode: "A",
		ResourceType: gbmodels.AlarmResourceInput, TypeCode: "134", Name: "门磁",
	}
	require.NoError(t, db.Create(alarm).Error)
	require.NoError(t, db.Create(&gbmodels.GbAlarmResourceParent{AlarmResourceID: alarm.ID, ParentCode: "C"}).Error)

	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	router := gin.New()
	router.GET("/channel/:id/device-status", controller.GetDeviceStatus)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/channel/"+uintStr(channel.ID)+"/device-status", nil))

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var envelope struct {
		Data struct {
			AlarmResolution struct {
				Freshness string `json:"freshness"`
			} `json:"alarmResolution"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.Equal(t, string(gbmodels.ControlStateUnknownFreshness), envelope.Data.AlarmResolution.Freshness)
}
