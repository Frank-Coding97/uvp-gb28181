package controllers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	"uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

func TestHomeDashboardControllerLayoutLifecycleAndCAS(t *testing.T) {
	app.Response = response.NewResponseHandler()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.GbDashboardLayout{}))
	controller := NewHomeDashboardController(func() *gorm.DB { return db })
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 7}})
		c.Next()
	})
	router.GET("/layout", controller.GetLayout)
	router.PUT("/layout", controller.SaveLayout)
	router.DELETE("/layout", controller.ResetLayout)

	get := performDashboardRequest(router, http.MethodGet, "/layout", nil)
	require.Equal(t, http.StatusOK, get.Code)
	require.Contains(t, get.Body.String(), `"revision":0`)
	require.Contains(t, get.Body.String(), `"sip-rpm"`)

	payload := []byte(`{"revision":0,"layout":{"schemaVersion":1,"widgets":[]}}`)
	save := performDashboardRequest(router, http.MethodPut, "/layout", payload)
	require.Equal(t, http.StatusOK, save.Code, save.Body.String())
	require.Contains(t, save.Body.String(), `"revision":1`)

	conflict := performDashboardRequest(router, http.MethodPut, "/layout", payload)
	require.Equal(t, http.StatusConflict, conflict.Code, conflict.Body.String())
	require.Contains(t, conflict.Body.String(), "LAYOUT_REVISION_CONFLICT")

	reset := performDashboardRequest(router, http.MethodDelete, "/layout", nil)
	require.Equal(t, http.StatusOK, reset.Code)
	require.Contains(t, reset.Body.String(), `"revision":0`)
}

func TestHomeDashboardControllerRejectsOversizedLayout(t *testing.T) {
	app.Response = response.NewResponseHandler()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	controller := NewHomeDashboardController(func() *gorm.DB { return db })
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 7}})
	})
	router.PUT("/layout", controller.SaveLayout)
	payload := bytes.Repeat([]byte("x"), maxDashboardLayoutBody+1)
	result := performDashboardRequest(router, http.MethodPut, "/layout", payload)
	require.Equal(t, http.StatusBadRequest, result.Code)
}

func TestHomeSummaryRunsOnlyRequestedGroups(t *testing.T) {
	app.Response = response.NewResponseHandler()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.GbDevice{}, &models.GbChannel{}))
	controller := NewHomeDashboardController(func() *gorm.DB { return db })
	router := dashboardSummaryRouter(controller)

	result := performDashboardRequest(router, http.MethodGet, "/summary?groups=assets", nil)
	require.Equal(t, http.StatusOK, result.Code, result.Body.String())
	sections := decodeDashboardSections(t, result.Body.Bytes())
	require.Contains(t, sections, "assets")
	require.NotContains(t, sections, "sip")
	require.NotContains(t, sections, "play")
	require.NotContains(t, sections, "traffic")
	require.NotContains(t, sections, "media")
}

func TestHomeSummaryKeepsHealthySectionWhenOtherRepositoriesFail(t *testing.T) {
	app.Response = response.NewResponseHandler()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.GbDevice{}, &models.GbChannel{}))
	controller := NewHomeDashboardController(func() *gorm.DB { return db })
	result := performDashboardRequest(dashboardSummaryRouter(controller), http.MethodGet, "/summary?groups=assets,aggregate", nil)
	require.Equal(t, http.StatusOK, result.Code, result.Body.String())
	sections := decodeDashboardSections(t, result.Body.Bytes())
	require.Equal(t, "empty", sections["assets"].Status)
	require.Equal(t, "unavailable", sections["sip"].Status)
	require.Equal(t, "unavailable", sections["play"].Status)
	require.Equal(t, "unavailable", sections["traffic"].Status)
}

func TestHomeSummaryRejectsUnknownGroupAndInvalidNode(t *testing.T) {
	controller := NewHomeDashboardController(func() *gorm.DB { return nil })
	router := dashboardSummaryRouter(controller)
	unknown := performDashboardRequest(router, http.MethodGet, "/summary?groups=other", nil)
	require.Equal(t, http.StatusBadRequest, unknown.Code)
	invalidNode := performDashboardRequest(router, http.MethodGet, "/summary?nodeId=0", nil)
	require.Equal(t, http.StatusBadRequest, invalidNode.Code)
}

func TestHomeSummaryDoesNotExposeMediaWithoutOverviewPermission(t *testing.T) {
	app.Response = response.NewResponseHandler()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	controller := NewHomeDashboardController(func() *gorm.DB { return db })
	controller.mediaRead = func(*gin.Context) bool { return false }
	result := performDashboardRequest(dashboardSummaryRouter(controller), http.MethodGet, "/summary?groups=realtime", nil)
	require.Equal(t, http.StatusOK, result.Code, result.Body.String())
	sections := decodeDashboardSections(t, result.Body.Bytes())
	require.Equal(t, "forbidden", sections["media"].Status)
}

