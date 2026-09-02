package controllers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

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

func performDashboardRequest(router http.Handler, method, path string, body []byte) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, request)
	return w
}
