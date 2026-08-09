package controllers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/subscribe"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

type serviceConfigTestYAML struct {
	values  map[string]interface{}
	saveErr error
	saveNum int
}

func (c *serviceConfigTestYAML) ConfigFileChangeListen(...func()) {}
func (c *serviceConfigTestYAML) Get(key string) interface{}       { return c.values[key] }
func (c *serviceConfigTestYAML) GetString(string) string          { return "" }
func (c *serviceConfigTestYAML) GetBool(key string) bool {
	value, _ := c.values[key].(bool)
	return value
}
func (c *serviceConfigTestYAML) GetInt(key string) int {
	value, _ := c.values[key].(int)
	return value
}
func (c *serviceConfigTestYAML) GetInt32(string) int32             { return 0 }
func (c *serviceConfigTestYAML) GetInt64(string) int64             { return 0 }
func (c *serviceConfigTestYAML) GetFloat64(string) float64         { return 0 }
func (c *serviceConfigTestYAML) GetDuration(string) time.Duration  { return 0 }
func (c *serviceConfigTestYAML) GetStringSlice(string) []string    { return nil }
func (c *serviceConfigTestYAML) GetUintSlice(string) []uint        { return nil }
func (c *serviceConfigTestYAML) Set(key string, value interface{}) { c.values[key] = value }
func (c *serviceConfigTestYAML) SaveConfig() error                 { c.saveNum++; return c.saveErr }

func newServiceConfigRouter(controller *ServiceConfigController) *gin.Engine {
	gin.SetMode(gin.TestMode)
	app.Response = response.NewResponseHandler()
	router := gin.New()
	router.GET("/position-history", controller.GetPositionHistory)
	router.PUT("/position-history", controller.UpdatePositionHistory)
	router.GET("/sdp-extension", controller.GetSDPExtension)
	router.PUT("/sdp-extension", controller.UpdateSDPExtension)
	router.GET("/sync-channels-on-online", controller.GetSyncChannelsOnOnline)
	router.PUT("/sync-channels-on-online", controller.UpdateSyncChannelsOnOnline)
	router.GET("/online-on-heartbeat", controller.GetOnlineOnHeartbeat)
	router.PUT("/online-on-heartbeat", controller.UpdateOnlineOnHeartbeat)
	router.GET("/save-alarm-messages", controller.GetSaveAlarmMessages)
	router.PUT("/save-alarm-messages", controller.UpdateSaveAlarmMessages)
	router.GET("/sip-command-timeout", controller.GetSIPCommandTimeout)
	router.PUT("/sip-command-timeout", controller.UpdateSIPCommandTimeout)
	router.GET("/preallocation-mode", controller.GetPreallocationMode)
	router.PUT("/preallocation-mode", controller.UpdatePreallocationMode)
	router.GET("/ignore-channel-offline-status-notify", controller.GetIgnoreChannelOfflineStatusNotify)
	router.PUT("/ignore-channel-offline-status-notify", controller.UpdateIgnoreChannelOfflineStatusNotify)
	router.GET("/ptz-default-speed", controller.GetPTZDefaultSpeed)
	router.PUT("/ptz-default-speed", controller.UpdatePTZDefaultSpeed)
	router.GET("/sip-log", controller.GetSIPLog)
	router.PUT("/sip-log", controller.UpdateSIPLog)
	return router
}

func TestServiceConfigController_SIPLogDefaultsToDisabled(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	app.ConfigYml = nil

	recorder := httptest.NewRecorder()
	newServiceConfigRouter(NewServiceConfigController()).ServeHTTP(
		recorder, httptest.NewRequest(http.MethodGet, "/sip-log", nil),
	)

	require.Equal(t, http.StatusOK, recorder.Code)
	data := serviceConfigData(t, recorder)
	require.Equal(t, false, data["enabled"])
	require.Equal(t, true, data["applied"])
}

func TestServiceConfigController_SIPLogReportsRuntimeMismatch(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	app.ConfigYml = &serviceConfigTestYAML{values: map[string]interface{}{gbconfig.SIPTraceEnabledConfigKey: true}}
	controller := NewServiceConfigController()
	controller.SetSIPTraceRuntimeProvider(func() bool { return false })

	recorder := httptest.NewRecorder()
	newServiceConfigRouter(controller).ServeHTTP(
		recorder, httptest.NewRequest(http.MethodGet, "/sip-log", nil),
	)

	require.Equal(t, http.StatusOK, recorder.Code)
	data := serviceConfigData(t, recorder)
	require.Equal(t, true, data["enabled"])
	require.Equal(t, false, data["applied"])
}

