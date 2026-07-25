package controllers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestDeviceControlCapabilitiesAreInformationalAndDoNotBlockDispatch(t *testing.T) {
	tests := []struct {
		name      string
		caps      *string
		wantState string
	}{
		{name: "unreported", wantState: "unknown"},
		{name: "reported false", caps: stringPtr(`{"iframe":false}`), wantState: "unsupported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller, db, channel, sender := newPTZResourceController(t)
			if tt.caps != nil {
				require.NoError(t, db.Model(channel).Update("capabilities", *tt.caps).Error)
			}
			router := gin.New()
			router.Use(gin.Recovery())
			router.GET("/channel/:id/control-capabilities", controller.GetControlCapabilities)
			router.POST("/channel/:id/device-control", controller.ControlDevice)

			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/channel/"+uintStr(channel.ID)+"/control-capabilities", nil))
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.Contains(t, response.Body.String(), `"iFrame":{"state":"`+tt.wantState+`"`)

			response = httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/device-control", strings.NewReader(`{"action":"iframe","idempotencyKey":"`+tt.name+`"}`))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(response, request)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.Len(t, sender.bodies, 1)
			require.Contains(t, sender.bodies[0], "<IFameCmd>Send</IFameCmd>")
		})
	}
}

func stringPtr(value string) *string { return &value }

func seedAdvancedAlarmTarget(t *testing.T, db *gorm.DB, channel *gbmodels.GbChannel, code string) *gbmodels.GbAlarmResource {
	t.Helper()
	var device gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", channel.DeviceID).First(&device).Error)
	alarm := &gbmodels.GbAlarmResource{
		DeviceID: device.ID, DeviceCode: device.DeviceID, AlarmCode: code,
		ResourceType: gbmodels.AlarmResourceInput, TypeCode: "134", Name: code,
	}
	require.NoError(t, db.Create(alarm).Error)
	require.NoError(t, db.Create(&gbmodels.GbAlarmResourceParent{
		AlarmResourceID: alarm.ID,
		ParentCode:      channel.ChannelID,
	}).Error)
	return alarm
}

func TestDeviceControlExecutesWhitelistedActionsDespiteUnsupportedCapabilityMetadata(t *testing.T) {
	controller, db, channel, sender := newPTZResourceController(t)
	seedAdvancedAlarmTarget(t, db, channel, "A")
	caps := `{"iframe":false,"recording":false,"guard":false,"alarm_reset":false,"teleboot":false,"drag_zoom":false}`
	require.NoError(t, db.Model(channel).Updates(map[string]interface{}{"capabilities": caps, "ptz_type": 0}).Error)
	router := gin.New()
	router.Use(gin.Recovery())
	router.POST("/channel/:id/device-control", controller.ControlDevice)

	tests := []struct {
		body             string
		xml              string
		responseRequired bool
	}{
		{`{"action":"iframe","idempotencyKey":"a"}`, "<IFameCmd>Send</IFameCmd>", false},
		{`{"action":"record_start","idempotencyKey":"b"}`, "<RecordCmd>Record</RecordCmd>", true},
		{`{"action":"guard_reset","idempotencyKey":"c"}`, "<GuardCmd>ResetGuard</GuardCmd>", true},
		{`{"action":"alarm_reset","idempotencyKey":"d"}`, "<AlarmCmd>ResetAlarm</AlarmCmd>", true},
		{`{"action":"drag_zoom_in","region":{"length":1920,"width":1080,"midPointX":960,"midPointY":540,"lengthX":640,"lengthY":360},"idempotencyKey":"e"}`, "<DragZoomIn>", false},
	}
	for _, item := range tests {
		sender.mu.Lock()
		beforeBodies := len(sender.bodies)
		sender.mu.Unlock()
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/device-control", strings.NewReader(item.body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var envelope struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
		require.NotEmpty(t, envelope.Data["operationId"])
		require.NotContains(t, response.Body.String(), "Call-ID")
		var operation gbmodels.GbPTZOperation
		require.NoError(t, db.Where("operation_id = ?", envelope.Data["operationId"]).First(&operation).Error)
		require.Equal(t, item.responseRequired, operation.ResponseRequired)
		if item.responseRequired {
			require.Equal(t, gbmodels.PTZOperationQueued, operation.Status, "业务应答命令应先进入 durable queue")
			sender.mu.Lock()
			require.Len(t, sender.bodies, beforeBodies, "业务应答命令由 scheduler 下发, controller 不应同步发送")
			sender.mu.Unlock()
			continue
		}
		sender.mu.Lock()
		bodies := append([]string(nil), sender.bodies[beforeBodies:]...)
		sender.mu.Unlock()
		require.Len(t, bodies, 1)
		require.Contains(t, bodies[0], item.xml)
	}
}

