package controllers_test

import (
	"context"
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
	request   play.AuthorizedRequest
	authorize func(play.AuthorizedRequest) error
}

type lifecycleBeginStore struct {
	id       string
	beginErr error
	calls    atomic.Int32
}

func (store *lifecycleBeginStore) Begin(context.Context, uint, string, string) (string, error) {
	store.calls.Add(1)
	return store.id, store.beginErr
}

func (*fixedAuthorizationControllerService) Start(context.Context, string, string) (*play.Result, error) {
	return nil, nil
}

func (s *fixedAuthorizationControllerService) StartAuthorized(_ context.Context, req play.AuthorizedRequest) (*play.Result, error) {
	s.request = req
	if s.authorize != nil {
		if err := s.authorize(req); err != nil {
			return nil, err
		}
	}
	return &play.Result{
		StreamID: "dynamic-stream", App: "rtp",
		Node: &play.ResultNode{ID: 7}, AuthorizationExpiresAt: 1_800_000_120,
		AuthorizationCorrelationID: "corr-dynamic",
	}, nil
}

func (*fixedAuthorizationControllerService) Stop(context.Context, string, string, string) error {
	return nil
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
	return router
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

func TestPlayControllerStartPropagatesLifecycleID(t *testing.T) {
	db := newScopedDeviceDB(t)
	seedDeptScopedUser(t, db, 100, 10)
	seedScopedDeviceRows(t, db)
	service := &fixedAuthorizationControllerService{}
	store := &lifecycleBeginStore{id: "lifecycle-1"}
	controller := gbcontrollers.NewPlayController(service, gbcontrollers.WithPlayLifecycleBeginner(store))
	router := gin.New()
	router.Use(gin.Recovery(), withClaims(100))
	router.POST("/api/gb28181/play/:deviceId/:channelId", controller.Start)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost,
		"/api/gb28181/play/34020000002000000010/37011200001310000010", nil)
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.EqualValues(t, 1, store.calls.Load())
	require.Equal(t, "lifecycle-1", service.request.LifecycleID)
	data := unmarshal(t, response)["data"].(map[string]any)
	require.Equal(t, "lifecycle-1", data["lifecycleId"])
}
