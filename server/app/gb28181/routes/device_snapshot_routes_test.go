package routes

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRegisterRoutesIncludesDeviceSnapshotEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterContentRoutes(engine)
	RegisterRoutes(engine.Group("/api"))
	routes := make(map[string]bool)
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	require.True(t, routes["POST /api/gb28181/device-mgmt/channel/:id/snapshot-sessions"])
	require.True(t, routes["GET /api/gb28181/device-mgmt/channel/:id/snapshot-sessions/:sessionId"])
	require.True(t, routes["PUT /api/gb28181/device-snapshots/uploads/:token/:filename"])
	require.True(t, routes["GET /api/gb28181/device-snapshots/uploads/:token/:filename"])
}