func TestDeviceControlTeleBootRequiresConfirmationAndDeduplicatesSixtySeconds(t *testing.T) {
	controller, db, channel, sender := newPTZResourceController(t)
	caps := `{"teleboot":false}`
	require.NoError(t, db.Model(channel).Update("capabilities", caps).Error)
	router := gin.New()
	router.Use(gin.Recovery())
	router.POST("/channel/:id/device-control", controller.ControlDevice)

	request := func(body string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/device-control", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, req)
		return response
	}
	request(`{"action":"teleboot","confirmed":false,"idempotencyKey":"no"}`)
	require.Empty(t, sender.bodies)
	first := request(`{"action":"teleboot","confirmed":true,"idempotencyKey":"boot-1"}`)
	second := request(`{"action":"teleboot","confirmed":true,"idempotencyKey":"boot-2"}`)
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	require.Equal(t, http.StatusOK, second.Code, second.Body.String())
	require.Len(t, sender.bodies, 1)
	require.Contains(t, sender.bodies[0], "<TeleBoot>Boot</TeleBoot>")
}

func TestDeviceControlRejectsOppositeActionWhileResourceOperationIsActive(t *testing.T) {
	controller, db, channel, sender := newPTZResourceController(t)
	seedAdvancedAlarmTarget(t, db, channel, "A")
	router := gin.New()
	router.Use(gin.Recovery())
	router.POST("/channel/:id/device-control", controller.ControlDevice)

	request := func(body string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/device-control", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, req)
		return response
	}

	first := request(`{"action":"record_start","idempotencyKey":"record-active"}`)
	require.Contains(t, first.Body.String(), `"code":0`)
	replay := request(`{"action":"record_start","idempotencyKey":"record-active"}`)
	require.Contains(t, replay.Body.String(), `"code":0`)
	require.Contains(t, replay.Body.String(), `"deduplicated":true`)
	second := request(`{"action":"record_stop","idempotencyKey":"record-opposite"}`)
	require.Contains(t, second.Body.String(), `"code":1`)
	require.Contains(t, second.Body.String(), "录像操作正在处理中")

	guardSet := request(`{"action":"guard_set","idempotencyKey":"guard-active"}`)
	require.Contains(t, guardSet.Body.String(), `"code":0`)
	guardReset := request(`{"action":"guard_reset","idempotencyKey":"guard-opposite"}`)
	require.Contains(t, guardReset.Body.String(), `"code":1`)
	require.Contains(t, guardReset.Body.String(), "布撤防操作正在处理中")

	iframe := request(`{"action":"iframe","idempotencyKey":"iframe-independent"}`)
	require.Contains(t, iframe.Body.String(), `"code":0`)
	sender.mu.Lock()
	require.Len(t, sender.bodies, 1, "活动录像 operation 不应阻塞无关的关键帧命令")
	sender.mu.Unlock()

	var recordCount int64
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Where("action IN ?", []string{"record_start", "record_stop"}).Count(&recordCount).Error)
	require.EqualValues(t, 1, recordCount)
	var guardCount int64
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Where("action IN ?", []string{"guard_set", "guard_reset"}).Count(&guardCount).Error)
	require.EqualValues(t, 1, guardCount)
}

