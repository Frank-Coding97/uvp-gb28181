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

	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/utils/response"
)

type serviceConfigTestYAML struct {
	values  map[string]interface{}
	saveErr error
	saveNum int
	setHook func(string, interface{})
}

func (c *serviceConfigTestYAML) ConfigFileChangeListen(...func()) {}
func (c *serviceConfigTestYAML) Get(key string) interface{}       { return c.values[key] }
func (c *serviceConfigTestYAML) GetString(key string) string {
	value, _ := c.values[key].(string)
	return value
}
func (c *serviceConfigTestYAML) GetBool(key string) bool {
	value, _ := c.values[key].(bool)
	return value
}
func (c *serviceConfigTestYAML) GetInt(key string) int {
	value, _ := c.values[key].(int)
	return value
}
func (c *serviceConfigTestYAML) GetInt32(string) int32            { return 0 }
func (c *serviceConfigTestYAML) GetInt64(string) int64            { return 0 }
func (c *serviceConfigTestYAML) GetFloat64(string) float64        { return 0 }
func (c *serviceConfigTestYAML) GetDuration(string) time.Duration { return 0 }
func (c *serviceConfigTestYAML) GetStringSlice(key string) []string {
	value, _ := c.values[key].([]string)
	return value
}
func (c *serviceConfigTestYAML) GetUintSlice(string) []uint { return nil }
func (c *serviceConfigTestYAML) Set(key string, value interface{}) {
	c.values[key] = value
	if c.setHook != nil {
		c.setHook(key, value)
	}
}
func (c *serviceConfigTestYAML) SaveConfig() error { c.saveNum++; return c.saveErr }

func newServiceConfigRouter(controller *ServiceConfigController) *gin.Engine {
	gin.SetMode(gin.TestMode)
	app.Response = response.NewResponseHandler()
	router := gin.New()
	router.GET("/service-config", controller.GetServiceConfig)
	router.PUT("/service-config", controller.UpdateServiceConfig)
	return router
}

func TestServiceConfigController_AggregateGetAndPut(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{}}
	app.ConfigYml = config
	router := newServiceConfigRouter(NewServiceConfigController())

	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/service-config", nil))
	require.Equal(t, http.StatusOK, get.Code, get.Body.String())
	var envelope struct {
		Data ServiceConfig `json:"data"`
	}
	require.NoError(t, json.Unmarshal(get.Body.Bytes(), &envelope))
	require.NotNil(t, envelope.Data.Cascade)
	require.Equal(t, "UVP-GB28181", envelope.Data.Cascade.UserAgent)
	envelope.Data.Cascade = &gbconfig.CascadeSettings{UserAgent: "Gateway/1.0", PlatformName: "Regional platform", Manufacturer: "Vendor", Model: "Gateway", CivilCode: "340200", Address: "Monitoring center"}
	envelope.Data.PTZDefaultSpeed.Level = 8
	envelope.Data.SIPCommandTimeout.TimeoutSec = 20
	envelope.Data.GlobalSubscriptions.Items = []string{"catalog", "mobile_position"}
	envelope.Data.RecordCacheTailToleranceSec = 5

	put := httptest.NewRecorder()
	router.ServeHTTP(put, httptest.NewRequest(http.MethodPut, "/service-config", jsonBody(t, envelope.Data)))
	require.Equal(t, http.StatusOK, put.Code, put.Body.String())
	require.Equal(t, 1, config.saveNum)
	require.Equal(t, 8, config.values[ptzDefaultSpeedLevelConfigKey])
	require.Equal(t, 20, config.values[gbconfig.SIPCommandTimeoutSecConfigKey])
	require.Equal(t, []string{"catalog", "mobile_position"}, config.values[gbconfig.GlobalSubscriptionItemsConfigKey])
	require.Equal(t, 5, config.values[gbconfig.RecordCacheTailToleranceSecConfigKey])
	require.Equal(t, "Gateway/1.0", config.values[gbconfig.CascadeUserAgentConfigKey])
	require.Equal(t, "Regional platform", config.values[gbconfig.CascadePlatformNameConfigKey])
	require.Equal(t, "340200", config.values[gbconfig.CascadeCivilCodeConfigKey])
}

