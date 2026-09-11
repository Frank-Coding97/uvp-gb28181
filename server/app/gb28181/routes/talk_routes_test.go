package routes

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestTalkSessionRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterRoutes(engine.Group("/api"))
	want := map[string]bool{
		"POST /api/gb28181/device-mgmt/channel/:id/talk-sessions":              false,
		"GET /api/gb28181/device-mgmt/channel/:id/talk-sessions/:sessionId":    false,
		"DELETE /api/gb28181/device-mgmt/channel/:id/talk-sessions/:sessionId": false,
	}
	for _, route := range engine.Routes() {
		key := route.Method + " " + route.Path
		if _, exists := want[key]; exists {
			want[key] = true
		}
	}
	for route, found := range want {
		if !found {
			t.Errorf("missing route %s", route)
		}
	}
}
