package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	"uvplatform.cn/uvp-gb28181/app/gb28181/play"
	"uvplatform.cn/uvp-gb28181/app/middleware"
)

type fixedAuthorizationControllerService struct {
	calls atomic.Int32
}

func (*fixedAuthorizationControllerService) Start(context.Context, string, string) (*play.Result, error) {
	return nil, nil
}

func (*fixedAuthorizationControllerService) StartAuthorized(context.Context, string, string, string) (*play.Result, error) {
	return &play.Result{
		StreamID: "dynamic-stream", App: "rtp",
		Node: &play.ResultNode{ID: 7}, AuthorizationExpiresAt: 1_800_000_120,
		AuthorizationCorrelationID: "corr-dynamic",
	}, nil
}

func (*fixedAuthorizationControllerService) Stop(context.Context, string) error { return nil }

func (s *fixedAuthorizationControllerService) AuthorizeFixedPlayback(context.Context, string, string, string) (*play.Result, error) {
	s.calls.Add(1)
	return &play.Result{
		StreamID: "34020000002000000010_37011200001310000010",
		App:      "rtp", Node: &play.ResultNode{ID: 9}, AuthorizationExpiresAt: 1_800_000_120,
		AuthorizationCorrelationID: "corr-fixed",
	}, nil
}

func newFixedAuthorizationControllerRouter(
	t *testing.T,
	userID uint,
	service *fixedAuthorizationControllerService,
	auditCapture ...*map[string]any,
) *gin.Engine {
	t.Helper()
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, userID, 10)
	seedScopedDeviceRows(t, db)
	controller := gbcontrollers.NewPlayController(service)
	router := gin.New()
	router.Use(gin.Recovery(), withClaims(userID))
	if len(auditCapture) > 0 && auditCapture[0] != nil {
		router.Use(func(c *gin.Context) {
			c.Next()
			metadata, _ := middleware.SensitiveOperationMetadata(c)
			*auditCapture[0] = metadata
		})
	}
	router.POST("/api/gb28181/play/:deviceId/:channelId", controller.Start)
	router.POST("/api/gb28181/play/:deviceId/:channelId/authorization", controller.Authorize)
	return router
}

func TestPlayControllerAuthorizeChecksChannelScopeBeforeService(t *testing.T) {
	service := &fixedAuthorizationControllerService{}
	router := newFixedAuthorizationControllerRouter(t, 100, service)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost,
		"/api/gb28181/play/34020000002000000020/37011200001310000020/authorization", nil)
	router.ServeHTTP(response, request)

	body := unmarshal(t, response)
	require.EqualValues(t, 1, body["code"])
	require.Equal(t, "通道不存在", body["message"])
	require.Zero(t, service.calls.Load())
}

func TestPlayControllerAuthorizeReturnsPreauthorizedFixedURLs(t *testing.T) {
	service := &fixedAuthorizationControllerService{}
	var audit map[string]any
	router := newFixedAuthorizationControllerRouter(t, 100, service, &audit)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost,
		"/api/gb28181/play/34020000002000000010/37011200001310000010/authorization", nil)
	request.RemoteAddr = "203.0.113.9:12345"
	router.ServeHTTP(response, request)

	body := unmarshal(t, response)
	require.EqualValues(t, 0, body["code"])
	require.EqualValues(t, 1, service.calls.Load())
	data := body["data"].(map[string]interface{})
	require.Equal(t, "34020000002000000010_37011200001310000010", data["streamId"])
	require.EqualValues(t, 1_800_000_120, data["authorizationExpiresAt"])
	require.Equal(t, "fixed_play_authorization_issue", audit["action"])
	require.Equal(t, "issued", audit["result"])
	require.EqualValues(t, 100, audit["userId"])
	require.Equal(t, "34020000002000000010", audit["deviceId"])
	require.Equal(t, "37011200001310000010", audit["channelId"])
	require.EqualValues(t, 9, audit["nodeId"])
	require.Equal(t, "corr-fixed", audit["correlationId"])
	encoded, err := json.Marshal(audit)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "play_token")
	require.NotContains(t, string(encoded), "http://")
}

func TestPlayControllerStartAuditsDynamicAuthorization(t *testing.T) {
	service := &fixedAuthorizationControllerService{}
	var audit map[string]any
	router := newFixedAuthorizationControllerRouter(t, 100, service, &audit)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost,
		"/api/gb28181/play/34020000002000000010/37011200001310000010", nil)
	router.ServeHTTP(response, request)

	body := unmarshal(t, response)
	require.EqualValues(t, 0, body["code"])
	require.Equal(t, "realtime_play_authorization_issue", audit["action"])
	require.Equal(t, "issued", audit["result"])
	require.EqualValues(t, 7, audit["nodeId"])
	require.Equal(t, "corr-dynamic", audit["correlationId"])
}
