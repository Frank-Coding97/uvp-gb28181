package routes

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestHomeDashboardRoutesAreRegisteredInProtectedGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	protected := router.Group("/api")
	RegisterRoutes(protected)
	routes := map[string]bool{}
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	require.True(t, routes["GET /api/gb28181/home/summary"])
	require.True(t, routes["GET /api/gb28181/home/drilldown/sip"])
	require.True(t, routes["GET /api/gb28181/home/layout"])
	require.True(t, routes["PUT /api/gb28181/home/layout"])
	require.True(t, routes["DELETE /api/gb28181/home/layout"])
}
