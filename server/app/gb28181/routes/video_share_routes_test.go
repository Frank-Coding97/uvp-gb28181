package routes

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVideoShareManagementRoutesAreProtected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterRoutes(engine.Group("/api"))
	routes := map[string]bool{}
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	require.True(t, routes["GET /api/gb28181/channel-shares/:id"])
	require.True(t, routes["PUT /api/gb28181/channel-shares/:id"])
	require.True(t, routes["POST /api/gb28181/channel-shares/:id/link"])
 require.True(t, routes["POST /api/gb28181/channel-shares/:id/regenerate"])
}

func TestVideoShareResolveRouteIsRegisteredOnPublicGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterPublicRoutes(engine.Group("/api"))
	routes := map[string]bool{}
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	require.True(t, routes["POST /api/public/video-share/resolve"])
	require.True(t, routes["POST /api/public/video-share/viewers"])
	require.True(t, routes["POST /api/public/video-share/viewers/status"])
	require.True(t, routes["DELETE /api/public/video-share/viewers/:viewerToken"])
}

func TestVideoShareLiveFLVRouteIsRegisteredAsUnprotectedContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterContentRoutes(engine)
	routes := map[string]bool{}
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	require.True(t, routes["GET /api/public/video-share/viewers/:viewerToken/live.flv"])
	require.True(t, routes["GET /api/public/video-share/viewers/:viewerToken/hls/index.m3u8"])
	require.True(t, routes["GET /api/public/video-share/viewers/:viewerToken/hls/:resourceID"])
}
