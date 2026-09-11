package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

// routes 包的测试默认不经过 bootstrap,而 controller 的 Fail 会调 app.ZapLog /
// app.Response —— 未初始化时 nil panic。中间件边界用例会真穿透到 handler,
// 所以这里补上最小初始化。
func init() {
	if app.ZapLog == nil {
		app.ZapLog = zap.NewNop()
	}
	if app.Response == nil {
		app.Response = response.NewResponseHandler()
	}
}

// T3 — 扫码接入的路由中间件边界.
//
// 光断言"路由表里有这条路径"证明不了鉴权边界 —— 必须验证 exchange 真的没挂
// JWT/Casbin,而 token 生成真的挂了。否则设备端会在联调时撞 401,或者反过来
// 免鉴权端点意外暴露在 protected 组之外的地方。
// spec: wiki/projects/uvp-gb28181/specs/qr-sip-provisioning.md §2.1

func routeSet(engine *gin.Engine) map[string]bool {
	got := make(map[string]bool)
	for _, route := range engine.Routes() {
		got[route.Method+" "+route.Path] = true
	}
	return got
}

// token 生成端点注册在 protected 组
func TestRegisterRoutes_IncludesQRTokenEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterRoutes(engine.Group("/api"))

	got := routeSet(engine)
	require.True(t, got["POST /api/gb28181/sip/qr/token"], "token 生成端点必须注册")
}

// 3.5 exchange 不得出现在 protected 组 —— 关键回归防线
//
// 若它被误注册进 protected,设备端(无登录态)会撞 401,而这个故障在真机联调时
// 表现为"扫码后一直转圈",很难定位。
func TestRegisterRoutes_ExcludesQRExchangeFromProtected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterRoutes(engine.Group("/api"))

	got := routeSet(engine)
	require.False(t, got["POST /api/gb28181/sip/qr/exchange"],
		"兑换端点免鉴权,不能注册在 protected 组")
	require.False(t, got["GET /gb28181/qr"],
		"引导页挂 engine 根,不属于 protected 组")
}

// exchange 注册在 public 组,且路径仍带 /api 前缀
func TestRegisterPublicRoutes_IncludesQRExchange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterPublicRoutes(engine.Group("/api"))

	got := routeSet(engine)
	require.True(t, got["POST /api/gb28181/sip/qr/exchange"],
		"兑换端点必须注册在 public 组,且保留 /api 前缀")
}

// 引导页挂 engine 根,不带 /api 前缀
func TestRegisterQRLandingRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterQRLandingRoute(engine)

	got := routeSet(engine)
	require.True(t, got["GET /gb28181/qr"], "引导页挂在 engine 根")
}

// 3.1 / 3.2 中间件边界真实生效.
//
// 用一个会拒绝所有请求的哨兵中间件模拟 JWT/Casbin:挂在 protected 组上,
// 然后验证 public 组的 exchange 不受影响。这验证的是 Gin"中间件按组挂载
// 而非路径前缀匹配"这一前提 —— 整个 public 组方案的地基。
func TestQRRoutes_MiddlewareBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	api := engine.Group("/api")

	// public 组:无鉴权中间件
	public := api.Group("")
	RegisterPublicRoutes(public)

	// protected 组:挂一个必定拒绝的哨兵,模拟 JWT + Casbin
	protected := api.Group("")
	protected.Use(func(c *gin.Context) {
		c.AbortWithStatus(http.StatusUnauthorized)
	})
	RegisterRoutes(protected)

	// exchange 必须能穿过到 handler(不被哨兵拦)
	exReq := httptest.NewRequest(http.MethodPost, "/api/gb28181/sip/qr/exchange",
		strings.NewReader(`{"token":"AAAAAAAAAAAAAAAAAAAAAA"}`))
	exReq.Header.Set("Content-Type", "application/json")
	exRR := httptest.NewRecorder()
	engine.ServeHTTP(exRR, exReq)
	require.NotEqual(t, http.StatusUnauthorized, exRR.Code,
		"兑换端点走 public 组,不应被鉴权中间件拦住")

	// token 生成必须被哨兵拦下
	tkReq := httptest.NewRequest(http.MethodPost, "/api/gb28181/sip/qr/token", nil)
	tkRR := httptest.NewRecorder()
	engine.ServeHTTP(tkRR, tkReq)
	require.Equal(t, http.StatusUnauthorized, tkRR.Code,
		"token 生成端点必须经过鉴权中间件")
}
