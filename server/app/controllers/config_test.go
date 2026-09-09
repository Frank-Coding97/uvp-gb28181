package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

type configControllerTestYAML struct {
	app.YmlConfigInterf
	values  map[string]interface{}
	saveNum int
}

func (c *configControllerTestYAML) GetString(key string) string {
	value, _ := c.values[key].(string)
	return value
}
func (c *configControllerTestYAML) GetBool(key string) bool {
	value, _ := c.values[key].(bool)
	return value
}
func (c *configControllerTestYAML) GetInt(key string) int {
	value, _ := c.values[key].(int)
	return value
}
func (c *configControllerTestYAML) Set(key string, value interface{}) {
	c.values[key] = value
}
func (c *configControllerTestYAML) SaveConfig() error {
	c.saveNum++
	return nil
}

func newConfigControllerTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	controller := NewConfigController()
	router.GET("/config/get", controller.GetConfig)
	router.PUT("/config/update", controller.UpdateConfig)
	return router
}

func configResponseSystem(t *testing.T, recorder *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var envelope struct {
		Data map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	system, ok := envelope.Data["system"].(map[string]interface{})
	require.True(t, ok, "响应应包含 system 配置")
	return system
}

func TestConfigControllerSystemBrandRoundTrip(t *testing.T) {
	previousConfig, previousResponse := app.ConfigYml, app.Response
	t.Cleanup(func() {
		app.ConfigYml = previousConfig
		app.Response = previousResponse
	})
	config := &configControllerTestYAML{values: map[string]interface{}{
		"system.systembrand":         "",
		"server.demoaccount.enabled": false,
	}}
	app.ConfigYml = config
	app.Response = response.NewResponseHandler()
	router := newConfigControllerTestRouter()

	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/config/get", nil))
	require.Equal(t, http.StatusOK, get.Code, get.Body.String())
	system := configResponseSystem(t, get)
	value, ok := system["systemBrand"]
	require.True(t, ok, "响应应包含 systemBrand")
	require.Equal(t, "", value, "未配置品牌简称时应保持空值")

	update := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/config/update", strings.NewReader(`{"system":{"systemBrand":"Ren"}}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(update, request)
	require.Equal(t, http.StatusOK, update.Code, update.Body.String())
	require.Equal(t, "Ren", config.values["system.systembrand"])
	require.Equal(t, 1, config.saveNum)

	get = httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/config/get", nil))
	require.Equal(t, "Ren", configResponseSystem(t, get)["systemBrand"])

	clear := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPut, "/config/update", strings.NewReader(`{"system":{"systemBrand":""}}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(clear, request)
	require.Equal(t, http.StatusOK, clear.Code, clear.Body.String())
	require.Equal(t, "", config.values["system.systembrand"])
	require.Equal(t, 2, config.saveNum)

	get = httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/config/get", nil))
	require.Equal(t, "", configResponseSystem(t, get)["systemBrand"], "空值不能回退到 UVP")
}

func TestSystemConfigSystemBrandJSONMapping(t *testing.T) {
	var request models.ConfigRequest
	require.NoError(t, json.Unmarshal([]byte(`{"system":{"systemBrand":"Ren"}}`), &request))
	require.Equal(t, "Ren", request.System.SystemBrand)
}

func TestConfigExampleSystemBrandDefaultsToEmpty(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "config", "config.example.yml"))
	require.NoError(t, err)
	var config struct {
		System map[string]interface{} `yaml:"system"`
	}
	require.NoError(t, yaml.Unmarshal(body, &config))
	brand, ok := config.System["systembrand"]
	require.True(t, ok, "默认模板应声明 systembrand")
	require.Equal(t, "", brand, "默认模板的 systembrand 应为空")
}
