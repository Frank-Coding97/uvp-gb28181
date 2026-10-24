package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/utils/ymlconfig"
)

func TestCorsNextAllowsPlaybackIdempotencyHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(CorsNext())
	router.POST("/playback", func(c *gin.Context) { c.Status(http.StatusOK) })

	request := httptest.NewRequest(http.MethodOptions, "/playback", nil)
	request.Header.Set("Origin", "http://localhost:5177")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "idempotency-key,content-type")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for preflight, got %d", recorder.Code)
	}
	allowed := recorder.Header().Get("Access-Control-Allow-Headers")
	if !strings.Contains(strings.ToLower(allowed), "idempotency-key") {
		t.Fatalf("expected idempotency-key in allowed headers, got %q", allowed)
	}
}

func corsRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(CorsNext())
	router.GET("/probe", func(c *gin.Context) { c.Status(http.StatusOK) })
	return router
}

func corsRequest(t *testing.T, router *gin.Engine, origin string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/probe", nil)
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

// 回显具体 Origin，且**绝不**再出现 `*` —— `*` 与 Allow-Credentials: true 并存会被浏览器整段拒绝。
func TestCorsNextEchoesOriginInsteadOfWildcard(t *testing.T) {
	recorder := corsRequest(t, corsRouter(t), "https://ops.example.com")

	require.Equal(t, "https://ops.example.com", recorder.Header().Get("Access-Control-Allow-Origin"))
	require.Equal(t, "true", recorder.Header().Get("Access-Control-Allow-Credentials"))
	require.Contains(t, recorder.Header().Get("Vary"), "Origin")
}

// 没有 Origin（同源请求、curl、服务端调用）时不该发 CORS 头：CORS 只对浏览器有意义。
func TestCorsNextOmitsCorsHeadersWithoutOrigin(t *testing.T) {
	recorder := corsRequest(t, corsRouter(t), "")

	require.Empty(t, recorder.Header().Get("Access-Control-Allow-Origin"))
	require.Empty(t, recorder.Header().Get("Access-Control-Allow-Credentials"))
	require.Contains(t, recorder.Header().Get("Vary"), "Origin")
}

// `Origin: null` 来自沙箱 iframe / file:// 页面，是"任意本地页面"的公共标识。
func TestCorsNextRejectsNullOrigin(t *testing.T) {
	recorder := corsRequest(t, corsRouter(t), "null")

	require.Empty(t, recorder.Header().Get("Access-Control-Allow-Origin"))
	require.Empty(t, recorder.Header().Get("Access-Control-Allow-Credentials"))
}

// 配置了 httpserver.corsallowedorigins 之后，只有名单内的来源能跨域。
func TestCorsNextHonorsConfiguredOriginWhitelist(t *testing.T) {
	oldConfig := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = oldConfig })

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yml"),
		[]byte("httpserver:\n  corsallowedorigins:\n    - \"https://ops.example.com\"\n"), 0600))
	app.ConfigYml = ymlconfig.CreateYamlFactory(dir)

	router := corsRouter(t)
	allowed := corsRequest(t, router, "https://ops.example.com")
	require.Equal(t, "https://ops.example.com", allowed.Header().Get("Access-Control-Allow-Origin"))

	blocked := corsRequest(t, router, "https://evil.example")
	require.Empty(t, blocked.Header().Get("Access-Control-Allow-Origin"),
		"白名单外的来源必须拿不到 ACAO，否则白名单形同虚设")
	require.Empty(t, blocked.Header().Get("Access-Control-Allow-Credentials"))
}
