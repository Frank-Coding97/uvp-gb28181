package controllers_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/streamprobe"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

type fakeProbeService struct {
	calls      int
	durationMS []int
}

func (f *fakeProbeService) Run(_ context.Context, _ string, durationMS int) (*streamprobe.ProbeSnapshot, error) {
	f.calls++
	f.durationMS = append(f.durationMS, durationMS)
	return &streamprobe.ProbeSnapshot{}, nil
}

func TestStreamProbeControllerScopesStream(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	require.NoError(t, db.Create(&gbmodels.GbChannel{DeviceID: "d", ChannelID: "c", StreamID: "hidden", OwnerDeptID: 20}).Error)
	service := &fakeProbeService{}
	controller := gbcontrollers.NewStreamProbeController(service)
	controller.SetDB(func() *gorm.DB { return db })
	app.Response = response.NewResponseHandler()
	router := gin.New()
	router.Use(gin.Recovery(), withClaims(100))
	router.POST("/stream-probes/:streamId", controller.Run)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/stream-probes/hidden", nil))
	require.Equal(t, 0, service.calls)
	require.Equal(t, "流不存在", unmarshal(t, recorder)["message"])
}

func TestStreamProbeControllerAcceptsSupportedDurationAndDefaults(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	require.NoError(t, db.Create(&gbmodels.GbChannel{DeviceID: "d", ChannelID: "c", StreamID: "visible", OwnerDeptID: 10}).Error)
	service := &fakeProbeService{}
	controller := gbcontrollers.NewStreamProbeController(service)
	controller.SetDB(func() *gorm.DB { return db })
	app.Response = response.NewResponseHandler()
	router := gin.New()
	router.Use(gin.Recovery(), withClaims(100))
	router.POST("/stream-probes/:streamId", controller.Run)

	for _, test := range []struct {
		name string
		body string
		want int
	}{
		{name: "default", body: "", want: streamprobe.DefaultDurationMS},
		{name: "three seconds", body: `{"durationMs":3000}`, want: 3000},
		{name: "ten seconds", body: `{"durationMs":10000}`, want: 10000},
		{name: "sixty seconds", body: `{"durationMs":60000}`, want: 60000},
	} {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/stream-probes/visible", bytes.NewBufferString(test.body))
		if test.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		router.ServeHTTP(recorder, req)
		require.Equal(t, http.StatusOK, recorder.Code, test.name)
		require.Equal(t, test.want, service.durationMS[len(service.durationMS)-1], test.name)
	}
}

func TestStreamProbeControllerRejectsUnsupportedDuration(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	require.NoError(t, db.Create(&gbmodels.GbChannel{DeviceID: "d", ChannelID: "c", StreamID: "visible", OwnerDeptID: 10}).Error)
	service := &fakeProbeService{}
	controller := gbcontrollers.NewStreamProbeController(service)
	controller.SetDB(func() *gorm.DB { return db })
	app.Response = response.NewResponseHandler()
	router := gin.New()
	router.Use(gin.Recovery(), withClaims(100))
	router.POST("/stream-probes/:streamId", controller.Run)

	for _, body := range []string{`{"durationMs":0}`, `{"durationMs":5000}`, `{"durationMs":60001}`, `{invalid`} {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/stream-probes/visible", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, req)
		require.Equal(t, http.StatusBadRequest, recorder.Code, body)
	}
	require.Equal(t, 0, service.calls)
}

func TestStreamProbeControllerReturns503WhenUnconfigured(t *testing.T) {
	controller := gbcontrollers.NewStreamProbeController(nil)
	router := gin.New()
	router.POST("/stream-probes/:streamId", controller.Run)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/stream-probes/stream", nil))
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
}
