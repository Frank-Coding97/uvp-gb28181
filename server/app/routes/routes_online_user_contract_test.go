package routes

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOnlineUserRoutesSeparateHeartbeatFromCasbin(t *testing.T) {
	source, err := os.ReadFile("routes.go")
	require.NoError(t, err)
	text := string(source)
	heartbeat := strings.Index(text, `sessionOnly.POST("/users/session/heartbeat"`)
	casbin := strings.Index(text, "protected.Use(middleware.CasbinMiddleware())")
	list := strings.Index(text, `sysOnlineUser.GET("/list"`)
	forceLogout := strings.Index(text, `sysOnlineUser.POST("/forceLogout"`)
	require.NotEqual(t, -1, heartbeat)
	require.NotEqual(t, -1, casbin)
	require.NotEqual(t, -1, list)
	require.NotEqual(t, -1, forceLogout)
	require.Less(t, heartbeat, casbin, "heartbeat must be registered without Casbin business permission")
	require.Greater(t, list, casbin)
	require.Greater(t, forceLogout, casbin)
}
