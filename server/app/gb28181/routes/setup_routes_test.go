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
		"GET /api/gb28181/sip/service-config/position-history",
		"PUT /api/gb28181/sip/service-config/position-history",
		"GET /api/gb28181/sip/service-config/sync-channels-on-online",
		"PUT /api/gb28181/sip/service-config/sync-channels-on-online",
		"GET /api/gb28181/sip/service-config/ptz-default-speed",
		"PUT /api/gb28181/sip/service-config/ptz-default-speed",
	} {
		require.True(t, got[route], route)
	}
}

func TestRegisterRoutes_IncludesPTZResourceEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterRoutes(engine.Group("/api"))
	got := make(map[string]bool)
	for _, route := range engine.Routes() {
		got[route.Method+" "+route.Path] = true
	}
	for _, route := range []string{
		"DELETE /api/gb28181/device-mgmt/channel/:id/ptz/presets/:presetId",
		"POST /api/gb28181/device-mgmt/channel/:id/ptz/cruise",
		"GET /api/gb28181/device-mgmt/channel/:id/ptz/home-position",
		"PATCH /api/gb28181/device-mgmt/channel/:id/ptz/home-position",
	} {
		require.True(t, got[route], route)
	}
	require.False(t, got["POST /api/gb28181/device-mgmt/channel/:id/ptz/aux"], "旧的通用辅助开关路由不应继续暴露")
}
