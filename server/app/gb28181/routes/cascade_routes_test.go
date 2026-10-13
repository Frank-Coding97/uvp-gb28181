package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCascadeRoutesReturn503UntilServiceIsInjected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	SetCascadeManagementController(nil)
	engine := gin.New()
	RegisterRoutes(engine.Group("/api"))

	for _, path := range []string{
		"/api/gb28181/cascade/platforms",
		"/api/gb28181/cascade/platforms/1",
		"/api/gb28181/cascade/platforms/1/shares",
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		engine.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusServiceUnavailable, recorder.Code, path)
	}
}
