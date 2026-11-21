package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	upgrade "uvplatform.com/uvp-gb28181/internal/upgrade"
)

// UpgradeCandidateMiddleware keeps a freshly activated release read-only
// until the updater clears the candidate gate. Upgrade inspection remains
// available so the helper can finish validation and the administrator can see
// the task; business writes are rejected before their handlers run.
func UpgradeCandidateMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		state, err := upgrade.GateStateFromEnv()
		if err != nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"code":        1,
				"message":     "升级门禁状态不可读",
				"upgradeGate": "unavailable",
			})
			return
		}
		if state.Mode == upgrade.GateNormal || isUpgradeInspectionRequest(c) {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
			"code":        1,
			"message":     "服务正在候选版本验证，请稍后重试",
			"upgradeGate": string(state.Mode),
		})
	}
}

func isUpgradeInspectionRequest(c *gin.Context) bool {
	path := c.Request.URL.Path
	if path == upgrade.RuntimeIdentityPath && c.Request.Method == http.MethodGet {
		return true
	}
	if !strings.HasPrefix(path, "/api/system/upgrade/") {
		return false
	}
	if c.Request.Method == http.MethodGet {
		return true
	}
	return c.Request.Method == http.MethodPost && strings.HasSuffix(path, "/check")
}
