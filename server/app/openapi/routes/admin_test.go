package routes

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/openapi/controllers"
)

func TestOpenAPIAdminExplicitProtectedRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/api")
	group.Use(func(c *gin.Context) { c.Header("X-Protected-Group", "yes"); c.Next() })
	RegisterAdminRoutes(group, controllers.NewClientAdminController(nil, nil, nil, nil))
	expected := map[string]bool{
		"GET /api/gb28181/openapi-clients":                       true,
		"POST /api/gb28181/openapi-clients":                      true,
		"GET /api/gb28181/openapi-clients/capabilities":          true,
		"GET /api/gb28181/openapi-clients/capabilities/catalog":  true,
		"GET /api/gb28181/openapi-clients/:id":                   true,
		"PUT /api/gb28181/openapi-clients/:id/scopes":            true,
		"POST /api/gb28181/openapi-clients/:id/rotate-secret":    true,
		"POST /api/gb28181/openapi-clients/:id/enable":           true,
		"POST /api/gb28181/openapi-clients/:id/disable":          true,
		"POST /api/gb28181/openapi-clients/:id/revoke":           true,
		"GET /api/gb28181/openapi-clients/:id/audits":            true,
		"GET /api/gb28181/openapi-clients/:id/revocation-status": true,
	}
	require.Len(t, router.Routes(), len(expected))
	for _, route := range router.Routes() {
		require.True(t, expected[route.Method+" "+route.Path], route.Path)
		out := httptest.NewRecorder()
		router.ServeHTTP(out, httptest.NewRequest(route.Method, strings.ReplaceAll(route.Path, ":id", "1"), nil))
		require.Equal(t, 403, out.Code, route.Path)
		require.Equal(t, "yes", out.Header().Get("X-Protected-Group"))
		require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
	}
}
