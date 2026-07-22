package controllers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

type stopTestPlayService struct {
	stopCalls atomic.Int32
}

func (s *stopTestPlayService) Start(context.Context, string, string) (*play.Result, error) {
	return nil, nil
}

func (s *stopTestPlayService) Stop(context.Context, string) error {
	s.stopCalls.Add(1)
	return nil
}

type stopTestRetentionPolicy struct {
	keep bool
}

func (p stopTestRetentionPolicy) ShouldKeepStream(context.Context, string) (bool, error) {
	return p.keep, nil
}

func TestPlayControllerStopKeepsCloudRecordingStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedDeviceDB(t)
	app.Response = response.NewResponseHandler()
	require.NoError(t, db.Create(&gbmodels.GbChannel{DeviceID: "device", ChannelID: "channel", StreamID: "dept10-stream"}).Error)
	service := &stopTestPlayService{}
	controller := gbcontrollers.NewPlayController(service,
		gbcontrollers.WithStreamRetentionPolicy(stopTestRetentionPolicy{keep: true}))
	router := gin.New()
	router.Use(gin.Recovery())
	router.DELETE("/play/:streamId", controller.Stop)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/play/dept10-stream", nil))

	require.Equal(t, http.StatusOK, response.Code)
	body := unmarshal(t, response)
	assert.EqualValues(t, 0, body["code"])
	assert.Equal(t, "已停止观看", body["message"])
	assert.EqualValues(t, 0, service.stopCalls.Load())
}

func TestPlayControllerStopReleasesUnrecordedStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newScopedDeviceDB(t)
	app.Response = response.NewResponseHandler()
	require.NoError(t, db.Create(&gbmodels.GbChannel{DeviceID: "device", ChannelID: "channel", StreamID: "dept10-stream"}).Error)
	service := &stopTestPlayService{}
	controller := gbcontrollers.NewPlayController(service,
		gbcontrollers.WithStreamRetentionPolicy(stopTestRetentionPolicy{keep: false}))
	router := gin.New()
	router.Use(gin.Recovery())
	router.DELETE("/play/:streamId", controller.Stop)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/play/dept10-stream", nil))

	require.Equal(t, http.StatusOK, response.Code)
	body := unmarshal(t, response)
	assert.EqualValues(t, 0, body["code"])
	assert.Equal(t, "已停止观看", body["message"])
	assert.EqualValues(t, 0, service.stopCalls.Load())
}
