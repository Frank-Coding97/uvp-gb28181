package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

type maintenanceTrackedSender struct {
	calls  int
	result uac.TrackedMessageResult
	err    error
	body   []byte
}

func (s *maintenanceTrackedSender) SendMessageTracked(_ context.Context, _ string, _ string, _ string, body []byte) (uac.TrackedMessageResult, error) {
	s.calls++
	s.body = append([]byte(nil), body...)
	if s.result.StatusCode == 0 && !s.result.Attempted && s.err == nil {
		s.result = uac.TrackedMessageResult{StatusCode: 200, CallID: "maintenance-call", CSeq: "1"}
	}
	return s.result, s.err
}

type maintenanceFixture struct {
	db         *gorm.DB
	controller *gbcontrollers.DeviceMgmtController
	sender     *maintenanceTrackedSender
	device     *gbmodels.GbDevice
	channel    *gbmodels.GbChannel
}

func newMaintenanceFixture(t *testing.T, deviceStatus int8, channelStatus int8) maintenanceFixture {
	t.Helper()
	app.Response = response.NewResponseHandler()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbPTZOperation{}, &gbmodels.GbDeviceFirmwareUpgrade{},
		&basemodels.SysDepartment{}, &basemodels.SysRole{}, &basemodels.SysUserRole{}, &basemodels.User{},
	))
	device := &gbmodels.GbDevice{
		OwnerDeptID: 1, DeviceID: "34020000002000000001", Name: "测试设备", IP: "192.0.2.10", Port: 5060,
		Transport: "TCP", Status: deviceStatus, EffectiveVersion: gbmodels.ProtocolVersion2022,
	}
	require.NoError(t, db.Create(device).Error)
	channel := &gbmodels.GbChannel{OwnerDeptID: 1, DeviceID: device.DeviceID, ChannelID: "37011200001310000001", Status: channelStatus}
	require.NoError(t, db.Create(channel).Error)
	sender := &maintenanceTrackedSender{}
	controller := gbcontrollers.NewDeviceMgmtController()
	controller.SetDB(func() *gorm.DB { return db })
	service, err := ptz.NewService(db, sender, time.Now)
	require.NoError(t, err)
	controller.SetPTZService(service)
	return maintenanceFixture{db: db, controller: controller, sender: sender, device: device, channel: channel}
}

func maintenanceRouter(f maintenanceFixture, middleware ...gin.HandlerFunc) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	if len(middleware) > 0 {
		router.Use(middleware...)
	}
	router.POST("/device/:id/reboot", f.controller.RebootDevice)
	router.GET("/device/:id/maintenance-operations", f.controller.ListMaintenanceOperations)
	router.POST("/channel/:id/device-control", f.controller.ControlDevice)
	return router
}

func maintenanceClaims(userID uint) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: userID}})
		c.Next()
	}
}

func seedMaintenanceAdmin(t *testing.T, db *gorm.DB) {
	t.Helper()
	dept := &basemodels.SysDepartment{BaseModel: basemodels.BaseModel{ID: 1}, Name: "维护部门"}
	require.NoError(t, db.Create(dept).Error)
	user := &basemodels.User{BaseModel: basemodels.BaseModel{ID: 17}, Username: "maintainer", Password: "x", DeptID: 1}
	require.NoError(t, db.Create(user).Error)
	role := &basemodels.SysRole{Name: "maintenance-admin", DataScope: 1}
	require.NoError(t, db.Create(role).Error)
	require.NoError(t, db.Create(&basemodels.SysUserRole{UserID: user.ID, RoleID: role.ID}).Error)
}

