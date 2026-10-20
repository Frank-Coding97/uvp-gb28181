package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"uvplatform.com/uvp-gb28181/app/global/app"
)

// corsAllowedOrigins 读取可选的来源白名单（`httpserver.corsallowedorigins`）。
//
// 留空 = 回显请求的 Origin（保持既有"开了跨域就任意来源可用"的部署习惯，
// 因为该开关本身就是显式 opt-in）；配置后只放行列出的来源，其余一律不发 CORS 头。
func corsAllowedOrigins() []string {
	if app.ConfigYml == nil {
		return nil
	}
	return app.ConfigYml.GetStringSlice("httpserver.corsallowedorigins")
}

// corsOriginAllowed 判断某个 Origin 是否允许跨域访问。
//
// ⛔ `null` 一律拒绝：沙箱 iframe / file:// 页面都会发 `Origin: null`，
// 而它恰好是"任意本地页面"的公共标识，放行等于没做限制。
func corsOriginAllowed(origin string, allowed []string) bool {
	if origin == "" || strings.EqualFold(origin, "null") {
		return false
	}
	if len(allowed) == 0 {
		return true
	}
	for _, item := range allowed {
		item = strings.TrimRight(strings.TrimSpace(item), "/")
		if item == "" {
			continue
		}
		if item == "*" || strings.EqualFold(item, origin) {
			return true
		}
	}
	return false
}

// 允许跨域
//
// ⛔ 绝不能同时发 `Access-Control-Allow-Origin: *` 和 `Access-Control-Allow-Credentials: true`：
// 浏览器会直接拒掉带凭据的响应（等于整段 CORS 白配），而 `*` 又让任意站点都能无凭据
// 打满所有接口。正确做法是**回显具体 Origin**（或不发），并补 `Vary: Origin` 防缓存串站。
func CorsNext() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		// 回显 Origin 必须带 Vary，否则中间缓存会把 A 站点的响应发给 B 站点。
		c.Header("Vary", "Origin")

		if corsOriginAllowed(origin, corsAllowedOrigins()) {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
		}
		c.Header("Access-Control-Allow-Headers", "Access-Control-Allow-Headers,Authorization,User-Agent, Keep-Alive, Content-Type, X-Requested-With,X-CSRF-Token,AccessToken,Token,Idempotency-Key")
		c.Header("Access-Control-Allow-Methods", "GET, POST, DELETE, PUT, PATCH, OPTIONS")
		c.Header("Access-Control-Expose-Headers", "Content-Length, Access-Control-Allow-Origin, Access-Control-Allow-Headers, Content-Type")

		// 放行所有OPTIONS方法
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		// 处理请求
		c.Next()
	}
}