func TestHomeSummaryReturnsServiceUnavailableWhenDatabaseIsMissing(t *testing.T) {
	controller := NewHomeDashboardController(func() *gorm.DB { return nil })
	result := performDashboardRequest(dashboardSummaryRouter(controller), http.MethodGet, "/summary?groups=assets", nil)
	require.Equal(t, http.StatusServiceUnavailable, result.Code)
}

func TestHomeRuntimeBindingsReturnsPlayingChannelWithDeviceDisplayName(t *testing.T) {
	app.Response = response.NewResponseHandler()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.GbDevice{}, &models.GbChannel{}))
	require.NoError(t, db.Create(&models.GbDevice{DeviceID: "34020000002000000001", Name: "设备原名", Alias: "南门摄像机"}).Error)
	require.NoError(t, db.Create(&models.GbChannel{DeviceID: "34020000002000000001", ChannelID: "34020000001320000001", Name: "通道原名", Alias: "南门通道", StreamID: "0200000000"}).Error)
	require.NoError(t, db.Create(&models.GbChannel{DeviceID: "34020000002000000001", ChannelID: "34020000001320000002", Name: "未播放", StreamID: ""}).Error)

	controller := NewHomeDashboardController(func() *gorm.DB { return db })
	result := performDashboardRequest(dashboardSummaryRouter(controller), http.MethodGet, "/summary?groups=aggregate", nil)
	require.Equal(t, http.StatusOK, result.Code, result.Body.String())
	var body struct {
		Data struct {
			Bindings struct {
				Data []struct {
					StreamID    string `json:"streamId"`
					DeviceName  string `json:"deviceName"`
					ChannelName string `json:"channelName"`
				} `json:"data"`
			} `json:"bindings"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(result.Body.Bytes(), &body))
	bindings := body.Data.Bindings.Data
	require.Len(t, bindings, 1)
	require.Equal(t, "0200000000", bindings[0].StreamID)
	require.Equal(t, "南门摄像机", bindings[0].DeviceName)
	require.Equal(t, "南门通道", bindings[0].ChannelName)
}

func TestHomeDrilldownSIPHistoryRejectsInvalidAndDuplicateRanges(t *testing.T) {
	controller := NewHomeDashboardController(func() *gorm.DB { return nil })
	router := gin.New()
	router.GET("/drilldown/sip", controller.SIPHistory)

	invalid := performDashboardRequest(router, http.MethodGet, "/drilldown/sip?range=30d", nil)
	require.Equal(t, http.StatusBadRequest, invalid.Code)
	duplicate := performDashboardRequest(router, http.MethodGet, "/drilldown/sip?range=1h&range=7d", nil)
	require.Equal(t, http.StatusBadRequest, duplicate.Code)
}

func TestHomeDrilldownPlayHistoryRejectsInvalidRange(t *testing.T) {
	controller := NewHomeDashboardController(func() *gorm.DB { return nil })
	router := gin.New()
	router.GET("/drilldown/play", controller.PlayHistory)
	result := performDashboardRequest(router, http.MethodGet, "/drilldown/play?range=30d", nil)
	require.Equal(t, http.StatusBadRequest, result.Code)
}

func TestHomeDrilldownTrafficHistoryRejectsMinuteRangeAndInvalidPage(t *testing.T) {
	controller := NewHomeDashboardController(func() *gorm.DB { return nil })
	router := gin.New()
	router.GET("/drilldown/traffic", controller.TrafficHistory)
	require.Equal(t, http.StatusBadRequest, performDashboardRequest(router, http.MethodGet, "/drilldown/traffic?range=1h", nil).Code)
	require.Equal(t, http.StatusBadRequest, performDashboardRequest(router, http.MethodGet, "/drilldown/traffic?range=24h&page=0", nil).Code)
}

type dashboardSectionStatus struct {
	Status string `json:"status"`
}

func dashboardSummaryRouter(controller *HomeDashboardController) *gin.Engine {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(consts.BindContextKeyName, &app.Claims{})
		c.Next()
	})
	router.GET("/summary", controller.Summary)
	return router
}

func decodeDashboardSections(t *testing.T, payload []byte) map[string]dashboardSectionStatus {
	t.Helper()
	var responseBody struct {
		Data map[string]dashboardSectionStatus `json:"data"`
	}
	require.NoError(t, json.Unmarshal(payload, &responseBody))
	return responseBody.Data
}

func performDashboardRequest(router http.Handler, method, path string, body []byte) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, request)
	return w
}
