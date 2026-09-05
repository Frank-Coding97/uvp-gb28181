package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	appmodels "uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/openapi/resource"
)

func TestOpenAPIMetadataControllersRequireTrustedOwner(t *testing.T) {
	db := newControllerTestDB(t)
	svc := resource.New(db)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	controller := NewDeviceController(svc, nil)
	r.GET("/devices", controller.List)

	req := httptest.NewRequest(http.MethodGet, "/devices", nil)
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)

	assert.Equal(t, http.StatusForbidden, resp.Code)
	assert.Equal(t, "AUTH_REQUIRED", decodeControllerResponse(t, resp).Code)
}

func TestOpenAPIMetadataControllersRejectUnknownQuery(t *testing.T) {
	db := newControllerTestDB(t)
	svc := resource.New(db)
	r := gin.New()
	controller := NewDeviceController(svc, func(*gin.Context) (uint, bool) { return 10, true })
	r.GET("/devices", controller.List)

	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/devices?ownerDeptId=10", nil))

	assert.Equal(t, http.StatusBadRequest, resp.Code)
	assert.Equal(t, "INVALID_REQUEST", decodeControllerResponse(t, resp).Code)

	resp = httptest.NewRecorder()
	r.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/devices?page=0", nil))
	assert.Equal(t, http.StatusBadRequest, resp.Code)
	assert.Equal(t, "INVALID_REQUEST", decodeControllerResponse(t, resp).Code)
}

func TestOpenAPIMetadataControllersExposeAllSixOperations(t *testing.T) {
	db := newControllerTestDB(t)
	svc := resource.New(db)
	resolver := func(*gin.Context) (uint, bool) { return 10, true }
	deviceController := NewDeviceController(svc, resolver)
	channelController := NewChannelController(svc, resolver)
	r := gin.New()
	r.GET("/devices", deviceController.List)
	r.GET("/devices/:deviceId", deviceController.Get)
	r.GET("/devices/:deviceId/status", deviceController.Status)
	r.GET("/devices/:deviceId/channels", channelController.List)
	r.GET("/devices/:deviceId/channels/:channelId", channelController.Get)
	r.GET("/devices/:deviceId/channels/:channelId/status", channelController.Status)

	paths := []string{
		"/devices",
		"/devices/34020000002000000010",
		"/devices/34020000002000000010/status",
		"/devices/34020000002000000010/channels",
		"/devices/34020000002000000010/channels/37011200001310000010",
		"/devices/34020000002000000010/channels/37011200001310000010/status",
	}
	for _, path := range paths {
		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, http.StatusOK, resp.Code, path)
		assert.Equal(t, "OK", decodeControllerResponse(t, resp).Code, path)
	}
}

func TestOpenAPIMetadataControllersExposeWhitelistAndUniformNotFound(t *testing.T) {
	db := newControllerTestDB(t)
	svc := resource.New(db)
	r := gin.New()
	resolver := func(*gin.Context) (uint, bool) { return 10, true }
	deviceController := NewDeviceController(svc, resolver)
	channelController := NewChannelController(svc, resolver)
	r.GET("/devices/:deviceId", deviceController.Get)
	r.GET("/devices/:deviceId/channels/:channelId", channelController.Get)

	visible := httptest.NewRecorder()
	r.ServeHTTP(visible, httptest.NewRequest(http.MethodGet, "/devices/34020000002000000010", nil))
	require.Equal(t, http.StatusOK, visible.Code)
	visiblePayload := decodeControllerResponse(t, visible)
	visibleData, ok := visiblePayload.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "34020000002000000010", visibleData["deviceId"])
	assert.NotContains(t, visibleData, "ownerDeptId")
	assert.NotContains(t, visibleData, "ip")
	assert.NotContains(t, visibleData, "snapshotUrl")

	foreign := httptest.NewRecorder()
	r.ServeHTTP(foreign, httptest.NewRequest(http.MethodGet, "/devices/34020000002000000020", nil))
	missing := httptest.NewRecorder()
	r.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/devices/34020000002000009999", nil))
	assert.Equal(t, http.StatusNotFound, foreign.Code)
	assert.Equal(t, http.StatusNotFound, missing.Code)
	assert.Equal(t, foreign.Body.String(), missing.Body.String())

	channel := httptest.NewRecorder()
	r.ServeHTTP(channel, httptest.NewRequest(http.MethodGet, "/devices/34020000002000000010/channels/37011200001310000010", nil))
	require.Equal(t, http.StatusOK, channel.Code)
	channelPayload := decodeControllerResponse(t, channel)
	channelData, ok := channelPayload.Data.(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, channelData, "streamId")
	assert.NotContains(t, channelData, "currentSsrc")
	assert.NotContains(t, channelData, "snapshotUrl")
}

type controllerResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId"`
	Data      any    `json:"data"`
}

func decodeControllerResponse(t *testing.T, response *httptest.ResponseRecorder) controllerResponse {
	t.Helper()
	var payload controllerResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
	return payload
}

func newControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	active := int8(1)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &appmodels.SysDepartment{}))
	require.NoError(t, db.Create(&appmodels.SysDepartment{BaseModel: appmodels.BaseModel{ID: 10}, Name: "A", Status: &active}).Error)
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: "34020000002000000010", Name: "本部门设备", Status: gbmodels.DeviceStatusOnline, OwnerDeptID: 10}).Error)
	require.NoError(t, db.Create(&gbmodels.GbChannel{DeviceID: "34020000002000000010", ChannelID: "37011200001310000010", Name: "正常通道", Status: gbmodels.ChannelStatusOnline, OwnerDeptID: 10}).Error)
	t.Cleanup(func() { _ = raw.Close() })
	return db
}
