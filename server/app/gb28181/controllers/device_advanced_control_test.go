package controllers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
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

func TestDeviceControlExecutesWhitelistedActionsDespiteUnsupportedCapabilityMetadata(t *testing.T) {
	controller, db, channel, sender := newPTZResourceController(t)
	caps := `{"iframe":false,"recording":false,"guard":false,"alarm_reset":false,"teleboot":false,"drag_zoom":false}`
	require.NoError(t, db.Model(channel).Updates(map[string]interface{}{"capabilities": caps, "ptz_type": 0}).Error)
	router := gin.New()
	router.Use(gin.Recovery())
	router.POST("/channel/:id/device-control", controller.ControlDevice)

	tests := []struct{ body, xml string }{
		{`{"action":"iframe","idempotencyKey":"a"}`, "<IFameCmd>Send</IFameCmd>"},
		{`{"action":"record_start","idempotencyKey":"b"}`, "<RecordCmd>Record</RecordCmd>"},
		{`{"action":"guard_reset","idempotencyKey":"c"}`, "<GuardCmd>ResetGuard</GuardCmd>"},
		{`{"action":"alarm_reset","alarmMethod":"4","alarmType":"1","idempotencyKey":"d"}`, "<AlarmCmd>ResetAlarm</AlarmCmd>"},
		{`{"action":"drag_zoom_in","region":{"length":1920,"width":1080,"midPointX":960,"midPointY":540,"lengthX":640,"lengthY":360},"idempotencyKey":"e"}`, "<DragZoomIn>"},
	}
	for i, item := range tests {
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
		require.Contains(t, sender.bodies[i], item.xml)
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