func TestServiceConfigController_UpdateSIPLogPersistsAndReloads(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{gbconfig.SIPTraceEnabledConfigKey: false}}
	app.ConfigYml = config
	reloads := 0
	controller := NewServiceConfigController()
	controller.SetSIPTraceReloader(func() error { reloads++; return nil })

	recorder := httptest.NewRecorder()
	newServiceConfigRouter(controller).ServeHTTP(
		recorder, httptest.NewRequest(http.MethodPut, "/sip-log", jsonBody(t, map[string]bool{"enabled": true})),
	)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, true, config.values[gbconfig.SIPTraceEnabledConfigKey])
	require.Equal(t, 1, config.saveNum)
	require.Equal(t, 1, reloads)
	require.Equal(t, true, serviceConfigData(t, recorder)["applied"])
}

func TestServiceConfigController_UpdateSIPLogReportsReloadFailure(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{gbconfig.SIPTraceEnabledConfigKey: false}}
	app.ConfigYml = config
	controller := NewServiceConfigController()
	controller.SetSIPTraceReloader(func() error { return errors.New("reload failed") })

	recorder := httptest.NewRecorder()
	newServiceConfigRouter(controller).ServeHTTP(
		recorder, httptest.NewRequest(http.MethodPut, "/sip-log", jsonBody(t, map[string]bool{"enabled": true})),
	)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	data := serviceConfigData(t, recorder)
	require.Equal(t, true, data["enabled"])
	require.Equal(t, false, data["applied"])
	require.Equal(t, "reload failed", data["applyError"])
}

func TestServiceConfigController_UpdateSIPLogRollsBackOnSaveFailure(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{
		values:  map[string]interface{}{gbconfig.SIPTraceEnabledConfigKey: false},
		saveErr: errors.New("write failed"),
	}
	app.ConfigYml = config

	recorder := httptest.NewRecorder()
	newServiceConfigRouter(NewServiceConfigController()).ServeHTTP(
		recorder, httptest.NewRequest(http.MethodPut, "/sip-log", jsonBody(t, map[string]bool{"enabled": true})),
	)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Equal(t, false, config.values[gbconfig.SIPTraceEnabledConfigKey])
}

func TestServiceConfigController_SyncChannelsOnOnlineDefaultsToEnabled(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	app.ConfigYml = nil

	recorder := httptest.NewRecorder()
	newServiceConfigRouter(NewServiceConfigController()).ServeHTTP(
		recorder, httptest.NewRequest(http.MethodGet, "/sync-channels-on-online", nil),
	)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, true, serviceConfigData(t, recorder)["enabled"])
}

func TestServiceConfigController_UpdateSyncChannelsOnOnlinePersistsAndRollsBack(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{gbconfig.SyncChannelsOnOnlineConfigKey: true}}
	app.ConfigYml = config
	router := newServiceConfigRouter(NewServiceConfigController())

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/sync-channels-on-online", jsonBody(t, map[string]bool{"enabled": false})))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, false, config.values[gbconfig.SyncChannelsOnOnlineConfigKey])
	require.Equal(t, 1, config.saveNum)

	config.saveErr = errors.New("write failed")
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/sync-channels-on-online", jsonBody(t, map[string]bool{"enabled": true})))
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Equal(t, false, config.values[gbconfig.SyncChannelsOnOnlineConfigKey])
}

func TestServiceConfigController_OnlineOnHeartbeatDefaultsToEnabled(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	app.ConfigYml = nil

	recorder := httptest.NewRecorder()
	newServiceConfigRouter(NewServiceConfigController()).ServeHTTP(
		recorder, httptest.NewRequest(http.MethodGet, "/online-on-heartbeat", nil),
	)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, true, serviceConfigData(t, recorder)["enabled"])
}

func TestServiceConfigController_UpdateOnlineOnHeartbeatPersistsAndRollsBack(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{gbconfig.OnlineOnHeartbeatConfigKey: true}}
	app.ConfigYml = config
	router := newServiceConfigRouter(NewServiceConfigController())

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/online-on-heartbeat", jsonBody(t, map[string]bool{"enabled": false})))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, false, config.values[gbconfig.OnlineOnHeartbeatConfigKey])

	config.saveErr = errors.New("write failed")
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/online-on-heartbeat", jsonBody(t, map[string]bool{"enabled": true})))
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Equal(t, false, config.values[gbconfig.OnlineOnHeartbeatConfigKey])
}

