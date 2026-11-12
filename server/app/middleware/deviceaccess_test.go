package middleware

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
	"uvplatform.com/uvp-gb28181/app/deviceaccess"
)

func TestDeviceAccessIngressRetainsContextAcrossPrivateRouter(t *testing.T) {
	outer, private := gin.New(), gin.New()
	private.POST("/openapi/capture", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
		defer cancel()
		base, err := deviceaccess.FromContext(ctx)
		if err != nil {
			c.String(503, "%s", err)
			return
		}
		c.String(200, "%s", base)
	})
	outer.Use(DeviceAccessIngress(nil))
	outer.Use(func(c *gin.Context) { c.Abort(); private.ServeHTTP(c.Writer, c.Request) })
	for _, host := range []string{"192.168.1.8:5177", "camera.example:8443"} {
		req := httptest.NewRequest("POST", "/openapi/capture", nil)
		req.RemoteAddr = "127.0.0.1:9000"
		req.Host = "backend:8280"
		req.Header.Set(deviceaccess.Header, "http://"+host)
		out := httptest.NewRecorder()
		outer.ServeHTTP(out, req)
		require.Equal(t, 200, out.Code)
		require.Equal(t, "http://"+host, out.Body.String())
	}
}

func TestDeviceAccessIngressIsRequestLocal(t *testing.T) {
	router := gin.New()
	router.Use(DeviceAccessIngress(nil))
	router.POST("/capture", func(c *gin.Context) {
		base, err := deviceaccess.FromContext(c.Request.Context())
		if err != nil {
			c.Status(503)
			return
		}
		c.String(200, "%s", base)
	})
	var running sync.WaitGroup
	for _, host := range []string{"camera-a.example:5177", "camera-b.example:51000", "localhost:5177"} {
		running.Add(1)
		go func(host string) {
			defer running.Done()
			req := httptest.NewRequest("POST", "/capture", nil)
			req.Host = host
			out := httptest.NewRecorder()
			router.ServeHTTP(out, req)
			if host == "localhost:5177" {
				require.Equal(t, 503, out.Code)
			} else {
				require.Equal(t, 200, out.Code)
				require.Equal(t, "http://"+host, out.Body.String())
			}
		}(host)
	}
	running.Wait()
}
