package routes

import (
	"strings"

	"github.com/gin-gonic/gin"
	"uvplatform.com/uvp-gb28181/app/openapi/auth"
	"uvplatform.com/uvp-gb28181/app/openapi/contracts"
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
	if err := registerPublicOperations(private, gateway); err != nil {
		return err
	}
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

func registerPublicOperations(private *gin.Engine, gateway *auth.Gateway) error {
	operations := contracts.Operations()
	if err := contracts.Validate(operations); err != nil {
		return err
	}
	for _, operation := range operations {
		private.Handle(operation.Method, operation.GinPath, gateway.Handler(operation.Scope))
	}
	return nil
}

func publicError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"code": code, "message": message, "requestId": "", "data": nil})
}