func TestDeviceControlUsesResolvedAlarmInputAsGuardTarget(t *testing.T) {
	controller, db, channel, _ := newPTZResourceController(t)
	alarm := seedAdvancedAlarmTarget(t, db, channel, "A")
	router := gin.New()
	router.Use(gin.Recovery())
	router.POST("/channel/:id/device-control", controller.ControlDevice)

	request := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/device-control",
		strings.NewReader(`{"action":"guard_set","idempotencyKey":"resolved-alarm"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Contains(t, response.Body.String(), `"code":0`)
	require.Contains(t, response.Body.String(), `"targetScope":"alarm"`)
	require.Contains(t, response.Body.String(), `"targetCode":"`+alarm.AlarmCode+`"`)
	var operation gbmodels.GbPTZOperation
	require.NoError(t, db.Where("action = ?", "guard_set").First(&operation).Error)
	require.Equal(t, gbmodels.ControlTargetScopeAlarm, operation.TargetScope)
	require.Equal(t, alarm.AlarmCode, operation.TargetCode)
	require.Equal(t, gbmodels.ControlTargetScopeAlarm+":"+alarm.AlarmCode, operation.ScopeKey)
}

func TestDeviceControlFallsBackToRegisteredDeviceWhenAlarmTargetIsUnavailable(t *testing.T) {
	controller, db, channel, _ := newPTZResourceController(t)
	router := gin.New()
	router.Use(gin.Recovery())
	router.POST("/channel/:id/device-control", controller.ControlDevice)

	request := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/device-control",
		strings.NewReader(`{"action":"alarm_reset","idempotencyKey":"alarm-parent-fallback"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Contains(t, response.Body.String(), `"code":0`)
	require.Contains(t, response.Body.String(), `"targetScope":"alarm"`)
	require.Contains(t, response.Body.String(), `"targetCode":"`+channel.DeviceID+`"`)
	var operation gbmodels.GbPTZOperation
	require.NoError(t, db.Where("action = ?", "alarm_reset").First(&operation).Error)
	require.Equal(t, gbmodels.ControlTargetScopeAlarm, operation.TargetScope)
	require.Equal(t, channel.DeviceID, operation.TargetCode)
}

func TestDeviceControlRejectsAmbiguousAlarmTarget(t *testing.T) {
	tests := []struct {
		name        string
		seedTargets int
		wantMessage string
	}{
		{name: "ambiguous", seedTargets: 2, wantMessage: "关联多个报警输入"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller, db, channel, sender := newPTZResourceController(t)
			for index := 0; index < tt.seedTargets; index++ {
				seedAdvancedAlarmTarget(t, db, channel, "A"+uintStr(uint(index+1)))
			}
			router := gin.New()
			router.Use(gin.Recovery())
			router.POST("/channel/:id/device-control", controller.ControlDevice)

			request := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/device-control",
				strings.NewReader(`{"action":"alarm_reset","idempotencyKey":"alarm-target-`+tt.name+`"}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			require.Contains(t, response.Body.String(), `"code":1`)
			require.Contains(t, response.Body.String(), tt.wantMessage)
			var operationCount int64
			require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Count(&operationCount).Error)
			require.Zero(t, operationCount)
			sender.mu.Lock()
			require.Empty(t, sender.bodies)
			sender.mu.Unlock()
		})
	}
}

func TestDeviceControlAllowsOppositeActionAfterResourceOperationFinishes(t *testing.T) {
	controller, db, channel, _ := newPTZResourceController(t)
	router := gin.New()
	router.Use(gin.Recovery())
	router.POST("/channel/:id/device-control", controller.ControlDevice)

	request := func(body string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/device-control", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, req)
		return response
	}

	first := request(`{"action":"record_start","idempotencyKey":"record-finished"}`)
	require.Contains(t, first.Body.String(), `"code":0`)
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).
		Where("action = ?", "record_start").
		Update("status", gbmodels.PTZOperationAccepted).Error)

	second := request(`{"action":"record_stop","idempotencyKey":"record-after-finish"}`)
	require.Contains(t, second.Body.String(), `"code":0`)
}

func TestDeviceControlKeepsResourceLockedWhileTransportResultIsUnknown(t *testing.T) {
	controller, db, channel, _ := newPTZResourceController(t)
	router := gin.New()
	router.Use(gin.Recovery())
	router.POST("/channel/:id/device-control", controller.ControlDevice)

	request := func(body string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/device-control", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, req)
		return response
	}

	first := request(`{"action":"record_start","idempotencyKey":"record-transport-unknown"}`)
	require.Contains(t, first.Body.String(), `"code":0`)
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).
		Where("action = ?", "record_start").
		Updates(map[string]interface{}{
			"status":                gbmodels.PTZOperationUnknown,
			"transport_deadline_at": time.Now().Add(time.Minute),
		}).Error)

	blocked := request(`{"action":"record_stop","idempotencyKey":"record-opposite-before-transport-deadline"}`)
	require.Contains(t, blocked.Body.String(), `"code":1`)
	require.Contains(t, blocked.Body.String(), "录像操作正在处理中")

	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).
		Where("action = ?", "record_start").
		Update("transport_deadline_at", time.Now().Add(-time.Second)).Error)
	allowed := request(`{"action":"record_stop","idempotencyKey":"record-opposite-after-transport-deadline"}`)
	require.Contains(t, allowed.Body.String(), `"code":0`)
}

func TestDeviceControlRejectsInvalidActionWithoutDispatch(t *testing.T) {
	controller, db, channel, sender := newPTZResourceController(t)
	router := gin.New()
	router.Use(gin.Recovery())
	router.POST("/channel/:id/device-control", controller.ControlDevice)

	request := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/device-control",
		strings.NewReader(`{"action":"aux_on","idempotencyKey":"invalid-action"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Contains(t, response.Body.String(), `"code":1`)
	require.Contains(t, response.Body.String(), "动作不合法")
	sender.mu.Lock()
	bodies := append([]string(nil), sender.bodies...)
	sender.mu.Unlock()
	require.Empty(t, bodies, "非法动作必须在 SIP 下发前被白名单拒绝")
	var operationCount int64
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Count(&operationCount).Error)
	require.Zero(t, operationCount, "非法动作不得创建控制操作记录")
}

func TestDeviceControlRejectsOfflineDeviceWithoutDispatch(t *testing.T) {
	controller, db, channel, sender := newPTZResourceController(t)
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).
		Where("device_id = ?", channel.DeviceID).
		Update("status", gbmodels.DeviceStatusOffline).Error)
	router := gin.New()
	router.Use(gin.Recovery())
	router.POST("/channel/:id/device-control", controller.ControlDevice)

	request := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/device-control",
		strings.NewReader(`{"action":"iframe","idempotencyKey":"offline-device"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Contains(t, response.Body.String(), `"code":1`)
	require.Contains(t, response.Body.String(), "下发设备控制失败")
	sender.mu.Lock()
	bodies := append([]string(nil), sender.bodies...)
	sender.mu.Unlock()
	require.Empty(t, bodies, "离线设备不得下发 SIP 控制报文")
	var operationCount int64
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Count(&operationCount).Error)
	require.Zero(t, operationCount, "在线校验失败时不得创建控制操作记录")
}
