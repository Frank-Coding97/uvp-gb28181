package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestZLMManagementRoutesExposeStableTypedFamilies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	SetZLMManagementController(nil)
	engine := gin.New()
	RegisterRoutes(engine.Group("/api"))

	routes := make(map[string]map[string]bool)
	for _, route := range engine.Routes() {
		if len(route.Path) < len("/api/gb28181/zlm") || route.Path[:len("/api/gb28181/zlm")] != "/api/gb28181/zlm" {
			continue
		}
		if routes[route.Path] == nil {
			routes[route.Path] = make(map[string]bool)
		}
		routes[route.Path][route.Method] = true
	}
	wants := map[string][]string{
		"/api/gb28181/zlm/nodes/probe":                             {http.MethodPost},
		"/api/gb28181/zlm/nodes/:id/restart":                       {http.MethodGet, http.MethodPost},
		"/api/gb28181/zlm/overview":                                {http.MethodGet},
		"/api/gb28181/zlm/streams":                                 {http.MethodGet},
		"/api/gb28181/zlm/nodes/:id/runtime":                       {http.MethodGet},
		"/api/gb28181/zlm/nodes/:id/streams":                       {http.MethodGet},
		"/api/gb28181/zlm/nodes/:id/streams/playback-grant":        {http.MethodPost},
		"/api/gb28181/zlm/nodes/:id/streams/close":                 {http.MethodPost},
		"/api/gb28181/zlm/nodes/:id/streams/force-close":           {http.MethodPost},
		"/api/gb28181/zlm/nodes/:id/sessions/network":              {http.MethodGet},
		"/api/gb28181/zlm/nodes/:id/sessions/kick":                 {http.MethodPost},
		"/api/gb28181/zlm/nodes/:id/proxies/pull":                  {http.MethodGet, http.MethodPost},
		"/api/gb28181/zlm/nodes/:id/ffmpeg-sources":                {http.MethodGet, http.MethodPost},
		"/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key/preflight": {http.MethodPost},
		"/api/gb28181/zlm/nodes/:id/rtp-servers":                   {http.MethodGet},
		"/api/gb28181/zlm/nodes/:id/rtp-servers/close":             {http.MethodPost},
		"/api/gb28181/zlm/nodes/:id/rtp-servers/force-close":       {http.MethodPost},
		"/api/gb28181/zlm/nodes/:id/recordings/runtime/status":     {http.MethodGet},
		"/api/gb28181/zlm/nodes/:id/recordings/runtime/stop":       {http.MethodPost},
		"/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop": {http.MethodPost},
	}
	for path, methods := range wants {
		for _, method := range methods {
			require.Truef(t, routes[path][method], "missing %s %s", method, path)
		}
	}

	// Existing compatibility routes are still registered exactly once; the
	// management facade did not replace or duplicate their handlers.
	require.True(t, routes["/api/gb28181/zlm/nodes"][http.MethodGet])
	require.True(t, routes["/api/gb28181/zlm/nodes/:id/config"][http.MethodGet])
	require.True(t, routes["/api/gb28181/zlm/scheduler"][http.MethodGet])
}

func TestZLMManagementRoutesReturnTyped503BeforeInjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	SetZLMManagementController(nil)
	engine := gin.New()
	RegisterRoutes(engine.Group("/api"))

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/gb28181/zlm/overview", nil))
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"code":"service_unavailable"`)
}
