package controllers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/streammonitor"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

type fakeStreamMonitor struct {
	calls int
}

func (f *fakeStreamMonitor) Get(context.Context, string) (*streammonitor.Snapshot, error) {
	f.calls++
	return &streammonitor.Snapshot{StreamID: "dept10-stream", Status: streammonitor.StatusOnline}, nil
}

func TestStreamMonitorControllerHidesOtherDepartmentStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	require.NoError(t, db.Create(&gbmodels.GbChannel{DeviceID: "device", ChannelID: "channel", StreamID: "dept20-stream", OwnerDeptID: 20}).Error)
	service := &fakeStreamMonitor{}
	controller := gbcontrollers.NewStreamMonitorController(service)
	controller.SetDB(func() *gorm.DB { return db })
	app.Response = response.NewResponseHandler()

	router := gin.New()
	router.Use(gin.Recovery(), withClaims(100))
	router.GET("/play/:streamId/monitor", controller.Get)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/play/dept20-stream/monitor", nil))

	require.Equal(t, 0, service.calls)
	require.Equal(t, "流不存在", unmarshal(t, recorder)["message"])
}

func TestStreamMonitorControllerReturns503WhenUnconfigured(t *testing.T) {
	controller := gbcontrollers.NewStreamMonitorController(nil)
	router := gin.New()
	router.GET("/play/:streamId/monitor", controller.Get)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/play/stream/monitor", nil))
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
}
