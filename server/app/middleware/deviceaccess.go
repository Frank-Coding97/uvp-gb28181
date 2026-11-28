package middleware

import (
	"github.com/gin-gonic/gin"
	"uvplatform.com/uvp-gb28181/app/deviceaccess"
)

// DeviceAccessIngress runs before the OpenAPI boundary so both routers retain
// the same trusted device ingress in their request context.
func DeviceAccessIngress(trustedProxies []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if base, err := deviceaccess.Resolve(c.Request, trustedProxies); err == nil {
			c.Request = c.Request.WithContext(deviceaccess.WithBaseURL(c.Request.Context(), base))
		}
		c.Next()
	}
}
