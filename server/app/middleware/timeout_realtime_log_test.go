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
