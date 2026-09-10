package middleware

import (
	"context"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// TimeoutMiddleware 超时中间件
// 为每个请求设置全局超时时间，防止长时间运行的请求阻塞服务器
func TimeoutMiddleware(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		// SSE streams own their lifetime and emit application-level heartbeats;
		// applying the ordinary request timeout would terminate them after 30s.
		if strings.TrimSuffix(c.Request.URL.Path, "/") == "/api/gb28181/logs/stream" {
			c.Next()
			return
		}
		// 创建带超时的上下文
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()

		// 替换请求的上下文
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
