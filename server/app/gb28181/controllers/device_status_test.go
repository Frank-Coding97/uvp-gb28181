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

// 这三个用例共用一套「一台设备 + 一路通道」的空库，报警目标资源由各用例自己决定加不加。
func newDeviceStatusFixture(t *testing.T) (*gorm.DB, *gbmodels.GbDevice, *gbmodels.GbChannel) {
	t.Helper()
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
	return db, device, channel
}

func serveDeviceStatus(t *testing.T, db *gorm.DB, channelID uint) *httptest.ResponseRecorder {
	t.Helper()
	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	router := gin.New()
	router.GET("/channel/:id/device-status", controller.GetDeviceStatus)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/channel/"+uintStr(channelID)+"/device-status", nil))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	return response
}

type deviceStatusEnvelope struct {
	Data struct {
		Completeness string `json:"completeness"`
		DeviceReport struct {
			Online           *string `json:"online"`
			SelfTest         *string `json:"selfTest"`
			Encode           *string `json:"encode"`
			DeviceTime       *string `json:"deviceTime"`
			ClockSkewSeconds *int64  `json:"clockSkewSeconds"`
			AlarmInputCount  *int    `json:"alarmInputCount"`
		} `json:"deviceReport"`
	} `json:"data"`
}

// 设备自报的四项事实必须原样出现在接口里 —— 以前它们被解析器丢掉，接口里根本没有。
func TestDeviceMgmtGetDeviceStatusExposesDeviceReportedFacts(t *testing.T) {
	db, device, channel := newDeviceStatusFixture(t)
	observedAt := time.Date(2026, 9, 19, 20, 3, 59, 0, time.Local)
	deviceTime := time.Date(2026, 9, 19, 20, 3, 58, 0, time.Local)
	online, selfTest, encode := gbmodels.DeviceOnlineStateOnline, gbmodels.DeviceSelfTestOK, gbmodels.ControlStateOn
	alarmCount := 0
	require.NoError(t, db.Create(&gbmodels.GbDeviceControlState{
		DeviceID: device.ID, ChannelID: channel.ID,
		TargetScope: gbmodels.ControlTargetScopeChannel, TargetCode: "C",
		RecordState: gbmodels.ControlStateOn, GuardState: gbmodels.ControlStateUnknown,
		Freshness: gbmodels.ControlStateFresh, ObservedAt: observedAt,
		OnlineState: &online, SelfTestState: &selfTest, EncodeState: &encode,
		DeviceTime: &deviceTime, AlarmInputCount: &alarmCount,
	}).Error)

	var envelope deviceStatusEnvelope
	require.NoError(t, json.Unmarshal(serveDeviceStatus(t, db, channel.ID).Body.Bytes(), &envelope))

	require.NotNil(t, envelope.Data.DeviceReport.Online)
	require.Equal(t, gbmodels.DeviceOnlineStateOnline, *envelope.Data.DeviceReport.Online)
	require.NotNil(t, envelope.Data.DeviceReport.SelfTest)
	require.Equal(t, gbmodels.DeviceSelfTestOK, *envelope.Data.DeviceReport.SelfTest)
	require.NotNil(t, envelope.Data.DeviceReport.Encode)
	require.Equal(t, gbmodels.ControlStateOn, *envelope.Data.DeviceReport.Encode)
	require.NotNil(t, envelope.Data.DeviceReport.ClockSkewSeconds)
	require.Equal(t, int64(1), *envelope.Data.DeviceReport.ClockSkewSeconds, "平台观测比设备自报晚 1 秒")
	require.NotNil(t, envelope.Data.DeviceReport.AlarmInputCount)
	require.Zero(t, *envelope.Data.DeviceReport.AlarmInputCount)
}

// 设备明确回了 Alarmstatus Num="0"、目录里也没有报警目标 ⇒ 报警事实本就是个空集，
// 不该再报 partial 让用户以为还有东西没查到。
func TestDeviceMgmtGetDeviceStatusTreatsDeclaredZeroAlarmInputsAsComplete(t *testing.T) {
	db, device, channel := newDeviceStatusFixture(t)
	alarmCount := 0
	require.NoError(t, db.Create(&gbmodels.GbDeviceControlState{
		DeviceID: device.ID, ChannelID: channel.ID,
		TargetScope: gbmodels.ControlTargetScopeChannel, TargetCode: "C",
		RecordState: gbmodels.ControlStateOn, GuardState: gbmodels.ControlStateUnknown,
		Freshness: gbmodels.ControlStateFresh, ObservedAt: time.Now(), AlarmInputCount: &alarmCount,
	}).Error)

	var envelope deviceStatusEnvelope
	require.NoError(t, json.Unmarshal(serveDeviceStatus(t, db, channel.ID).Body.Bytes(), &envelope))
	require.Equal(t, "complete", envelope.Data.Completeness)
}

// 对照组：同样解析不到报警目标，但设备**没有**声明报警输入数量 —— 这时仍然是未知，
// 不能因为「目录里没有」就替设备下结论说自己没有报警输入。
func TestDeviceMgmtGetDeviceStatusStaysPartialWhenAlarmInputsUnreported(t *testing.T) {
	db, device, channel := newDeviceStatusFixture(t)
	require.NoError(t, db.Create(&gbmodels.GbDeviceControlState{
		DeviceID: device.ID, ChannelID: channel.ID,
		TargetScope: gbmodels.ControlTargetScopeChannel, TargetCode: "C",
		RecordState: gbmodels.ControlStateOn, GuardState: gbmodels.ControlStateUnknown,
		Freshness: gbmodels.ControlStateFresh, ObservedAt: time.Now(),
	}).Error)

	var envelope deviceStatusEnvelope
	require.NoError(t, json.Unmarshal(serveDeviceStatus(t, db, channel.ID).Body.Bytes(), &envelope))
	require.Equal(t, "partial", envelope.Data.Completeness)
	require.Nil(t, envelope.Data.DeviceReport.AlarmInputCount, "设备没提数量时接口要给 null,不是 0")
	require.Nil(t, envelope.Data.DeviceReport.Online)
}
