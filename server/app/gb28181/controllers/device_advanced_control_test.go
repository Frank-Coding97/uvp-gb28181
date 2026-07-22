package controllers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDeviceControlCapabilitiesKeepUnreportedAdvancedControlsUnknown(t *testing.T) {
	controller, _, channel, sender := newPTZResourceController(t)
	router := gin.New()
	router.Use(gin.Recovery())
	router.GET("/channel/:id/control-capabilities", controller.GetControlCapabilities)
	router.POST("/channel/:id/device-control", controller.ControlDevice)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/channel/"+uintStr(channel.ID)+"/control-capabilities", nil))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"iFrame":{"state":"unknown"`)

	response = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/channel/"+uintStr(channel.ID)+"/device-control", strings.NewReader(`{"action":"iframe","idempotencyKey":"unknown"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	require.Empty(t, sender.bodies, "unknown capability must not send SIP")
}

func TestDeviceControlExecutesSupportedActionsAndReturnsTrackedOperation(t *testing.T) {
	controller, db, channel, sender := newPTZResourceController(t)
	caps := `{"iframe":true,"recording":true,"guard":true,"alarm_reset":true,"teleboot":true,"drag_zoom":true}`
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
	caps := `{"teleboot":true}`
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
