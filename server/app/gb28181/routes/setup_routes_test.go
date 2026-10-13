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
		"GET /api/gb28181/sip/service-config",
		"PUT /api/gb28181/sip/service-config",
		"GET /api/gb28181/play/lifecycles",
		"GET /api/gb28181/play/lifecycles/:lifecycleId",
		"POST /api/gb28181/play/lifecycles/:lifecycleId/client-events",
	} {
		require.True(t, got[route], route)
	}
	require.False(t, got["POST /api/gb28181/play/:deviceId/:channelId/authorization"], "固定播放地址授权不再作为独立内部接口暴露")
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
		// 雨刷(GB/T 28181 A.3.7 表 A.11)走**独立路由**,编号固定 1。
		"POST /api/gb28181/device-mgmt/channel/:id/ptz/wiper",
	} {
		require.True(t, got[route], route)
	}
	require.False(t, got["POST /api/gb28181/device-mgmt/channel/:id/ptz/aux"], "旧的通用辅助开关路由不应继续暴露")
}
