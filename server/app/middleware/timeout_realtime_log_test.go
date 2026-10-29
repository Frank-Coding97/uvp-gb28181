package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTimeoutMiddlewareLeavesRealtimeLogStreamUnbounded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(TimeoutMiddleware(time.Second))
	router.GET("/api/gb28181/logs/stream", func(c *gin.Context) {
		_, bounded := c.Request.Context().Deadline()
		require.False(t, bounded)
		c.Status(http.StatusNoContent)
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/gb28181/logs/stream", nil))
	require.Equal(t, http.StatusNoContent, response.Code)
}

func TestTimeoutMiddlewareLeavesServiceUpgradeImportUnbounded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(TimeoutMiddleware(time.Second))
	router.POST("/api/system/upgrade/import", func(c *gin.Context) {
		_, bounded := c.Request.Context().Deadline()
		require.False(t, bounded)
		c.Status(http.StatusNoContent)
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/system/upgrade/import", nil))
	require.Equal(t, http.StatusNoContent, response.Code)
}

func TestUpgradeImportExtendsHTTPReadAndWriteDeadlines(t *testing.T) {
	deadline := time.Now().Add(upgradeUploadTimeout)
	writer := &deadlineCaptureWriter{}
	setRequestIODeadline(writer, deadline)
	require.Equal(t, deadline, writer.readDeadline)
	require.Equal(t, deadline, writer.writeDeadline)
}

type deadlineCaptureWriter struct {
	readDeadline  time.Time
	writeDeadline time.Time
}

func (w *deadlineCaptureWriter) Header() http.Header { return make(http.Header) }
func (w *deadlineCaptureWriter) Write(data []byte) (int, error) { return len(data), nil }
func (w *deadlineCaptureWriter) WriteHeader(int) {}
func (w *deadlineCaptureWriter) SetReadDeadline(deadline time.Time) error {
	w.readDeadline = deadline
	return nil
}
func (w *deadlineCaptureWriter) SetWriteDeadline(deadline time.Time) error {
	w.writeDeadline = deadline
	return nil
}

func TestTimeoutMiddlewareKeepsStreamProbeRequestsBounded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(TimeoutMiddleware(time.Second))
	router.POST("/api/gb28181/stream-probes/:streamId", func(c *gin.Context) {
		_, bounded := c.Request.Context().Deadline()
		require.True(t, bounded)
		c.Status(http.StatusNoContent)
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/gb28181/stream-probes/stream-1", nil))
	require.Equal(t, http.StatusNoContent, response.Code)
}

func TestTimeoutMiddlewareKeepsOrdinaryRequestsBounded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(TimeoutMiddleware(time.Second))
	router.GET("/api/gb28181/device/list", func(c *gin.Context) {
		_, bounded := c.Request.Context().Deadline()
		require.True(t, bounded)
		c.Status(http.StatusNoContent)
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/gb28181/device/list", nil))
	require.Equal(t, http.StatusNoContent, response.Code)
}
