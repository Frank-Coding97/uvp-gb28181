package routes

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAPIPublicBoundaryRejectsWithoutRedirectOrGlobalMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := gin.New()
	require.NoError(t, InstallPublicBoundary(root, nil, nil))
	globalCalls := 0
	root.Use(func(c *gin.Context) { globalCalls++; c.Next() })
	root.GET("/internal", func(c *gin.Context) { c.Status(204) })
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/openapi/v1/devices", 503},
		{"GET", "/openapi/v1/devices/", 404},
		{"GET", "/openapi/v1//devices", 404},
		{"GET", "/openapi/v1/%64evices", 400},
		{"GET", "/openapi", 404},
		{"HEAD", "/openapi/v1/devices", 405},
		{"OPTIONS", "/openapi/v1/devices", 405},
		{"PATCH", "/openapi/v1/devices", 405},
		{"CUSTOM", "/openapi/v1/devices", 405},
		{"POST", "/openapi/v1/devices/34020000002000000010/channels/34020000001320000010/live-authorizations", 503},
	} {
		out := httptest.NewRecorder()
		root.ServeHTTP(out, httptest.NewRequest(tc.method, tc.path, nil))
		require.Equal(t, tc.status, out.Code, tc.method+" "+tc.path+" "+out.Body.String())
		require.Empty(t, out.Header().Get("Location"))
	}
	require.Zero(t, globalCalls)
	out := httptest.NewRecorder()
	root.ServeHTTP(out, httptest.NewRequest("GET", "/internal/", nil))
	require.Equal(t, 301, out.Code, "backend redirect semantics must remain unchanged")
	out = httptest.NewRecorder()
	root.ServeHTTP(out, httptest.NewRequest("GET", "/internal", nil))
	require.Equal(t, 204, out.Code)
	require.Equal(t, 1, globalCalls)
}