func TestServiceConfigController_SaveAlarmMessagesDefaultsToEnabled(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	app.ConfigYml = nil

	recorder := httptest.NewRecorder()
	newServiceConfigRouter(NewServiceConfigController()).ServeHTTP(
		recorder, httptest.NewRequest(http.MethodGet, "/save-alarm-messages", nil),
	)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, true, serviceConfigData(t, recorder)["enabled"])
}

func TestServiceConfigController_UpdateSaveAlarmMessagesPersistsAndRollsBack(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{gbconfig.SaveAlarmMessagesConfigKey: true}}
	app.ConfigYml = config
	router := newServiceConfigRouter(NewServiceConfigController())

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/save-alarm-messages", jsonBody(t, map[string]bool{"enabled": false})))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, false, config.values[gbconfig.SaveAlarmMessagesConfigKey])

	config.saveErr = errors.New("write failed")
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/save-alarm-messages", jsonBody(t, map[string]bool{"enabled": true})))
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Equal(t, false, config.values[gbconfig.SaveAlarmMessagesConfigKey])
}

func TestServiceConfigController_SIPCommandTimeoutDefaultsToTen(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	app.ConfigYml = nil

	recorder := httptest.NewRecorder()
	newServiceConfigRouter(NewServiceConfigController()).ServeHTTP(
		recorder, httptest.NewRequest(http.MethodGet, "/sip-command-timeout", nil),
	)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, float64(10), serviceConfigData(t, recorder)["timeoutSec"])
}

func TestServiceConfigController_UpdateSIPCommandTimeoutValidatesAndRollsBack(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{gbconfig.SIPCommandTimeoutSecConfigKey: 10}}
	app.ConfigYml = config
	router := newServiceConfigRouter(NewServiceConfigController())

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/sip-command-timeout", jsonBody(t, map[string]int{"timeoutSec": 30})))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 30, config.values[gbconfig.SIPCommandTimeoutSecConfigKey])

	for _, invalid := range []int{0, 301} {
		recorder = httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/sip-command-timeout", jsonBody(t, map[string]int{"timeoutSec": invalid})))
		require.Equal(t, http.StatusBadRequest, recorder.Code)
	}

	config.saveErr = errors.New("write failed")
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/sip-command-timeout", jsonBody(t, map[string]int{"timeoutSec": 20})))
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Equal(t, 30, config.values[gbconfig.SIPCommandTimeoutSecConfigKey])
}

func TestServiceConfigController_PreallocationModeDefaultsToDisabled(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	app.ConfigYml = nil

	recorder := httptest.NewRecorder()
	newServiceConfigRouter(NewServiceConfigController()).ServeHTTP(
		recorder, httptest.NewRequest(http.MethodGet, "/preallocation-mode", nil),
	)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, false, serviceConfigData(t, recorder)["enabled"])
}

func TestServiceConfigController_UpdatePreallocationModePersistsAndRollsBack(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{gbconfig.PreallocationModeConfigKey: false}}
	app.ConfigYml = config
	router := newServiceConfigRouter(NewServiceConfigController())

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/preallocation-mode", jsonBody(t, map[string]bool{"enabled": true})))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, true, config.values[gbconfig.PreallocationModeConfigKey])

	config.saveErr = errors.New("write failed")
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/preallocation-mode", jsonBody(t, map[string]bool{"enabled": false})))
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Equal(t, true, config.values[gbconfig.PreallocationModeConfigKey])
}

func TestServiceConfigController_IgnoreChannelOfflineStatusNotifyDefaultsToDisabled(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	app.ConfigYml = nil

	recorder := httptest.NewRecorder()
	newServiceConfigRouter(NewServiceConfigController()).ServeHTTP(
		recorder, httptest.NewRequest(http.MethodGet, "/ignore-channel-offline-status-notify", nil),
	)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, false, serviceConfigData(t, recorder)["enabled"])
}

