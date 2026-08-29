package routes

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRegisterRoutesIncludesRecordingPlanEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterRoutes(engine.Group("/api"))

	want := map[string]bool{
		"GET /api/gb28181/recording-plans":                                      false,
		"POST /api/gb28181/recording-plans":                                     false,
		"GET /api/gb28181/recording-plans/:id":                                  false,
		"PUT /api/gb28181/recording-plans/:id":                                  false,
		"DELETE /api/gb28181/recording-plans/:id":                               false,
		"PATCH /api/gb28181/recording-plans/:id/status":                         false,
		"GET /api/gb28181/recording-plans/:id/assignment-options/devices":       false,
		"GET /api/gb28181/recording-plans/:id/assignment-options/channels":      false,
		"POST /api/gb28181/recording-plans/:id/assignments":                     false,
		"PATCH /api/gb28181/recording-plans/channels/:channelId/recording-mode": false,
	}
	for _, route := range engine.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for endpoint, found := range want {
		require.True(t, found, endpoint)
	}
}
