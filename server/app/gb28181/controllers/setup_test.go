package controllers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbsetup "uvplatform.cn/uvp-gb28181/app/gb28181/setup"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

func newSetupControllerDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbsetup.SIPConfig{}))
	return db
}

func TestSetupController_AuditLogsNeverContainPassword(t *testing.T) {
	db := newSetupControllerDB(t)
	controller := NewSetupController(db, gbsetup.NewRuntimeStatus(), nil, nil)
	router := newSetupControllerRouter(controller)
	core, observed := observer.New(zap.InfoLevel)
	app.ZapLog = zap.New(core)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/skip", nil))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	request := `{"deploymentMode":"lan","listenIp":"0.0.0.0","advertiseIp":"192.168.1.10","port":5061,"domain":"3402000000","serverId":"34020000002000000001","password":"Sec12345Aa!!"}`
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/config", bytes.NewBufferString(request)))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	encoded := ""
	for _, entry := range observed.All() {
		encoded += entry.Message
		for key, value := range entry.ContextMap() {
			encoded += key
			encoded += fmt.Sprint(value)
		}
	}
	require.Contains(t, encoded, "SIP 配置已保存")
	require.Contains(t, encoded, "SIP 首次安装引导已暂缓")
	require.NotContains(t, encoded, "Sec12345Aa!!")
}

func newSetupControllerRouter(controller *SetupController) *gin.Engine {
	gin.SetMode(gin.TestMode)
	app.Response = response.NewResponseHandler()
	app.ZapLog = zap.NewNop()
	router := gin.New()
	router.GET("/status", controller.Status)
	router.PUT("/config", controller.SaveConfig)
	router.POST("/skip", controller.Skip)
	return router
}

func TestSetupController_SkipReturnsAcknowledged(t *testing.T) {
	db := newSetupControllerDB(t)
	router := newSetupControllerRouter(NewSetupController(db, gbsetup.NewRuntimeStatus(), nil, nil))
	// 新架构下 skip 是纯前端信号,后端只返回 acknowledged
	for i := 0; i < 3; i++ {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/skip", nil))
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.Contains(t, recorder.Body.String(), `"acknowledged":true`)
	}
}

