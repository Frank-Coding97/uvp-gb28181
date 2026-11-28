package routes

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 首页「平台信息」卡片的运行环境信息对所有登录会话可见，
// 因此必须注册在 sessionOnly 组（Casbin 业务权限之前）。
func TestSystemEnvironmentRouteDoesNotRequireBusinessPermission(t *testing.T) {
	source, err := os.ReadFile("routes.go")
	require.NoError(t, err)
	text := string(source)
	envRoute := strings.Index(text, `sessionOnly.GET("/system/environment"`)
	casbin := strings.Index(text, "protected.Use(middleware.CasbinMiddleware())")
	require.NotEqual(t, -1, envRoute, "system/environment route must be registered")
	require.NotEqual(t, -1, casbin)
	require.Less(t, envRoute, casbin, "system/environment must be registered without Casbin business permission")
}