func TestServiceConfigController_UpdateIgnoreChannelOfflineStatusNotifyPersistsAndRollsBack(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{gbconfig.IgnoreChannelOfflineStatusNotifyConfigKey: false}}
	app.ConfigYml = config
	router := newServiceConfigRouter(NewServiceConfigController())

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/ignore-channel-offline-status-notify", jsonBody(t, map[string]any{"enabled": true})))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, true, config.values[gbconfig.IgnoreChannelOfflineStatusNotifyConfigKey])

	config.saveErr = errors.New("disk full")
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/ignore-channel-offline-status-notify", jsonBody(t, map[string]any{"enabled": false})))
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Equal(t, true, config.values[gbconfig.IgnoreChannelOfflineStatusNotifyConfigKey])
}

func TestServiceConfigController_PTZDefaultSpeedDefaultsToSix(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	app.ConfigYml = nil

	recorder := httptest.NewRecorder()
	newServiceConfigRouter(NewServiceConfigController()).ServeHTTP(
		recorder, httptest.NewRequest(http.MethodGet, "/ptz-default-speed", nil),
	)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, float64(6), serviceConfigData(t, recorder)["level"])
}

func TestServiceConfigController_UpdatePTZDefaultSpeedPersistsBoundaries(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{ptzDefaultSpeedLevelConfigKey: 6}}
	app.ConfigYml = config
	router := newServiceConfigRouter(NewServiceConfigController())

	for _, level := range []int{1, 10} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(
			http.MethodPut, "/ptz-default-speed", jsonBody(t, map[string]int{"level": level}),
		))

		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.Equal(t, level, config.values[ptzDefaultSpeedLevelConfigKey])
		require.Equal(t, float64(level), serviceConfigData(t, recorder)["level"])
	}
	require.Equal(t, 2, config.saveNum)
}

func TestServiceConfigController_UpdatePTZDefaultSpeedRejectsInvalidLevel(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{ptzDefaultSpeedLevelConfigKey: 6}}
	app.ConfigYml = config
	router := newServiceConfigRouter(NewServiceConfigController())

	for _, body := range []map[string]int{{"level": 0}, {"level": 11}, {}} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/ptz-default-speed", jsonBody(t, body)))
		require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
	require.Equal(t, 0, config.saveNum)
	require.Equal(t, 6, config.values[ptzDefaultSpeedLevelConfigKey])
}

func TestServiceConfigController_UpdatePTZDefaultSpeedRollsBackOnSaveError(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{
		values:  map[string]interface{}{ptzDefaultSpeedLevelConfigKey: 6},
		saveErr: errors.New("write failed"),
	}
	app.ConfigYml = config

	recorder := httptest.NewRecorder()
	newServiceConfigRouter(NewServiceConfigController()).ServeHTTP(
		recorder, httptest.NewRequest(http.MethodPut, "/ptz-default-speed", jsonBody(t, map[string]int{"level": 10})),
	)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Equal(t, 6, config.values[ptzDefaultSpeedLevelConfigKey])
}

func TestServiceConfigController_SDPExtensionDefaultsToDisabled(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	app.ConfigYml = nil

	recorder := httptest.NewRecorder()
	newServiceConfigRouter(NewServiceConfigController()).ServeHTTP(
		recorder, httptest.NewRequest(http.MethodGet, "/sdp-extension", nil),
	)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, false, serviceConfigData(t, recorder)["enabled"])
}

func TestServiceConfigController_UpdateSDPExtensionPersistsToggle(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{sdpExtensionConfigKey: false}}
	app.ConfigYml = config
	router := newServiceConfigRouter(NewServiceConfigController())

	for _, enabled := range []bool{true, false} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(
			http.MethodPut, "/sdp-extension", jsonBody(t, map[string]bool{"enabled": enabled}),
		))

		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.Equal(t, enabled, config.values[sdpExtensionConfigKey])
		require.Equal(t, enabled, serviceConfigData(t, recorder)["enabled"])
	}
	require.Equal(t, 2, config.saveNum)
}

func TestServiceConfigController_UpdateSDPExtensionRollsBackOnSaveError(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{
		values:  map[string]interface{}{sdpExtensionConfigKey: false},
		saveErr: errors.New("write failed"),
	}
	app.ConfigYml = config

	recorder := httptest.NewRecorder()
	newServiceConfigRouter(NewServiceConfigController()).ServeHTTP(
		recorder, httptest.NewRequest(http.MethodPut, "/sdp-extension", jsonBody(t, map[string]bool{"enabled": true})),
	)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Equal(t, false, config.values[sdpExtensionConfigKey])
}

