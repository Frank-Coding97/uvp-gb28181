package controllers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"

	gbsetup "uvplatform.cn/uvp-gb28181/app/gb28181/setup"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

func newSetupControllerDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbsetup.SystemInstallation{}, &gbsetup.SIPConfig{}))
	require.NoError(t, db.Create(&gbsetup.SystemInstallation{
		ID: gbsetup.SingletonID, OnboardingVersion: 1, SIPOnboardingStatus: gbsetup.OnboardingPending,
	}).Error)
	return db
}

func newSetupControllerRouter(controller *SetupController) *gin.Engine {
	gin.SetMode(gin.TestMode)
	app.Response = response.NewResponseHandler()
	app.ZapLog = zap.NewNop()
	router := gin.New()
	router.GET("/status", controller.Status)
	router.PUT("/config", controller.SaveConfig)
	return router
}

func decodeSetupResponse(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var response map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestSetupController_StatusPendingAndNoPassword(t *testing.T) {
	db := newSetupControllerDB(t)
	password := "Secret123"
	require.NoError(t, db.Create(&gbsetup.SIPConfig{
		ID: gbsetup.SingletonID, DeploymentMode: gbsetup.DeploymentLAN,
		ListenIP: "0.0.0.0", AdvertiseIP: "192.168.1.10", Port: 5061,
		Domain: "3402000000", ServerID: "34020000002000000001", Password: password,
	}).Error)
	router := newSetupControllerRouter(NewSetupController(db, gbsetup.NewRuntimeStatus(), nil))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/status", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotContains(t, recorder.Body.String(), password)
	data := decodeSetupResponse(t, recorder)["data"].(map[string]any)
	require.Equal(t, "pending", data["onboardingStatus"])
	require.Equal(t, "configured", data["configStatus"])
	require.Equal(t, true, data["canConfigure"])
	config := data["config"].(map[string]any)
	require.Equal(t, true, config["hasPassword"])
}

func TestSetupController_SaveConfigAndPreservePassword(t *testing.T) {
	db := newSetupControllerDB(t)
	runtime := gbsetup.NewRuntimeStatus()
	router := newSetupControllerRouter(NewSetupController(db, runtime, nil))

	request := `{"deploymentMode":"lan","listenIp":"0.0.0.0","advertiseIp":"192.168.1.10","port":5061,"domain":"3402000000","serverId":"34020000002000000001","password":"Secret123"}`
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/config", bytes.NewBufferString(request)))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, gbsetup.RuntimeRestartRequired, runtime.Snapshot().State)

	request = `{"deploymentMode":"lan","listenIp":"192.168.1.20","advertiseIp":"192.168.1.20","port":5062,"domain":"3402000000","serverId":"34020000002000000001"}`
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/config", bytes.NewBufferString(request)))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var row gbsetup.SIPConfig
	require.NoError(t, db.First(&row, gbsetup.SingletonID).Error)
	require.Equal(t, "Secret123", row.Password)
	require.Equal(t, 5062, row.Port)
}

func TestSetupController_SaveConfigValidationAndDatabaseFailure(t *testing.T) {
	db := newSetupControllerDB(t)
	router := newSetupControllerRouter(NewSetupController(db, gbsetup.NewRuntimeStatus(), nil))
	invalid := `{"deploymentMode":"lan","listenIp":"0.0.0.0","advertiseIp":"0.0.0.0","port":5061,"domain":"bad","serverId":"bad","password":"Secret123"}`
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
