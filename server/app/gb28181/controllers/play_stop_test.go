package controllers_test

import (
	"context"
	"errors"
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
	stopErr   error
	startErr  error
	result    *play.Result
}

func (s *stopTestPlayService) Start(context.Context, string, string) (*play.Result, error) {
	return s.result, s.startErr
}

func TestPlayControllerStartMapsGlobalTimeoutToGatewayTimeout(t *testing.T) {
	service := &stopTestPlayService{startErr: play.ErrPlayTimeout}
	router := buildStopRouter(t, service)
	controller := gbcontrollers.NewPlayController(service)
	router.POST("/start/:deviceId/:channelId", controller.Start)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/start/device/channel", nil))

	require.Equal(t, http.StatusGatewayTimeout, recorder.Code, recorder.Body.String())
	body := unmarshal(t, recorder)
	assert.Equal(t, "点播超时", body["message"])
}

func (s *stopTestPlayService) Stop(context.Context, string) error {
	s.stopCalls.Add(1)
	return s.stopErr
}

type stopTestRecordingStarter struct {
	beginCalls atomic.Int32
	streamID   atomic.Value
}

func (l *stopTestRecordingStarter) BeginPlayback(_ context.Context, streamID string) error {
	l.streamID.Store(streamID)
	l.beginCalls.Add(1)
	return nil
}

// buildStopRouter 装配一个仅挂 Stop 路由的最小 router,seed 一条 dept10 的通道
// 用于 stopPlay 主路径断言。
func buildStopRouter(t *testing.T, svc gbcontrollers.PlayService, opts ...gbcontrollers.PlayControllerOption) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := newScopedDeviceDB(t)
	app.Response = response.NewResponseHandler()
	require.NoError(t, db.Create(&gbmodels.GbChannel{DeviceID: "device", ChannelID: "channel", StreamID: "dept10-stream"}).Error)
	controller := gbcontrollers.NewPlayController(svc, opts...)
	router := gin.New()
	router.Use(gin.Recovery())
	router.DELETE("/play/:streamId", controller.Stop)
	return router
}

func serveStop(router *gin.Engine, streamID string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/play/"+streamID, nil))
	return recorder
}

func TestPlayControllerStartBeginsRecordingForReturnedStream(t *testing.T) {
	service := &stopTestPlayService{result: &play.Result{StreamID: "dept10-stream"}}
	starter := &stopTestRecordingStarter{}
	router := buildStopRouter(t, service)
	controller := gbcontrollers.NewPlayController(service, gbcontrollers.WithPlaybackRecordingStarter(starter))
	router.POST("/start/:deviceId/:channelId", controller.Start)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/start/device/channel", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.EqualValues(t, 1, starter.beginCalls.Load())
	assert.Equal(t, "dept10-stream", starter.streamID.Load())
}

func TestPlayControllerStopDelegatesLifecycleCleanupToPlayService(t *testing.T) {
	service := &stopTestPlayService{}
	router := buildStopRouter(t, service)

	recorder := serveStop(router, "dept10-stream")

	require.Equal(t, http.StatusOK, recorder.Code)
	body := unmarshal(t, recorder)
	assert.EqualValues(t, 0, body["code"])
	assert.Equal(t, "已停止直播", body["message"])
	data, ok := body["data"].(map[string]any)
	require.True(t, ok, "response.data 应该是对象: %v", body["data"])
	assert.Equal(t, true, data["released"])
	assert.Equal(t, "dept10-stream", data["streamId"])
	assert.EqualValues(t, 1, service.stopCalls.Load())
}

func TestPlayControllerStopReleasesUnrecordedStream(t *testing.T) {
	service := &stopTestPlayService{}
	router := buildStopRouter(t, service)

	recorder := serveStop(router, "dept10-stream")

	require.Equal(t, http.StatusOK, recorder.Code)
	body := unmarshal(t, recorder)
	assert.EqualValues(t, 0, body["code"])
	assert.Equal(t, "已停止直播", body["message"])
	data, ok := body["data"].(map[string]any)
	require.True(t, ok, "response.data 应该是对象: %v", body["data"])
	assert.Equal(t, true, data["released"])
	assert.Equal(t, "dept10-stream", data["streamId"])
	assert.EqualValues(t, 1, service.stopCalls.Load())
}

func TestPlayControllerStopReturnsErrorWhenServiceFails(t *testing.T) {
	service := &stopTestPlayService{stopErr: errors.New("bye 失败")}
	router := buildStopRouter(t, service)

	recorder := serveStop(router, "dept10-stream")

	body := unmarshal(t, recorder)
	assert.NotEqualValues(t, 0, body["code"], "svc.Stop 报错时 code 不能为 0: %v", body)
	assert.Contains(t, body["message"], "停止流失败")
	assert.EqualValues(t, 1, service.stopCalls.Load())
}

func TestPlayControllerStopWithoutPolicyReleases(t *testing.T) {
	service := &stopTestPlayService{}
	router := buildStopRouter(t, service)

	recorder := serveStop(router, "dept10-stream")

	require.Equal(t, http.StatusOK, recorder.Code)
	body := unmarshal(t, recorder)
	assert.EqualValues(t, 0, body["code"])
	assert.Equal(t, "已停止直播", body["message"])
	data, ok := body["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, data["released"])
	assert.EqualValues(t, 1, service.stopCalls.Load())
}
