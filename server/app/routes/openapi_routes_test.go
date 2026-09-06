package routes

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type openAPIRootConfig struct {
	app.YmlConfigInterf
	staticDir string
	enabled   bool
}
type openAPIRootCasbin struct{ app.CasbinInterf }

func (openAPIRootCasbin) CasbinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) { c.AbortWithStatus(403) }
}

type openAPIRootTokens struct{ app.TokenServiceInterface }
type openAPIRootSessions struct{ app.SessionValidatorInterface }

func (c openAPIRootConfig) GetString(key string) string {
	switch key {
	case "httpserver.serverrootpath":
		return "/__static_test"
	case "httpserver.serverroot":
		return c.staticDir
	default:
		return ""
	}
}
func (c openAPIRootConfig) GetBool(key string) bool {
	if key == "openapi.enabled" {
		return c.enabled
	}
	return key == "httpserver.allowcrossdomain"
}
func (c openAPIRootConfig) GetInt(string) int              { return 0 }
func (c openAPIRootConfig) GetStringSlice(string) []string { return nil }

func TestOpenAPIRootDefaultClosedAndIsolatedFromCORS(t *testing.T) {
	old := app.ConfigYml
	oldDB := app.GormDbMysql
	app.GormDbMysql = openAPISecurityDB(t)
	seedOpenAPISecurity(t, app.GormDbMysql)
	oldCasbin, oldTokens, oldSessions := app.CasbinV2, app.TokenService, app.SessionValidator
	app.CasbinV2, app.TokenService, app.SessionValidator = openAPIRootCasbin{}, openAPIRootTokens{}, openAPIRootSessions{}
	app.ConfigYml = openAPIRootConfig{staticDir: t.TempDir()}
	t.Cleanup(func() {
		app.ConfigYml = old
		app.GormDbMysql = oldDB
		app.CasbinV2, app.TokenService, app.SessionValidator = oldCasbin, oldTokens, oldSessions
	})
	gin.SetMode(gin.TestMode)
	root := gin.New()
	InitRoutes(root)
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/openapi/v1/devices", 503},
		{"OPTIONS", "/openapi/v1/devices", 405},
		{"GET", "/openapi/v1/devices/", 404},
		{"CUSTOM", "/openapi/v1/devices", 405},
	} {
		out := httptest.NewRecorder()
		request := httptest.NewRequest(tc.method, tc.path, nil)
		request.Header.Set("Origin", "https://untrusted.example")
		root.ServeHTTP(out, request)
		require.Equal(t, tc.status, out.Code, out.Body.String())
		require.Empty(t, out.Header().Get("Access-Control-Allow-Origin"))
	}
	for _, route := range root.Routes() {
		require.False(t, strings.HasPrefix(route.Path, "/openapi"), "only the private router may own this namespace")
	}
	out := httptest.NewRecorder()
	root.ServeHTTP(out, httptest.NewRequest("GET", "/api/gb28181/openapi-clients", nil))
	require.Contains(t, []int{401, 403}, out.Code, "management must remain JWT protected")
}
