package ginhelper

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"net"
	"os"
	"syscall"
	"time"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/global/consts"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
	"uvplatform.cn/uvp-gb28181/app/utils/response"
)

func validClientID(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, c := range []byte(s) {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// RequestLogging keeps Gin's writer untouched, including Flush and Hijack.
func RequestLogging(root *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		id := uuid.NewString()
		fields := []zap.Field{zap.String("request_id", id)}
		if external := c.GetHeader("X-Request-ID"); validClientID(external) {
			fields = append(fields, zap.String("client_request_id", external))
		}
		logger := logging.WithIdentity(root, fields...)
		c.Request = c.Request.WithContext(logging.WithContext(c.Request.Context(), logger))
		c.Header("X-Request-ID", id)
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "<unmatched>"
		}
		method := c.Request.Method
		switch method {
		case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "CONNECT", "TRACE":
		default:
			method = "OTHER"
		}
		size := c.Writer.Size()
		if size < 0 {
			size = 0
		}
		fields = []zap.Field{zap.String("event", "http.access"), zap.String("method", method), zap.String("route", route), zap.Int("http_status", c.Writer.Status()), zap.Float64("duration_ms", float64(time.Since(started))/float64(time.Millisecond)), zap.Int("response_bytes", size)}
		if result, ok := response.BusinessResult(c); ok {
			code := zap.Int("business_code", result.Code)
			if result.StringCode != "" {
				code = zap.String("business_code", result.StringCode)
			}
			fields = append(fields, code, zap.Bool("business_success", result.Success))
		}
		logger.Named("access").Info("HTTP request completed", fields...)
	}
}

// CustomRecovery captures the original panic stack before returning to access
// logging. It never formats the recovered value or dumps the request.
func CustomRecovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			value := recover()
			if value == nil {
				return
			}
			c.Abort()
			if text, ok := value.(string); ok && text == consts.RequestAborted {
				return
			}
			app.Log(c.Request.Context()).Named("http").Error("HTTP handler panicked", zap.String("event", "http.panic"), zap.String("panic_type", logging.TypeName(value)), zap.StackSkip("stack", 1))
			if isBrokenConnection(value) || c.Writer.Written() {
				return
			}
			response.ErrorSystem(c, "", nil)
		}()
		c.Next()
	}
}
func isBrokenConnection(value interface{}) bool {
	// Inspect known wrappers only; never invoke a user supplied Error/Unwrap.
	if op, ok := value.(*net.OpError); ok && op != nil {
		value = op.Err
	}
	if op, ok := value.(*os.SyscallError); ok && op != nil {
		value = op.Err
	}
	if errno, ok := value.(syscall.Errno); ok {
		return errno == syscall.EPIPE || errno == syscall.ECONNRESET
	}
	return false
}