func TestServiceConfigController_RejectsInvalidRecordCacheTailTolerance(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{}}
	app.ConfigYml = config
	request := NewServiceConfigController().currentServiceConfig()
	request.RecordCacheTailToleranceSec = gbconfig.MaxRecordCacheTailToleranceSec + 1
	recorder := httptest.NewRecorder()
	newServiceConfigRouter(NewServiceConfigController()).ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/service-config", jsonBody(t, request)))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, config.saveNum)
}

func TestServiceConfigController_LegacyRequestPreservesCascadeSettings(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{gbconfig.CascadePlatformNameConfigKey: "Existing platform"}}
	app.ConfigYml = config
	controller := NewServiceConfigController()
	request := controller.currentServiceConfig()
	request.Cascade = nil
	request.PTZDefaultSpeed.Level = 8
	recorder := httptest.NewRecorder()
	newServiceConfigRouter(controller).ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/service-config", jsonBody(t, request)))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, "Existing platform", config.values[gbconfig.CascadePlatformNameConfigKey])
	var result struct {
		Data ServiceConfig `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
	require.Equal(t, "Existing platform", result.Data.Cascade.PlatformName)
}

func TestServiceConfigController_LegacyRequestPreservesTailTolerance(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{gbconfig.RecordCacheTailToleranceSecConfigKey: 5}}
	app.ConfigYml = config
	request := NewServiceConfigController().currentServiceConfig()
	payload, err := json.Marshal(request)
	require.NoError(t, err)
	var legacy map[string]interface{}
	require.NoError(t, json.Unmarshal(payload, &legacy))
	delete(legacy, "recordCacheTailToleranceSec")
	recorder := httptest.NewRecorder()
	newServiceConfigRouter(NewServiceConfigController()).ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/service-config", jsonBody(t, legacy)))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 5, config.values[gbconfig.RecordCacheTailToleranceSecConfigKey])
}

func TestServiceConfigController_InvalidCascadeDoesNotSave(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{}}
	app.ConfigYml = config
	request := NewServiceConfigController().currentServiceConfig()
	request.Cascade.UserAgent = "UVP\r\nX-Injected: true"
	recorder := httptest.NewRecorder()
	newServiceConfigRouter(NewServiceConfigController()).ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/service-config", jsonBody(t, request)))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, config.saveNum)
}

func TestServiceConfigController_AggregateRejectsInvalidWithoutSaving(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{}}
	app.ConfigYml = config
	request := NewServiceConfigController().currentServiceConfig()
	request.PTZDefaultSpeed.Level = 0
	recorder := httptest.NewRecorder()
	newServiceConfigRouter(NewServiceConfigController()).ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/service-config", jsonBody(t, request)))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, config.saveNum)
}

func TestServiceConfigController_AggregateRollsBackOnSaveFailure(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{ptzDefaultSpeedLevelConfigKey: 4, gbconfig.CascadePlatformNameConfigKey: "Existing platform"}, saveErr: errors.New("disk full")}
	app.ConfigYml = config
	request := NewServiceConfigController().currentServiceConfig()
	request.PTZDefaultSpeed.Level = 9
	request.Cascade.PlatformName = "New platform"
	recorder := httptest.NewRecorder()
	newServiceConfigRouter(NewServiceConfigController()).ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/service-config", jsonBody(t, request)))
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Equal(t, 4, config.values[ptzDefaultSpeedLevelConfigKey])
	require.Equal(t, "Existing platform", config.values[gbconfig.CascadePlatformNameConfigKey])
}

func TestServiceConfigController_AggregateRollsBackWhenPlayAuthTTLApplyFails(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{gbconfig.PlayAuthTTLSecondsConfigKey: 120}}
	app.ConfigYml = config
	controller := NewServiceConfigController()
	controller.SetPlayAuthTTLUpdater(func(time.Duration) error { return errors.New("runtime unavailable") })
	request := controller.currentServiceConfig()
	request.PlayAuth.TTLSeconds = 300
	recorder := httptest.NewRecorder()
	newServiceConfigRouter(controller).ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/service-config", jsonBody(t, request)))
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Equal(t, 120, config.values[gbconfig.PlayAuthTTLSecondsConfigKey])
}

// Device access 的两个基址是"设备能访问到"的地址，页面可配（2026-10-20 现场定案）。
// 这组用例锁住三件事：①读写往返；②老前端整包 PUT（不带/带 null）不能把地址清空；
// ③显式提交空串才算清空，且非法值当场拒绝不落盘。
func TestServiceConfigController_DeviceAccessRoundTrip(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{
		gbconfig.QRProvisionBaseURLConfigKey:    "http://10.0.0.68:51010",
		gbconfig.SnapshotUploadBaseURLConfigKey: "http://10.0.0.68:51001",
	}}
	app.ConfigYml = config
	router := newServiceConfigRouter(NewServiceConfigController())

	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/service-config", nil))
	require.Equal(t, http.StatusOK, get.Code, get.Body.String())
	var loaded struct {
		Data ServiceConfig `json:"data"`
	}
	require.NoError(t, json.Unmarshal(get.Body.Bytes(), &loaded))
	require.Equal(t, "http://10.0.0.68:51010", loaded.Data.DeviceAccess.QRProvisionBaseURL)
	require.Equal(t, "http://10.0.0.68:51001", loaded.Data.DeviceAccess.SnapshotUploadBaseURL)

	loaded.Data.DeviceAccess.QRProvisionBaseURL = "  https://vms.example.com:51000/  "
	loaded.Data.DeviceAccess.SnapshotUploadBaseURL = "http://112.25.55.6:51001"
	put := httptest.NewRecorder()
	router.ServeHTTP(put, httptest.NewRequest(http.MethodPut, "/service-config", jsonBody(t, loaded.Data)))
	require.Equal(t, http.StatusOK, put.Code, put.Body.String())
	// 入库形态与运行时读到的一致（去尾斜杠、scheme 小写），不是原始输入。
	require.Equal(t, "https://vms.example.com:51000", config.values[gbconfig.QRProvisionBaseURLConfigKey])
	require.Equal(t, "http://112.25.55.6:51001", config.values[gbconfig.SnapshotUploadBaseURLConfigKey])
}

func TestServiceConfigController_LegacyRequestPreservesDeviceAccess(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(map[string]interface{})
		qr      string
		snapsho string
	}{
		{name: "整包不带 deviceAccess", mutate: func(payload map[string]interface{}) { delete(payload, "deviceAccess") }, qr: "http://10.0.0.68:51010", snapsho: "http://10.0.0.68:51001"},
		{name: "deviceAccess 为 null", mutate: func(payload map[string]interface{}) { payload["deviceAccess"] = nil }, qr: "http://10.0.0.68:51010", snapsho: "http://10.0.0.68:51001"},
		{
			name: "只带一个字段",
			mutate: func(payload map[string]interface{}) {
				payload["deviceAccess"] = map[string]interface{}{"snapshotUploadBaseUrl": "http://112.25.55.6:51001"}
			},
			qr:      "http://10.0.0.68:51010",
			snapsho: "http://112.25.55.6:51001",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			previous := app.ConfigYml
			t.Cleanup(func() { app.ConfigYml = previous })
			config := &serviceConfigTestYAML{values: map[string]interface{}{
				gbconfig.QRProvisionBaseURLConfigKey:    "http://10.0.0.68:51010",
				gbconfig.SnapshotUploadBaseURLConfigKey: "http://10.0.0.68:51001",
			}}
			app.ConfigYml = config
			controller := NewServiceConfigController()
			payload, err := json.Marshal(controller.currentServiceConfig())
			require.NoError(t, err)
			var legacy map[string]interface{}
			require.NoError(t, json.Unmarshal(payload, &legacy))
			tc.mutate(legacy)

			recorder := httptest.NewRecorder()
			newServiceConfigRouter(controller).ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/service-config", jsonBody(t, legacy)))
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.Equal(t, tc.qr, config.values[gbconfig.QRProvisionBaseURLConfigKey])
			require.Equal(t, tc.snapsho, config.values[gbconfig.SnapshotUploadBaseURLConfigKey])
		})
	}
}

func TestServiceConfigController_DeviceAccessExplicitEmptyClears(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	config := &serviceConfigTestYAML{values: map[string]interface{}{
		gbconfig.QRProvisionBaseURLConfigKey:    "http://10.0.0.68:51010",
		gbconfig.SnapshotUploadBaseURLConfigKey: "http://10.0.0.68:51001",
	}}
	app.ConfigYml = config
	controller := NewServiceConfigController()
	payload, err := json.Marshal(controller.currentServiceConfig())
	require.NoError(t, err)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(payload, &body))
	body["deviceAccess"] = map[string]interface{}{"qrProvisionBaseUrl": "", "snapshotUploadBaseUrl": ""}

	recorder := httptest.NewRecorder()
	newServiceConfigRouter(controller).ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/service-config", jsonBody(t, body)))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Empty(t, config.values[gbconfig.QRProvisionBaseURLConfigKey])
	require.Empty(t, config.values[gbconfig.SnapshotUploadBaseURLConfigKey])
}

func TestServiceConfigController_RejectsUnreachableDeviceAccess(t *testing.T) {
	cases := []struct {
		name string
		body map[string]interface{}
		msg  string
	}{
		{name: "抓拍地址是回环", body: map[string]interface{}{"snapshotUploadBaseUrl": "http://127.0.0.1:51001"}, msg: "回环"},
		{name: "抓拍地址是 localhost", body: map[string]interface{}{"snapshotUploadBaseUrl": "http://localhost:51001"}, msg: "localhost"},
		{name: "扫码地址不是 URL", body: map[string]interface{}{"qrProvisionBaseUrl": "10.0.0.68:51010"}, msg: "扫码配网地址"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			previous := app.ConfigYml
			t.Cleanup(func() { app.ConfigYml = previous })
			config := &serviceConfigTestYAML{values: map[string]interface{}{
				gbconfig.QRProvisionBaseURLConfigKey:    "http://10.0.0.68:51010",
				gbconfig.SnapshotUploadBaseURLConfigKey: "http://10.0.0.68:51001",
			}}
			app.ConfigYml = config
			controller := NewServiceConfigController()
			payload, err := json.Marshal(controller.currentServiceConfig())
			require.NoError(t, err)
			var body map[string]interface{}
			require.NoError(t, json.Unmarshal(payload, &body))
			access, _ := body["deviceAccess"].(map[string]interface{})
			require.NotNil(t, access)
			for key, value := range tc.body {
				access[key] = value
			}

			recorder := httptest.NewRecorder()
			newServiceConfigRouter(controller).ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/service-config", jsonBody(t, body)))
			require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), tc.msg)
			require.Zero(t, config.saveNum)
			require.Equal(t, "http://10.0.0.68:51010", config.values[gbconfig.QRProvisionBaseURLConfigKey])
			require.Equal(t, "http://10.0.0.68:51001", config.values[gbconfig.SnapshotUploadBaseURLConfigKey])
		})
	}
}

func jsonBody(t *testing.T, value interface{}) *bytes.Reader {
	t.Helper()
	payload, err := json.Marshal(value)
	require.NoError(t, err)
	return bytes.NewReader(payload)
}
