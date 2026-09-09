package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	gbcontrollers "uvplatform.cn/uvp-gb28181/app/gb28181/controllers"
	globalapp "uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
)

type workRecordingRouteLeaseAnswer bool

func (answer workRecordingRouteLeaseAnswer) HasLease(string) bool { return bool(answer) }

func withWorkRecordingRouteClaims(userID uint) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(consts.BindContextKeyName, &globalapp.Claims{ClaimsUser: globalapp.ClaimsUser{UserID: userID}})
		c.Next()
	}
}

func TestWorkRecordingRoutesAreRegisteredAndUnavailableBeforeInjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	SetWorkRecordingController(nil)
	t.Cleanup(func() { SetWorkRecordingController(nil) })

	engine := gin.New()
	engine.Use(withWorkRecordingRouteClaims(1))
	RegisterRoutes(engine.Group("/api"))

	want := map[string]bool{
		"POST /api/gb28181/work-recordings":          false,
		"POST /api/gb28181/work-recordings/:id/stop": false,
		"GET /api/gb28181/work-recordings/status":    false,
		"GET /api/gb28181/work-recordings/:id":       false,
	}
	for _, route := range engine.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for route, found := range want {
		require.True(t, found, route)
	}

	requests := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/gb28181/work-recordings"},
		{http.MethodPost, "/api/gb28181/work-recordings/job/stop"},
		{http.MethodGet, "/api/gb28181/work-recordings/status?channelIds=1"},
		{http.MethodGet, "/api/gb28181/work-recordings/job"},
	}
	for _, request := range requests {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, httptest.NewRequest(request.method, request.path, nil))
		require.Equal(t, http.StatusServiceUnavailable, response.Code, request.method+" "+request.path)
	}
}

func TestSetWorkRecordingControllerUsesAtomicPlaceholderForNil(t *testing.T) {
	controller := gbcontrollers.NewWorkRecordingController(nil)
	SetWorkRecordingController(controller)
	require.Same(t, controller, workRecordingController.Load())

	SetWorkRecordingController(nil)
	placeholder := workRecordingController.Load()
	require.NotNil(t, placeholder)
	require.NotSame(t, controller, placeholder)
}

func TestWorkRecordingSourceLeaseCheckerComposesWithCascadeAndPlan(t *testing.T) {
	sourceLeaseMu.Lock()
	oldCascade := cascadeSourceLeaseChecker
	oldPlan := recordingPlanSourceLeaseChecker
	oldWork := workRecordingSourceLeaseChecker
	sourceLeaseMu.Unlock()
	t.Cleanup(func() {
		SetCascadeSourceLeaseChecker(oldCascade)
		SetRecordingPlanSourceLeaseChecker(oldPlan)
		SetWorkRecordingSourceLeaseChecker(oldWork)
	})

	SetCascadeSourceLeaseChecker(workRecordingRouteLeaseAnswer(true))
	SetRecordingPlanSourceLeaseChecker(workRecordingRouteLeaseAnswer(false))
	SetWorkRecordingSourceLeaseChecker(workRecordingRouteLeaseAnswer(false))
	require.False(t, invokeWorkRecordingNoneReader(t, "cascade-stream"))

	SetCascadeSourceLeaseChecker(workRecordingRouteLeaseAnswer(false))
	SetRecordingPlanSourceLeaseChecker(workRecordingRouteLeaseAnswer(true))
	SetWorkRecordingSourceLeaseChecker(workRecordingRouteLeaseAnswer(false))
	require.False(t, invokeWorkRecordingNoneReader(t, "plan-stream"))

	SetCascadeSourceLeaseChecker(workRecordingRouteLeaseAnswer(false))
	SetRecordingPlanSourceLeaseChecker(workRecordingRouteLeaseAnswer(false))
	SetWorkRecordingSourceLeaseChecker(workRecordingRouteLeaseAnswer(true))
	require.False(t, invokeWorkRecordingNoneReader(t, "work-stream"))

	SetWorkRecordingSourceLeaseChecker(nil)
	SetCascadeSourceLeaseChecker(nil)
	SetRecordingPlanSourceLeaseChecker(nil)
	require.True(t, invokeWorkRecordingNoneReader(t, "no-lease-stream"))
}

func invokeWorkRecordingNoneReader(t *testing.T, stream string) bool {
	t.Helper()
	oldLog := globalapp.ZapLog
	globalapp.ZapLog = zap.NewNop()
	t.Cleanup(func() { globalapp.ZapLog = oldLog })
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"app":"hls","stream":"`+stream+`"}`))
	hookController.OnStreamNoneReader(ctx)
	var payload struct {
		Close bool `json:"close"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
	return payload.Close
}
