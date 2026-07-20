package routes

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRegisterRoutes_IncludesSIPSetupEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterRoutes(engine.Group("/api"))
	got := make(map[string]bool)
	for _, route := range engine.Routes() {
		got[route.Method+" "+route.Path] = true
	}
	for _, route := range []string{
		"GET /api/gb28181/sip/setup/status",
		"GET /api/gb28181/sip/setup/network-interfaces",
		"PUT /api/gb28181/sip/setup/config",
		"POST /api/gb28181/sip/setup/skip",
	} {
		require.True(t, got[route], route)
	}
}
