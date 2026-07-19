package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	gbtrace "uvplatform.cn/uvp-gb28181/app/gb28181/trace"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

type fakeTraceAccess struct{ allowed bool }

func (a fakeTraceAccess) IsSystemAdmin(context.Context, uint) bool { return a.allowed }

type fakeTraceQueryService struct {
	health       gbtrace.HealthSnapshot
	messages     gbtrace.MessagePage
	sessions     []gbtrace.SessionSummary
	detail       gbtrace.MessageDetail
	err          error
	listCalls    int
	detailAccess gbtrace.DisclosureContext
}

func (s *fakeTraceQueryService) Health() gbtrace.HealthSnapshot { return s.health }
func (s *fakeTraceQueryService) ListMessages(context.Context, gbtrace.MessageFilter) (gbtrace.MessagePage, error) {
	s.listCalls++
	return s.messages, s.err
}
func (s *fakeTraceQueryService) GetMessage(_ context.Context, _ string, sensitive bool, audit gbtrace.DisclosureContext) (gbtrace.MessageDetail, error) {
	s.detailAccess = audit
	if sensitive != audit.IsAdmin {
		return gbtrace.MessageDetail{}, errors.New("invalid test disclosure context")
	}
	return s.detail, s.err
}
func (s *fakeTraceQueryService) ListSessions(context.Context, gbtrace.SessionFilter) ([]gbtrace.SessionSummary, error) {
	return s.sessions, s.err
}

func newTraceControllerRouter(service TraceQueryService, access TraceAdminAccess) *gin.Engine {
	gin.SetMode(gin.TestMode)
	app.Response = response.NewResponseHandler()
	ctrl := NewTraceController(service, nil, access)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 7, Username: "operator"}})
		c.Next()
	})
	r.GET("/health", ctrl.Health)
	r.GET("/messages", ctrl.ListMessages)
	r.GET("/messages/:id", ctrl.GetMessage)
	r.GET("/sessions", ctrl.ListSessions)
	return r
}

func decodeTraceResponse(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload), recorder.Body.String())
	return payload
}

func TestTraceControllerRejectsNonAdminBeforeService(t *testing.T) {
	service := &fakeTraceQueryService{health: gbtrace.HealthSnapshot{State: gbtrace.HealthReady}}
	r := newTraceControllerRouter(service, fakeTraceAccess{allowed: false})
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/messages?from=2026-07-19T00:00:00Z&to=2026-07-19T01:00:00Z", nil))

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Zero(t, service.listCalls)
}

func TestTraceControllerValidatesMessageFilters(t *testing.T) {
	service := &fakeTraceQueryService{}
	r := newTraceControllerRouter(service, fakeTraceAccess{allowed: true})
	tests := []string{
		"/messages",
		"/messages?from=bad&to=2026-07-19T01:00:00Z",
		"/messages?from=2026-07-19T00:00:00Z&to=2026-07-29T00:00:00Z",
		"/messages?from=2026-07-19T00:00:00Z&to=2026-07-19T01:00:00Z&direction=sideways",
		"/messages?from=2026-07-19T00:00:00Z&to=2026-07-19T01:00:00Z&callId=ok%0Ainjected",
		"/messages?from=2026-07-19T00:00:00Z&to=2026-07-19T01:00:00Z&cursor=invalid",
	}
	for _, target := range tests {
		recorder := httptest.NewRecorder()
		r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
		require.Equal(t, http.StatusBadRequest, recorder.Code, target)
	}
	require.Zero(t, service.listCalls)
}

func TestTraceControllerReturnsDisabledHealthAndEmptyList(t *testing.T) {
	service := &fakeTraceQueryService{
		health:   gbtrace.DisabledHealth(),
		messages: gbtrace.MessagePage{Items: []gbtrace.MessageSummary{}},
	}
	r := newTraceControllerRouter(service, fakeTraceAccess{allowed: true})

	healthRecorder := httptest.NewRecorder()
	r.ServeHTTP(healthRecorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	require.Equal(t, http.StatusOK, healthRecorder.Code)
	healthData := decodeTraceResponse(t, healthRecorder)["data"].(map[string]any)
	require.Equal(t, "disabled", healthData["state"])

	listRecorder := httptest.NewRecorder()
	r.ServeHTTP(listRecorder, httptest.NewRequest(http.MethodGet, "/messages?from=2026-07-19T00:00:00Z&to=2026-07-19T01:00:00Z", nil))
	require.Equal(t, http.StatusOK, listRecorder.Code)
	listData := decodeTraceResponse(t, listRecorder)["data"].(map[string]any)
	require.Empty(t, listData["items"])
}

func TestTraceControllerDegradedQueryIsExplicit(t *testing.T) {
	service := &fakeTraceQueryService{
		health: gbtrace.HealthSnapshot{State: gbtrace.HealthDegraded, LastError: "clickhouse unavailable"},
		err:    errors.New("query failed"),
	}
	r := newTraceControllerRouter(service, fakeTraceAccess{allowed: true})
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/messages?from=2026-07-19T00:00:00Z&to=2026-07-19T01:00:00Z", nil))

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	payload := decodeTraceResponse(t, recorder)
	data := payload["data"].(map[string]any)
	require.Equal(t, "degraded", data["state"])
}

func TestTraceControllerSensitiveMessageRequiresPurposeAndAudits(t *testing.T) {
	service := &fakeTraceQueryService{detail: gbtrace.MessageDetail{Payload: "Authorization: secret"}}
	r := newTraceControllerRouter(service, fakeTraceAccess{allowed: true})
	id := "4c7bd3c1-93ec-4ff0-8348-d4e6996b9834"

	missingPurpose := httptest.NewRecorder()
	r.ServeHTTP(missingPurpose, httptest.NewRequest(http.MethodGet, "/messages/"+id+"?sensitive=true", nil))
	require.Equal(t, http.StatusBadRequest, missingPurpose.Code)

	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/messages/"+id+"?sensitive=true&purpose=incident-42", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, service.detailAccess.IsAdmin)
	require.Equal(t, "7", service.detailAccess.UserID)
	require.Equal(t, "incident-42", service.detailAccess.Purpose)
}

func TestTraceControllerValidatesSessionWindow(t *testing.T) {
	service := &fakeTraceQueryService{sessions: []gbtrace.SessionSummary{}}
	r := newTraceControllerRouter(service, fakeTraceAccess{allowed: true})
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/sessions?from=2026-01-01T00:00:00Z&to=2026-07-19T00:00:00Z", nil))
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}
