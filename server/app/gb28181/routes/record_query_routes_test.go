package routes

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRecordQueryRoutesAreRegisteredInProtectedGroup(t *testing.T) {
	engine := gin.New()
	RegisterRoutes(engine.Group("/api"))
	want := map[string]bool{
		"GET /api/gb28181/device-mgmt/channel/:id/record-query/options":                  false,
		"POST /api/gb28181/device-mgmt/channel/:id/record-query":                         false,
		"POST /api/gb28181/device-mgmt/channel/:id/playback-sessions":                    false,
		"GET /api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId":          false,
		"POST /api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId/actions": false,
		"DELETE /api/gb28181/device-mgmt/channel/:id/playback-sessions/:sessionId":       false,
	}
	for _, route := range engine.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for route, found := range want {
		require.True(t, found, route)
	}
}