func serviceConfigData(t *testing.T, recorder *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var envelope struct {
		Data map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	return envelope.Data
}

func TestServiceConfigController_GetPositionHistoryDefaultsToEnabled(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	app.ConfigYml = nil

	recorder := httptest.NewRecorder()
	newServiceConfigRouter(NewServiceConfigController()).ServeHTTP(
		recorder, httptest.NewRequest(http.MethodGet, "/position-history", nil),
	)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, true, serviceConfigData(t, recorder)["enabled"])
	require.Equal(t, float64(7), serviceConfigData(t, recorder)["retentionDays"])
}

func TestServiceConfigController_UpdatePositionHistoryPersistsToggle(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{
		positionHistoryConfigKey:              true,
		positionHistoryRetentionDaysConfigKey: 7,
	}}
	app.ConfigYml = config
	router := newServiceConfigRouter(NewServiceConfigController())

	for _, enabled := range []bool{false, true} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPut, "/position-history", jsonBody(t, map[string]interface{}{
			"enabled":       enabled,
			"retentionDays": 30,
		}))
		router.ServeHTTP(recorder, request)

		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.Equal(t, enabled, config.values[positionHistoryConfigKey])
		require.Equal(t, 30, config.values[positionHistoryRetentionDaysConfigKey])
		require.Equal(t, enabled, serviceConfigData(t, recorder)["enabled"])
		require.Equal(t, float64(30), serviceConfigData(t, recorder)["retentionDays"])
		require.Equal(t, enabled, subscribe.PositionHistoryEnabled(), "位置处理器应读取最新配置值")
	}
	require.Equal(t, 2, config.saveNum)
}

func TestServiceConfigController_UpdatePositionHistoryAcceptsLegacyToggleOnly(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{
		positionHistoryConfigKey:              true,
		positionHistoryRetentionDaysConfigKey: 14,
	}}
	app.ConfigYml = config

	recorder := httptest.NewRecorder()
	newServiceConfigRouter(NewServiceConfigController()).ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodPut, "/position-history", jsonBody(t, map[string]bool{"enabled": false})),
	)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, false, config.values[positionHistoryConfigKey])
	require.Equal(t, 14, config.values[positionHistoryRetentionDaysConfigKey])
	require.Equal(t, float64(14), serviceConfigData(t, recorder)["retentionDays"])
}

func TestServiceConfigController_UpdatePositionHistoryRejectsInvalidRetentionDays(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{
		positionHistoryConfigKey:              true,
		positionHistoryRetentionDaysConfigKey: 7,
	}}
	app.ConfigYml = config

	recorder := httptest.NewRecorder()
	newServiceConfigRouter(NewServiceConfigController()).ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodPut, "/position-history", jsonBody(t, map[string]interface{}{
			"enabled":       true,
			"retentionDays": 366,
		})),
	)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, 0, config.saveNum)
	require.Equal(t, 7, config.values[positionHistoryRetentionDaysConfigKey])
}

func TestServiceConfigController_UpdatePositionHistoryRejectsMissingValue(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{}}
	app.ConfigYml = config

	recorder := httptest.NewRecorder()
	newServiceConfigRouter(NewServiceConfigController()).ServeHTTP(
		recorder, httptest.NewRequest(http.MethodPut, "/position-history", jsonBody(t, map[string]string{})),
	)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, 0, config.saveNum)
}

func TestServiceConfigController_UpdatePositionHistoryReturnsSaveError(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{
		positionHistoryRetentionDaysConfigKey: 7,
	}, saveErr: errors.New("write failed")}
	app.ConfigYml = config

	recorder := httptest.NewRecorder()
	newServiceConfigRouter(NewServiceConfigController()).ServeHTTP(
		recorder, httptest.NewRequest(http.MethodPut, "/position-history", jsonBody(t, map[string]bool{"enabled": false})),
	)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Equal(t, 1, config.saveNum)
	require.Equal(t, true, config.values[positionHistoryConfigKey], "保存失败时不能留下未持久化的运行时开关")
	require.Equal(t, 7, config.values[positionHistoryRetentionDaysConfigKey], "保存失败时不能留下未持久化的保留天数")
}

func jsonBody(t *testing.T, value interface{}) *bytes.Reader {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return bytes.NewReader(encoded)
}
