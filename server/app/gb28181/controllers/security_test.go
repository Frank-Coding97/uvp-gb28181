package controllers

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSecurityControllerSnapshotReportsUnavailableRuntime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/snapshot", NewSecurityController(nil).Snapshot)
	req := httptest.NewRequest("GET", "/snapshot", nil)
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	require.Equal(t, 503, resp.Code)
	require.Contains(t, resp.Body.String(), `security runtime unavailable`)
}
