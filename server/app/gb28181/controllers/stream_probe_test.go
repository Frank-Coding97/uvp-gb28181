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
	"uvplatform.cn/uvp-gb28181/app/gb28181/streamprobe"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

type fakeProbeService struct{ calls int }

func (f *fakeProbeService) Run(context.Context, string) (*streamprobe.ProbeSnapshot, error) {
	f.calls++
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
	router.POST("/play/:streamId/probe", controller.Run)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/play/hidden/probe", nil))
	require.Equal(t, 0, service.calls)
	require.Equal(t, "流不存在", unmarshal(t, recorder)["message"])
}

func TestStreamProbeControllerReturns503WhenUnconfigured(t *testing.T) {
	controller := gbcontrollers.NewStreamProbeController(nil)
	router := gin.New()
	router.POST("/play/:streamId/probe", controller.Run)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/play/stream/probe", nil))
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
}