func TestDeviceMaintenanceRebootAllowsOfflineChannelAndReturnsDeviceScopeResult(t *testing.T) {
	fixture := newMaintenanceFixture(t, gbmodels.DeviceStatusOnline, gbmodels.ChannelStatusOffline)
	seedMaintenanceAdmin(t, fixture.db)
	router := maintenanceRouter(fixture, maintenanceClaims(17))
	request := httptest.NewRequest(http.MethodPost, "/device/"+uintStr(fixture.device.ID)+"/reboot", strings.NewReader(`{"confirmed":true,"idempotencyKey":"maintenance-1"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var envelope struct {
		Data map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.Equal(t, "teleboot", envelope.Data["action"])
	require.Equal(t, "device", envelope.Data["targetScope"])
	require.Equal(t, fixture.device.DeviceID, envelope.Data["targetCode"])
	require.Equal(t, "sent", envelope.Data["status"])
	require.Equal(t, false, envelope.Data["responseRequired"])
	require.Equal(t, false, envelope.Data["deduplicated"])
	require.Contains(t, string(fixture.sender.body), "<DeviceID>"+fixture.device.DeviceID+"</DeviceID>")
	require.Contains(t, string(fixture.sender.body), "<TeleBoot>Boot</TeleBoot>")

	var operation gbmodels.GbPTZOperation
	require.NoError(t, fixture.db.Where("device_id = ? AND action = ?", fixture.device.ID, "teleboot").First(&operation).Error)
	require.Zero(t, operation.ChannelID)
	require.Empty(t, operation.ChannelCode)
}

func TestDeviceMaintenanceRebootRejectsUnconfirmedAndOfflineDevice(t *testing.T) {
	fixture := newMaintenanceFixture(t, gbmodels.DeviceStatusOffline, gbmodels.ChannelStatusOffline)
	seedMaintenanceAdmin(t, fixture.db)
	router := maintenanceRouter(fixture, maintenanceClaims(17))
	request := httptest.NewRequest(http.MethodPost, "/device/"+uintStr(fixture.device.ID)+"/reboot", strings.NewReader(`{"confirmed":false,"idempotencyKey":"no"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Contains(t, response.Body.String(), "确认")
	require.Zero(t, fixture.sender.calls)

	request = httptest.NewRequest(http.MethodPost, "/device/"+uintStr(fixture.device.ID)+"/reboot", strings.NewReader(`{"confirmed":true,"idempotencyKey":"offline"}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Contains(t, response.Body.String(), "下发设备重启失败")
	require.Zero(t, fixture.sender.calls)
}

func TestDeviceMaintenanceRebootSharesDedupWithLegacyChannelEntryAndRecordsActor(t *testing.T) {
	fixture := newMaintenanceFixture(t, gbmodels.DeviceStatusOnline, gbmodels.ChannelStatusOffline)
	previousConfig := app.ConfigYml
	app.ConfigYml = teleBootTestConfig{}
	t.Cleanup(func() { app.ConfigYml = previousConfig })
	dept := &basemodels.SysDepartment{BaseModel: basemodels.BaseModel{ID: 1}, Name: "维护部门"}
	require.NoError(t, fixture.db.Create(dept).Error)
	user := &basemodels.User{BaseModel: basemodels.BaseModel{ID: 17}, Username: "maintainer", Password: "x", DeptID: 1}
	require.NoError(t, fixture.db.Create(user).Error)
	role := &basemodels.SysRole{Name: "maintenance", DataScope: 3}
	require.NoError(t, fixture.db.Create(role).Error)
	require.NoError(t, fixture.db.Create(&basemodels.SysUserRole{UserID: user.ID, RoleID: role.ID}).Error)
	router := maintenanceRouter(fixture, maintenanceClaims(17))
	request := httptest.NewRequest(http.MethodPost, "/device/"+uintStr(fixture.device.ID)+"/reboot", strings.NewReader(`{"confirmed":true,"idempotencyKey":"shared"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	request = httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(fixture.channel.ID)+"/device-control", strings.NewReader(`{"action":"teleboot","confirmed":true,"idempotencyKey":"legacy-key"}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"deduplicated":true`)
	require.Equal(t, 1, fixture.sender.calls)

	var operation gbmodels.GbPTZOperation
	require.NoError(t, fixture.db.Where("device_id = ? AND action = ?", fixture.device.ID, "teleboot").First(&operation).Error)
	require.Equal(t, uint(17), operation.ActorID)
	require.Equal(t, uint(1), operation.ActorDeptID)
}

func TestDeviceMaintenanceOperationsArePagedPrivateAndReadableWhenOffline(t *testing.T) {
	fixture := newMaintenanceFixture(t, gbmodels.DeviceStatusOffline, gbmodels.ChannelStatusOffline)
	seedMaintenanceAdmin(t, fixture.db)
	now := time.Now().UTC()
	rows := []gbmodels.GbPTZOperation{
		{OperationID: "new", IdempotencyKey: "reboot:v1:new", DeviceID: fixture.device.ID, DeviceCode: fixture.device.DeviceID, ChannelID: 0, CmdType: "DeviceControl", Action: "teleboot", PayloadJSON: `{"secret":"do-not-return"}`, TargetScope: "device", TargetCode: fixture.device.DeviceID, Status: gbmodels.PTZOperationSent, SIPStatus: 200, ActorID: 17, CreatedAt: now.Add(-time.Minute), ResponseRequired: false},
		{OperationID: "old", IdempotencyKey: "legacy", DeviceID: fixture.device.ID, DeviceCode: fixture.device.DeviceID, ChannelID: fixture.channel.ID, ChannelCode: fixture.channel.ChannelID, CmdType: "DeviceControl", Action: "teleboot", PayloadJSON: `{"secret":"old-secret"}`, Status: gbmodels.PTZOperationAccepted, SIPStatus: 200, CreatedAt: now.Add(-2 * time.Minute)},
	}
	require.NoError(t, fixture.db.Create(&rows).Error)
	router := maintenanceRouter(fixture, maintenanceClaims(17))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/device/"+uintStr(fixture.device.ID)+"/maintenance-operations?page=1&pageSize=1", nil))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var envelope struct {
		Data struct {
			List     []map[string]interface{} `json:"list"`
			Total    int                      `json:"total"`
			Page     int                      `json:"page"`
			PageSize int                      `json:"pageSize"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.Equal(t, 2, envelope.Data.Total)
	require.Equal(t, 1, envelope.Data.Page)
	require.Equal(t, 1, envelope.Data.PageSize)
	require.Len(t, envelope.Data.List, 1)
	item := envelope.Data.List[0]
	for _, key := range []string{"operationId", "action", "status", "sipStatus", "errorMessage", "actorId", "createdAt", "sentAt", "completedAt", "responseRequired", "targetCode"} {
		_, ok := item[key]
		require.True(t, ok, key)
	}
	for _, forbidden := range []string{"payloadJson", "payloadJSON", "secret", "deviceCode", "channelCode", "callId", "cseq", "responseBody"} {
		_, ok := item[forbidden]
		require.False(t, ok, forbidden)
	}
	require.NotContains(t, response.Body.String(), "old-secret")
}

func TestDeviceMaintenanceRejectsHiddenDeviceForRebootAndHistory(t *testing.T) {
	fixture := newMaintenanceFixture(t, gbmodels.DeviceStatusOnline, gbmodels.ChannelStatusOffline)
	require.NoError(t, fixture.db.Model(fixture.device).Update("owner_dept_id", 2).Error)
	dept := &basemodels.SysDepartment{BaseModel: basemodels.BaseModel{ID: 1}, Name: "用户部门"}
	require.NoError(t, fixture.db.Create(dept).Error)
	user := &basemodels.User{BaseModel: basemodels.BaseModel{ID: 17}, Username: "limited", Password: "x", DeptID: 1}
	require.NoError(t, fixture.db.Create(user).Error)
	role := &basemodels.SysRole{Name: "limited", DataScope: 3}
	require.NoError(t, fixture.db.Create(role).Error)
	require.NoError(t, fixture.db.Create(&basemodels.SysUserRole{UserID: user.ID, RoleID: role.ID}).Error)
	router := maintenanceRouter(fixture, maintenanceClaims(17))
	for _, method := range []string{http.MethodPost, http.MethodGet} {
		path := "/device/" + uintStr(fixture.device.ID)
		if method == http.MethodPost {
			path += "/reboot"
		} else {
			path += "/maintenance-operations"
		}
		request := httptest.NewRequest(method, path, strings.NewReader(`{"confirmed":true,"idempotencyKey":"hidden"}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Contains(t, response.Body.String(), "不存在", method)
	}
	require.Zero(t, fixture.sender.calls)
}

func TestDeviceMaintenanceRejectsAnonymousRequests(t *testing.T) {
	fixture := newMaintenanceFixture(t, gbmodels.DeviceStatusOnline, gbmodels.ChannelStatusOffline)
	router := maintenanceRouter(fixture)

	request := httptest.NewRequest(http.MethodPost, "/device/"+uintStr(fixture.device.ID)+"/reboot", strings.NewReader(`{"confirmed":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Contains(t, response.Body.String(), "未登录")

	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/device/"+uintStr(fixture.device.ID)+"/maintenance-operations", nil))
	require.Contains(t, response.Body.String(), "未登录")
	require.Zero(t, fixture.sender.calls)
}
