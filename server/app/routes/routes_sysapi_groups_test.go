package routes

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 「接口管理」的分组下拉只能有一个数据源：后端受控清单。
// 这里钉住它的接线方式 —— 挂在 protected 组里（走 Casbin）、
// 与 /list 同级、且必须排在 `/:id` 之前，否则 `groups` 会被当成 id 吃掉。
func TestSysApiGroupsRouteIsProtectedAndRegisteredBeforeID(t *testing.T) {
	source, err := os.ReadFile("routes.go")
	require.NoError(t, err)
	text := string(source)

	casbin := strings.Index(text, "protected.Use(middleware.CasbinMiddleware())")
	list := strings.Index(text, `sysApi.GET("/list"`)
	groups := strings.Index(text, `sysApi.GET("/groups", sysApiControllers.Groups)`)
	byID := strings.Index(text, `sysApi.GET("/:id"`)

	require.NotEqual(t, -1, casbin, "未找到 Casbin 中间件接线")
	require.NotEqual(t, -1, list, "未找到 /sysApi/list 路由")
	require.NotEqual(t, -1, groups, "未找到 /sysApi/groups 路由")
	require.NotEqual(t, -1, byID, "未找到 /sysApi/:id 路由")

	require.Greater(t, groups, casbin, "分组字典接口必须在 Casbin 之后注册，不能是公开接口")
	require.Greater(t, groups, list, "分组字典应与 /list 相邻注册")
	require.Less(t, groups, byID, "分组字典必须在 /:id 之前注册，否则 groups 会被当成 id")
}
