package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
)

type developerCapabilityConfig struct {
	app.YmlConfigInterf
	dialect string
}

func (c developerCapabilityConfig) GetString(key string) string {
	if key == "gormv2.usedbtype" {
		return c.dialect
	}
	return ""
}

func TestSQLiteDeveloperRoutesRejectBeforeControllerWork(t *testing.T) {
	old := app.ConfigYml
	app.ConfigYml = developerCapabilityConfig{dialect: "sqlite"}
	t.Cleanup(func() { app.ConfigYml = old })
	engine := gin.New()
	group := engine.Group("/api", func(c *gin.Context) {
		c.Set(consts.BindContextKeyName, &app.Claims{ClaimsUser: app.ClaimsUser{UserID: 42}})
		c.Next()
	})
	registerDeveloperRoutes(group)
	// Use every registered production endpoint, including malformed bodies. The
	// capability decision must happen before DB, archive or filesystem access.
	for _, route := range engine.Routes() {
		t.Run(route.Method+route.Path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, httptest.NewRequest(route.Method, route.Path, nil))
			require.Equal(t, http.StatusNotImplemented, recorder.Code, recorder.Body.String())
			require.JSONEq(t, `{"code":1,"message":"Windows 单机版不支持开发代码生成及插件结构管理","data":null}`, recorder.Body.String())
		})
	}
	require.Len(t, engine.Routes(), 16)
}

func TestDeveloperCapabilityPreservesServerDialects(t *testing.T) {
	old := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = old })
	for _, dialect := range []string{"mysql", "postgresql", "sqlserver"} {
		t.Run(dialect, func(t *testing.T) {
			app.ConfigYml = developerCapabilityConfig{dialect: dialect}
			engine := gin.New()
			engine.GET("/api/codegen/databases", requireDeveloperTools(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/codegen/databases", nil))
			require.Equal(t, http.StatusNoContent, recorder.Code)
		})
	}
}

func (developerCapabilityConfig) GetBool(string) bool            { return false }
func (developerCapabilityConfig) GetInt(string) int              { return 0 }
func (developerCapabilityConfig) GetStringSlice(string) []string { return nil }

type developerRouteCasbin struct{ app.CasbinInterf }

func (developerRouteCasbin) CasbinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) { c.Next() }
}

func TestStandalonePublishedRoutesKeepAuthAndUnpublishedAPIsAbsent(t *testing.T) {
	oldConfig, oldCasbin, oldToken, oldSession := app.ConfigYml, app.CasbinV2, app.TokenService, app.SessionValidator
	app.ConfigYml = developerCapabilityConfig{dialect: "sqlite"}
	app.CasbinV2 = developerRouteCasbin{}
	app.TokenService, app.SessionValidator = nil, nil
	t.Cleanup(func() {
		app.ConfigYml, app.CasbinV2, app.TokenService, app.SessionValidator = oldConfig, oldCasbin, oldToken, oldSession
	})
	engine := gin.New()
	InitRoutes(engine)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/codegen/databases", nil))
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code, "authentication must run before capability handling")
	require.Contains(t, recorder.Body.String(), "认证会话服务不可用")
	for _, path := range []string{"/api/openapi/v1/devices", "/openapi/v1/devices", "/swagger/index.html", "/viewCache"} {
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusNotFound, recorder.Code, path)
	}
}
