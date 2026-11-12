package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSecurityRoutesAreProtectedGroupPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterRoutes(r.Group("/api"))
	paths := make(map[string]bool)
	for _, route := range r.Routes() {
		paths[route.Method+" "+route.Path] = true
	}
	require.True(t, paths["GET /api/gb28181/security/snapshot"])
	require.True(t, paths["GET /api/gb28181/security/stream"])
	require.True(t, paths["POST /api/gb28181/security/bans/:id/unban"])
	require.True(t, paths["GET /api/gb28181/security/access-rules"])
	require.True(t, paths["POST /api/gb28181/security/access-rules"])
	require.True(t, paths["PUT /api/gb28181/security/access-rules/:id"])
	require.True(t, paths["DELETE /api/gb28181/security/access-rules/:id"])
}
