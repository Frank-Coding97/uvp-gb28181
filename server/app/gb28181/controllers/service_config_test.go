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
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
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
	envelope.Data.PTZDefaultSpeed.Level = 8
	envelope.Data.SIPCommandTimeout.TimeoutSec = 20
	envelope.Data.GlobalSubscriptions.Items = []string{"catalog", "mobile_position"}

	put := httptest.NewRecorder()
	router.ServeHTTP(put, httptest.NewRequest(http.MethodPut, "/service-config", jsonBody(t, envelope.Data)))
	require.Equal(t, http.StatusOK, put.Code, put.Body.String())
	require.Equal(t, 1, config.saveNum)
	require.Equal(t, 8, config.values[ptzDefaultSpeedLevelConfigKey])
	require.Equal(t, 20, config.values[gbconfig.SIPCommandTimeoutSecConfigKey])
	require.Equal(t, []string{"catalog", "mobile_position"}, config.values[gbconfig.GlobalSubscriptionItemsConfigKey])
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
	config := &serviceConfigTestYAML{values: map[string]interface{}{ptzDefaultSpeedLevelConfigKey: 4}, saveErr: errors.New("disk full")}
	app.ConfigYml = config
	request := NewServiceConfigController().currentServiceConfig()
	request.PTZDefaultSpeed.Level = 9
	recorder := httptest.NewRecorder()
	newServiceConfigRouter(NewServiceConfigController()).ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/service-config", jsonBody(t, request)))
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Equal(t, 4, config.values[ptzDefaultSpeedLevelConfigKey])
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

func jsonBody(t *testing.T, value interface{}) *bytes.Reader {
	t.Helper()
	payload, err := json.Marshal(value)
	require.NoError(t, err)
	return bytes.NewReader(payload)
}
