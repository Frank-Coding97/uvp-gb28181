package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCascadeSnapshotUploadContentRoute(t *testing.T) {
	defer SetCascadeSnapshotUploadHandler(nil)
	router := gin.New()
	RegisterContentRoutes(router)
	SetCascadeSnapshotUploadHandler(func(c *gin.Context) {
		require.Equal(t, "/capability"+c.Query("suffix"), c.Param("token"))
		c.Status(http.StatusNoContent)
	})
	for _, method := range []string{"POST", "PUT"} {
		for _, suffix := range []string{"", "/", "/photo.jpg"} {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(method, "/api/gb28181/cascade-snapshots/uploads/capability"+suffix+"?suffix="+suffix, nil))
			require.Equal(t, http.StatusNoContent, response.Code)
			require.Empty(t, response.Header().Get("Location"))
		}
	}
	SetCascadeSnapshotUploadHandler(nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("POST", "/api/gb28181/cascade-snapshots/uploads/capability", nil))
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
}