func decodeSetupResponse(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var response map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestSetupController_StatusReflectsDBConfig(t *testing.T) {
	db := newSetupControllerDB(t)
	password := "Sec12345Aa!!"
	require.NoError(t, db.Create(&gbsetup.SIPConfig{
		ID: gbsetup.SingletonID, DeploymentMode: gbsetup.DeploymentLAN,
		ListenIP: "0.0.0.0", AdvertiseIP: "192.168.1.10", Port: 5061,
		Domain: "3402000000", ServerID: "34020000002000000001", Password: password,
	}).Error)
	router := newSetupControllerRouter(NewSetupController(db, gbsetup.NewRuntimeStatus(), nil, nil))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/status", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	// 2026-07-20 起 status 接口返回明文密码 —— 用户需要抄到设备端;权限拦截在 gin middleware,
	// 该 test 用无中间件的 router,直接验证 view 层字段展开.
	require.Contains(t, recorder.Body.String(), password)
	data := decodeSetupResponse(t, recorder)["data"].(map[string]any)
	require.Equal(t, "configured", data["configStatus"])
	config := data["config"].(map[string]any)
	require.Equal(t, true, config["hasPassword"])
	require.Equal(t, password, config["password"])
	// runtime 字段一定存在,新架构下这是前端 gating 依据
	_, ok := data["runtime"].(map[string]any)
	require.True(t, ok, "runtime snapshot should always be present")
}

func TestSetupController_StatusUnconfiguredWhenDBEmpty(t *testing.T) {
	db := newSetupControllerDB(t)
	router := newSetupControllerRouter(NewSetupController(db, gbsetup.NewRuntimeStatus(), nil, nil))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/status", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	data := decodeSetupResponse(t, recorder)["data"].(map[string]any)
	require.Equal(t, "unconfigured", data["configStatus"])
	require.Nil(t, data["config"], "config 未落库时应为 null")
}

func TestSetupController_SaveConfigAndPreservePassword(t *testing.T) {
	db := newSetupControllerDB(t)
	runtime := gbsetup.NewRuntimeStatus()
	router := newSetupControllerRouter(NewSetupController(db, runtime, nil, nil))

	request := `{"deploymentMode":"lan","listenIp":"0.0.0.0","advertiseIp":"192.168.1.10","port":5061,"domain":"3402000000","serverId":"34020000002000000001","password":"Sec12345Aa!!"}`
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/config", bytes.NewBufferString(request)))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	// 保存后 runtime state 由 reloader 主导;测试传 nil reloader 时保持初始 disabled 态,
	// 生产链路里 bootstrap 会注入 ReloadSIP,把状态推进到 running / failed.
	require.NotEqual(t, gbsetup.RuntimeRestartRequired, runtime.Snapshot().State,
		"restart_required 语义已废弃,SaveConfig 现在直接热启动")

	request = `{"deploymentMode":"lan","listenIp":"192.168.1.20","advertiseIp":"192.168.1.20","port":5062,"domain":"3402000000","serverId":"34020000002000000001"}`
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/config", bytes.NewBufferString(request)))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var row gbsetup.SIPConfig
	require.NoError(t, db.First(&row, gbsetup.SingletonID).Error)
	require.Equal(t, "Sec12345Aa!!", row.Password)
	require.Equal(t, 5062, row.Port)
}

func TestSetupController_SaveConfigTriggersReload(t *testing.T) {
	db := newSetupControllerDB(t)
	runtime := gbsetup.NewRuntimeStatus()
	reloadCalls := 0
	reload := func() error {
		reloadCalls++
		runtime.MarkRunning()
		return nil
	}
	router := newSetupControllerRouter(NewSetupController(db, runtime, nil, reload))
	body := `{"deploymentMode":"lan","listenIp":"0.0.0.0","advertiseIp":"192.168.1.10","port":5061,"domain":"3402000000","serverId":"34020000002000000001","password":"Sec12345Aa!!"}`
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/config", bytes.NewBufferString(body)))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, 1, reloadCalls, "reloader 应被调用一次")
	require.Equal(t, gbsetup.RuntimeRunning, runtime.Snapshot().State)
	require.Contains(t, recorder.Body.String(), `"reloadedOk":true`)
}

func TestSetupController_SaveConfigReloadFailurePropagates(t *testing.T) {
	db := newSetupControllerDB(t)
	runtime := gbsetup.NewRuntimeStatus()
	reload := func() error {
		runtime.MarkFailed("bind failed")
		return errors.New("bind failed")
	}
	router := newSetupControllerRouter(NewSetupController(db, runtime, nil, reload))
	body := `{"deploymentMode":"lan","listenIp":"0.0.0.0","advertiseIp":"192.168.1.10","port":5061,"domain":"3402000000","serverId":"34020000002000000001","password":"Sec12345Aa!!"}`
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/config", bytes.NewBufferString(body)))
	// 保存动作本身成功(数据已落 DB),reload 失败通过响应体告知前端
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), `"reloadedOk":false`)
	require.Contains(t, recorder.Body.String(), "bind failed")
	require.Equal(t, gbsetup.RuntimeFailed, runtime.Snapshot().State)
}

func TestSetupController_SaveConfigValidationAndDatabaseFailure(t *testing.T) {
	db := newSetupControllerDB(t)
	router := newSetupControllerRouter(NewSetupController(db, gbsetup.NewRuntimeStatus(), nil, nil))
	invalid := `{"deploymentMode":"lan","listenIp":"0.0.0.0","advertiseIp":"0.0.0.0","port":5061,"domain":"bad","serverId":"bad","password":"Sec12345Aa!!"}`
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/config", bytes.NewBufferString(invalid)))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "advertiseIp")

	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/status", nil))
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "SELECT")
}
