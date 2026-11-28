package routes

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPermissionWorkbenchRoutesReplaceLegacyAssignmentAPIs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterRoutes(engine.Group("/api"))
	registered := make(map[string]bool)
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, route := range []string{
		"GET /api/gb28181/device-mgmt/permission-workbench/summary",
		"POST /api/gb28181/device-mgmt/permission-workbench/devices/resolve",
		"POST /api/gb28181/device-mgmt/permission-workbench/assignments",
		"POST /api/gb28181/device-mgmt/permission-workbench/assignments/departments",
		"POST /api/gb28181/device-mgmt/permission-workbench/grants/query",
		"GET /api/gb28181/device-mgmt/permission-workbench/grant-targets",
		"POST /api/gb28181/device-mgmt/permission-workbench/grants/apply",
	} {
		require.True(t, registered[route], route)
	}
	for _, route := range []string{
		"POST /api/gb28181/device-mgmt/assign",
		"POST /api/gb28181/device-mgmt/assign-dept",
		"GET /api/gb28181/device-mgmt/device/:id/grants",
		"POST /api/gb28181/device-mgmt/device/:id/grants",
		"DELETE /api/gb28181/device-mgmt/device/:id/grants/:grantId",
	} {
		require.False(t, registered[route], route)
	}
}
