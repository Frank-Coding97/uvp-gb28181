package controllers

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/logcleanup"
	"uvplatform.com/uvp-gb28181/app/models"
	"uvplatform.com/uvp-gb28181/app/utils/response"
)

type configControllerYML struct {
	values   map[string]interface{}
	saveErr  error
	saveCall int
}

func (c *configControllerYML) ConfigFileChangeListen(...func()) {}
func (c *configControllerYML) Get(key string) interface{}       { return c.values[key] }
func (c *configControllerYML) GetString(key string) string {
	value, _ := c.values[key].(string)
	return value
}
func (c *configControllerYML) GetBool(key string) bool {
	value, _ := c.values[key].(bool)
	return value
}
func (c *configControllerYML) GetInt(key string) int             { value, _ := c.values[key].(int); return value }
func (c *configControllerYML) GetInt32(key string) int32         { return int32(c.GetInt(key)) }
func (c *configControllerYML) GetInt64(key string) int64         { return int64(c.GetInt(key)) }
func (c *configControllerYML) GetFloat64(string) float64         { return 0 }
func (c *configControllerYML) GetDuration(string) time.Duration  { return 0 }
func (c *configControllerYML) GetStringSlice(string) []string    { return nil }
func (c *configControllerYML) GetUintSlice(string) []uint        { return nil }
func (c *configControllerYML) Set(key string, value interface{}) { c.values[key] = value }
func (c *configControllerYML) SaveConfig() error                 { c.saveCall++; return c.saveErr }

func setupLogCleanupConfigController(t *testing.T, values map[string]interface{}) (*gorm.DB, *configControllerYML) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}))
	config := &configControllerYML{values: values}
	oldDB, oldConfig, oldResponse, oldLog := app.GormDbMysql, app.ConfigYml, app.Response, app.ZapLog
	t.Cleanup(func() {
		app.GormDbMysql, app.ConfigYml, app.Response, app.ZapLog = oldDB, oldConfig, oldResponse, oldLog
		logcleanup.SetSIPReloader(nil)
	})
	app.GormDbMysql = db
	app.ConfigYml = config
	app.Response = response.NewResponseHandler()
	app.ZapLog = zap.NewNop()
	logcleanup.SetSIPReloader(nil)
	gin.SetMode(gin.TestMode)
	return db, config
}

func invokeConfigUpdate(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("PUT", "/api/config/update", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	func() {
		defer func() { _ = recover() }()
		NewConfigController().UpdateConfig(ctx)
	}()
	return recorder
}

func TestGetConfigReturnsLogCleanupDefaultsAndConfiguredState(t *testing.T) {
	_, _ = setupLogCleanupConfigController(t, map[string]interface{}{"gormv2.usedbtype": "mysql"})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "/api/config/get", nil)
	NewConfigController().GetConfig(ctx)
	require.Equal(t, 200, recorder.Code)
	var responseBody map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &responseBody))
	data := responseBody["data"].(map[string]interface{})
	got := data["logCleanup"].(map[string]interface{})
	require.Equal(t, float64(7), got["sipRetentionDays"])
	require.Equal(t, float64(180), got["operationRetentionDays"])
	require.Equal(t, float64(180), got["loginRetentionDays"])
	require.Equal(t, float64(30), got["jobRetentionDays"])
	require.Equal(t, float64(7), got["playbackRetentionDays"])
	require.Equal(t, float64(7), got["schedulerRetentionDays"])
	require.Equal(t, false, got["configured"])
}

func TestUpdateConfigAcceptsOnlyLogCleanupWithoutClearingOtherSections(t *testing.T) {
	_, config := setupLogCleanupConfigController(t, map[string]interface{}{
		"system.systemname": "既有平台", "safe.loginlockthreshold": 8, "captcha.length": 5,
		"gb28181.trace.retention_days": 7,
	})
	recorder := invokeConfigUpdate(t, `{"logCleanup":{"sipRetentionDays":7,"operationRetentionDays":90,"loginRetentionDays":120,"jobRetentionDays":30,"playbackRetentionDays":7,"schedulerRetentionDays":5,"configured":false}}`)
	require.Equal(t, 200, recorder.Code, recorder.Body.String())
	require.Equal(t, "既有平台", config.GetString("system.systemname"))
	require.Equal(t, 8, config.GetInt("safe.loginlockthreshold"))
	require.Equal(t, 5, config.GetInt("captcha.length"))
	require.Equal(t, 90, config.GetInt(logcleanup.OperationRetentionDaysConfigKey))
	require.Equal(t, 120, config.GetInt(logcleanup.LoginRetentionDaysConfigKey))
	require.Equal(t, true, config.GetBool(logcleanup.ConfiguredConfigKey), "configured must be set by the server after a valid save")
}

