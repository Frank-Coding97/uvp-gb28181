package middleware

import (
	"context"
	"net/http"
	"time"
	"uvplatform.com/uvp-gb28181/app/global/app"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// TimeoutMiddleware 超时中间件
// 为每个请求设置全局超时时间，防止长时间运行的请求阻塞服务器
func TimeoutMiddleware(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		// SSE streams own their lifetime and emit application-level heartbeats.
		// The upgrade import endpoint owns a bounded multipart upload. Its
		// handler returns as soon as the file is durably handed to the updater;
		// applying the ordinary request deadline would cancel slower, valid
		// uploads before the asynchronous verifier can take over.
		if isUploadRequestPath(c.Request.URL.Path) {
			setRequestIODeadline(c.Writer, time.Now().Add(upgradeUploadTimeout))
			c.Next()
			return
		}
		if isRealtimeRequestPath(c.Request.URL.Path) {
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

const upgradeUploadTimeout = 30 * time.Minute

func isUploadRequestPath(path string) bool {
	return path == "/api/system/upgrade/import" || path == "/api/system/upgrade/import/"
}

func isRealtimeRequestPath(path string) bool {
	switch path {
	case "/api/gb28181/logs/stream", "/api/gb28181/logs/stream/":
		return true
	default:
		return false
	}
}

func setRequestIODeadline(writer http.ResponseWriter, deadline time.Time) {
	controller := http.NewResponseController(writer)
	// ⛔ 不能静默吞错：中间件链上任何一层包装如果没转发 Unwrap，这里就是
	// ErrNotSupported，读超时仍停留在 httpserver.read_timeout(30s)，慢速上传
	// 会被掐断成一句看不出原因的 400。宁可留下一条告警。
	if err := controller.SetReadDeadline(deadline); err != nil && app.ZapLog != nil {
		app.ZapLog.Warn("升级包上传读超时延长失败", zap.String("event", "http.upgrade_upload_read_deadline_failed"), zap.Error(err))
	}
	if err := controller.SetWriteDeadline(deadline); err != nil && app.ZapLog != nil {
		app.ZapLog.Warn("升级包上传写超时延长失败", zap.String("event", "http.upgrade_upload_write_deadline_failed"), zap.Error(err))
	}
}
