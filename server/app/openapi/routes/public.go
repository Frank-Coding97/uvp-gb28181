package routes

import (
	"strings"

	"github.com/gin-gonic/gin"
	"uvplatform.cn/uvp-gb28181/app/openapi/auth"
)

// InstallPublicBoundary must run before CORS/business logging/timeouts. The
// prefix is a namespace boundary only; the private router below is the entire
// published capability catalog, never inferred from the backend's routes.
func InstallPublicBoundary(root *gin.Engine, gateway *auth.Gateway, trustedProxies []string) error {
	private := gin.New()
	private.RedirectTrailingSlash = false
	private.RedirectFixedPath = false
	private.RemoveExtraSlash = false
	private.HandleMethodNotAllowed = true
	if err := private.SetTrustedProxies(trustedProxies); err != nil {
		return err
	}
	private.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if c.Request.URL.RawPath != "" || strings.Contains(c.Request.URL.EscapedPath(), "%") {
			publicError(c, 400, "INVALID_REQUEST", "invalid request")
			c.Abort()
			return
		}
		c.Next()
	})
	private.NoRoute(func(c *gin.Context) { publicError(c, 404, "RESOURCE_NOT_FOUND", "resource not found") })
	private.NoMethod(func(c *gin.Context) { publicError(c, 405, "INVALID_REQUEST", "invalid request") })
	private.GET("/openapi/v1/devices", gateway.Handler("device:list"))
	private.GET("/openapi/v1/devices/:deviceId", gateway.Handler("device:detail"))
	private.GET("/openapi/v1/devices/:deviceId/status", gateway.Handler("device:status"))
	private.GET("/openapi/v1/devices/:deviceId/channels", gateway.Handler("channel:list"))
	private.GET("/openapi/v1/devices/:deviceId/channels/:channelId", gateway.Handler("channel:detail"))
	private.GET("/openapi/v1/devices/:deviceId/channels/:channelId/status", gateway.Handler("channel:status"))
	private.POST("/openapi/v1/devices/:deviceId/channels/:channelId/live-authorizations", gateway.Handler("play:live:apply"))
	private.GET("/openapi/v1/devices/:deviceId/channels/:channelId/ptz/presets", gateway.Handler("ptz:preset:list"))
	private.POST("/openapi/v1/devices/:deviceId/channels/:channelId/ptz/presets", gateway.Handler("ptz:preset:save"))
	private.POST("/openapi/v1/devices/:deviceId/channels/:channelId/ptz/presets/:presetId/call", gateway.Handler("ptz:preset:call"))
	private.DELETE("/openapi/v1/devices/:deviceId/channels/:channelId/ptz/presets/:presetId", gateway.Handler("ptz:preset:delete"))
	private.GET("/openapi/v1/devices/:deviceId/channels/:channelId/ptz/operations/:operationId", gateway.Handler("ptz:operation:read"))
	root.Use(func(c *gin.Context) {
		path := c.Request.URL.Path
		if path == "/openapi" || strings.HasPrefix(path, "/openapi/") {
			c.Abort()
			private.ServeHTTP(c.Writer, c.Request)
			return
		}
		c.Next()
	})
	return nil
}
func publicError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"code": code, "message": message, "requestId": "", "data": nil})
}