func TestUpdateConfigWithoutLogCleanupPreservesLegacyRequestBehavior(t *testing.T) {
	_, config := setupLogCleanupConfigController(t, map[string]interface{}{
		"system.systemname": "before", "safe.loginlockthreshold": 8,
		logcleanup.ConfiguredConfigKey: true, logcleanup.OperationRetentionDaysConfigKey: 45,
	})
	recorder := invokeConfigUpdate(t, `{"system":{"systemName":"after"}}`)
	require.Equal(t, 200, recorder.Code, recorder.Body.String())
	require.Equal(t, "after", config.GetString("system.systemname"))
	require.Equal(t, 8, config.GetInt("safe.loginlockthreshold"))
	require.True(t, config.GetBool(logcleanup.ConfiguredConfigKey))
	require.Equal(t, 45, config.GetInt(logcleanup.OperationRetentionDaysConfigKey))
}

func TestUpdateConfigRejectsInvalidOrNonIntegerRetentionBeforeWriting(t *testing.T) {
	tests := []struct {
		name, body, message string
	}{
		{"out of range", `{"logCleanup":{"sipRetentionDays":7,"operationRetentionDays":0,"loginRetentionDays":120,"jobRetentionDays":30,"playbackRetentionDays":7,"schedulerRetentionDays":5}}`, "operationRetentionDays"},
		{"missing field", `{"logCleanup":{"sipRetentionDays":7,"operationRetentionDays":90,"loginRetentionDays":120,"jobRetentionDays":30,"playbackRetentionDays":7}}`, "全部六类"},
		{"fractional field", `{"logCleanup":{"sipRetentionDays":7,"operationRetentionDays":90.5,"loginRetentionDays":120,"jobRetentionDays":30,"playbackRetentionDays":7,"schedulerRetentionDays":5}}`, "参数绑定失败"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, config := setupLogCleanupConfigController(t, map[string]interface{}{"system.systemname": "before"})
			recorder := invokeConfigUpdate(t, test.body)
			require.Equal(t, 400, recorder.Code)
			require.Contains(t, recorder.Body.String(), test.message)
			require.Equal(t, "before", config.GetString("system.systemname"))
			require.Zero(t, config.saveCall)
			require.Nil(t, config.Get(logcleanup.ConfiguredConfigKey))
		})
	}
}

func TestUpdateConfigRestoresInMemoryValuesWhenSaveFails(t *testing.T) {
	_, config := setupLogCleanupConfigController(t, map[string]interface{}{
		"system.systemname": "before", "gb28181.trace.retention_days": 7,
		logcleanup.OperationRetentionDaysConfigKey: 45,
	})
	config.saveErr = errors.New("disk full")
	recorder := invokeConfigUpdate(t, `{"system":{"systemName":"after"},"logCleanup":{"sipRetentionDays":7,"operationRetentionDays":90,"loginRetentionDays":120,"jobRetentionDays":30,"playbackRetentionDays":7,"schedulerRetentionDays":5}}`)
	require.Equal(t, 400, recorder.Code)
	require.Equal(t, "before", config.GetString("system.systemname"))
	require.Equal(t, 45, config.GetInt(logcleanup.OperationRetentionDaysConfigKey))
	require.Nil(t, config.Get(logcleanup.ConfiguredConfigKey))
}

func TestSIPRetentionChangeRequiresAndInvokesRuntimeReloader(t *testing.T) {
	_, config := setupLogCleanupConfigController(t, map[string]interface{}{"gb28181.trace.retention_days": 7})
	logcleanup.SetSIPReloader(func() error { return nil })
	recorder := invokeConfigUpdate(t, `{"logCleanup":{"sipRetentionDays":14,"operationRetentionDays":180,"loginRetentionDays":180,"jobRetentionDays":30,"playbackRetentionDays":7,"schedulerRetentionDays":7}}`)
	require.Equal(t, 200, recorder.Code, recorder.Body.String())
	require.Equal(t, 14, config.GetInt("gb28181.trace.retention_days"))
}

func TestSIPRetentionChangeReportsSavedButNotAppliedWithoutReloader(t *testing.T) {
	_, config := setupLogCleanupConfigController(t, map[string]interface{}{"gb28181.trace.retention_days": 7})
	recorder := invokeConfigUpdate(t, `{"logCleanup":{"sipRetentionDays":14,"operationRetentionDays":180,"loginRetentionDays":180,"jobRetentionDays":30,"playbackRetentionDays":7,"schedulerRetentionDays":7}}`)
	require.Equal(t, 503, recorder.Code)
	require.Contains(t, recorder.Body.String(), "配置已保存")
	require.Equal(t, 14, config.GetInt("gb28181.trace.retention_days"))
}
