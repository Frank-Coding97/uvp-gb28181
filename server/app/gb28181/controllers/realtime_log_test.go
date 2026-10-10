package controllers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRealtimeLogStreamRequiresClaims(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/logs/stream", NewRealtimeLogController().Stream)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/logs/stream", nil))
	require.Equal(t, http.StatusForbidden, response.Code)
}

func TestRealtimeLogSystemAdminRole(t *testing.T) {
	require.True(t, containsRole([]uint{3, 1}, 1))
	require.False(t, containsRole([]uint{2, 3}, 1))
}
