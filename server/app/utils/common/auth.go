package common

import (
	"context"
	"errors"
	"net/http"
	"path"
	"strings"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/global/consts"

	"github.com/gin-gonic/gin"
)

// queryTokenAllowlist 是**唯一**允许从 URL 查询参数读取访问令牌的端点集合。
//
// 为什么必须收口成白名单：`?token=` 原本是给"浏览器带不了请求头"的场景兜底 ——
// EventSource 建连（SSE）与 `<img src>` 取图。代价是令牌会落进 nginx access log、
// 浏览器历史、Referer。全站无差别开口等于把这个代价扩散到所有接口：任何一个只
// 记 URL 的下游（反代、WAF、埋点）都会各存一份令牌，泄漏面成倍放大。
//
// ⛔ 新增条目只在"该端点由浏览器原生发起、且无法附加 Authorization 头"时才允许。
// 普通 XHR/fetch 一律走请求头。
var queryTokenAllowlist = map[string]struct{}{
	"/api/gb28181/sip/dashboard/stream": {}, // 首页 SIP 看板 SSE
	"/api/gb28181/sip-traces/stream":    {}, // 信令跟踪实时报文 SSE
	"/api/gb28181/logs/stream":          {}, // 运行日志 SSE
	"/api/gb28181/security/stream":      {}, // 安全事件 SSE
}

// 图像库取图接口（`<img>` 直连，带不了请求头）：`/api/gb28181/device-mgmt/snapshots/:id/content`。
const (
	snapshotContentPrefix = "/api/gb28181/device-mgmt/snapshots/"
	snapshotContentSuffix = "/content"
)

// queryTokenAllowed 判断本次请求是否允许用 `?token=` 鉴权。
//
// 只认 GET/HEAD：写操作在浏览器侧从来不需要这条通道，放行只会让令牌多出几个落点
// （表单 action、日志回放）。路径先做 path.Clean 再比对，`//`、`/./` 这类写法
// 不会绕过白名单（`..` 归一化后也对不上路由，最终 404）。
func queryTokenAllowed(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	switch c.Request.Method {
	case http.MethodGet, http.MethodHead:
	default:
		return false
	}
	requestPath := path.Clean(c.Request.URL.Path)
	if _, ok := queryTokenAllowlist[requestPath]; ok {
		return true
	}
	return strings.HasPrefix(requestPath, snapshotContentPrefix) &&
		strings.HasSuffix(requestPath, snapshotContentSuffix)
}

// 获取access token
func GetAccessToken(c *gin.Context) (string, error) {
	// 首先尝试从 Authorization header 获取 token
	authHeader := c.GetHeader("Authorization")
	if authHeader != "" {
		headerParts := strings.Split(authHeader, " ")
		if len(headerParts) == 2 && headerParts[0] == "Bearer" {
			return headerParts[1], nil
		}
	}

	// URL 参数兜底：**仅限白名单端点**（见 queryTokenAllowlist）。
	// ⛔ 不要放宽成"所有 GET"：那样每个接口的令牌都会进访问日志。
	if token := c.Query("token"); token != "" && queryTokenAllowed(c) {
		return token, nil
	}

	// 如果 header 格式不正确但没有 token 参数，返回格式错误
	if authHeader != "" {
		return "", errors.New("authorization header format must be Bearer {token}")
	}

	// 都没有找到 token
	return "", errors.New("token is required in Authorization header (Bearer {token}); URL parameter (?token=) is only accepted for streaming and image endpoints")
}

// GetClaims 从上下文获取 Claims
func GetClaims(c *gin.Context) *app.Claims {
	claims, exists := c.Get(consts.BindContextKeyName)
	if !exists {
		return nil
	}
	// 类型断言
	res, ok := claims.(*app.Claims)
	if !ok {
		return nil
	}
	return res
}

// GetCurrentUserID 获取当前用户ID
func GetCurrentUserID(c *gin.Context) uint {
	claims := GetClaims(c)
	if claims == nil {
		return 0
	}
	return claims.UserID
}

// 尝试将context.Context转换成*gin.Context
func TryConvertToGinContext(c context.Context) *gin.Context {
	if c == nil {
		return nil
	}
	ginCtx, ok := c.(*gin.Context)
	if !ok {
		return nil
	}
	return ginCtx
}
