package ginhelper

import (
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

// InstallationGate must be registered before all application routes. The phase
// callback returns a synchronized snapshot whose writes follow database commits.
// Allowed routes still run their normal authentication and authorization.
func InstallationGate(phase func() string) gin.HandlerFunc {
	return func(c *gin.Context) {
		state := phase()
		if state == "complete" || installationRouteAllowed(state, c.Request.Method, c.Request.URL.Path) {
			c.Next()
			return
		}
		c.Header("Cache-Control", "no-store")
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": 503, "msg": "请先完成本机初始化"})
	}
}

func installationRouteAllowed(phase, method, requestPath string) bool {
	if phase != "pending_admin" && phase != "pending_sip" {
		return false
	}
	if method == http.MethodGet || method == http.MethodHead {
		// Vite's shipped assets only. Uploads, public QR and media paths are not
		// installation resources; the static handler still enforces file ownership.
		if requestPath == "/" || requestPath == "/index.html" || requestPath == "/favicon.ico" ||
			(strings.HasPrefix(requestPath, "/static/") && path.Clean(requestPath) == requestPath && !strings.Contains(requestPath, "\\")) {
			return true
		}
	}
	key := method + " " + requestPath
	if key == "GET /api/standalone/ready" || key == "GET /api/standalone/setup/status" {
		return true
	}
	if phase == "pending_admin" {
		return key == "POST /api/standalone/setup/admin"
	}
	switch key {
	case "POST /api/login", "POST /api/refreshToken", "GET /api/captcha/verify", "GET /api/config/get",
		"GET /api/users/profile", "POST /api/users/logout", "POST /api/users/session/heartbeat",
		"GET /api/sysMenu/getRouters", "GET /api/sysDict/getAllDicts",
		"GET /api/gb28181/sip/setup/status", "GET /api/gb28181/sip/setup/network-interfaces",
		"PUT /api/gb28181/sip/setup/config":
		return true
	default:
		return false
	}
}
