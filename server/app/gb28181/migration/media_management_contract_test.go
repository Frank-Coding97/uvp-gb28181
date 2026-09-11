package migration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	gb28181routes "uvplatform.cn/uvp-gb28181/app/gb28181/routes"
	migrationsfs "uvplatform.cn/uvp-gb28181/resource/database/gb28181"
)

var mediaManagementMenuPaths = []string{
	"/gb28181/zlm/overview",
	"/gb28181/zlm/nodes",
	"/gb28181/zlm/scheduler",
	"/gb28181/zlm/scheduler/logs",
	"/gb28181/zlm/runtime",
	"/gb28181/zlm/streams",
	"/gb28181/zlm/sessions",
	"/gb28181/zlm/proxies",
	"/gb28181/zlm/ffmpeg-sources",
	"/gb28181/zlm/rtp-servers",
	"/gb28181/cloud-recordings",
	"/gb28181/recording-schedules",
	"/gb28181/zlm/config",
}

var mediaManagementHighRiskPermissions = []string{
	"gb28181:zlm:node:manage",
	"gb28181:zlm:node:kick",
	"gb28181:zlm:scheduler:manage",
	"gb28181:zlm:stream:preview",
	"gb28181:zlm:stream:close",
	"gb28181:zlm:stream:force-close",
	"gb28181:zlm:session:kick",
	"gb28181:zlm:proxy:manage",
	"gb28181:zlm:ffmpeg:manage",
	"gb28181:zlm:rtp:manage",
	"gb28181:zlm:rtp:force-close",
	"gb28181:recording:control",
	"gb28181:recording:force-stop",
	"gb28181:zlm:config:update",
	"gb28181:zlm:restart",
}

type mediaManagementAPI struct {
	method string
	path   string
}

var mediaManagementAPIs = []mediaManagementAPI{
	{method: "GET", path: "/api/gb28181/zlm/nodes"},
	{method: "POST", path: "/api/gb28181/zlm/nodes"},
	{method: "GET", path: "/api/gb28181/zlm/nodes/:id"},
	{method: "PUT", path: "/api/gb28181/zlm/nodes/:id"},
	{method: "DELETE", path: "/api/gb28181/zlm/nodes/:id"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/maintenance"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/activate"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/kick"},
	{method: "GET", path: "/api/gb28181/zlm/nodes/:id/restart"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/restart"},
	{method: "GET", path: "/api/gb28181/zlm/nodes/:id/config"},
	{method: "PUT", path: "/api/gb28181/zlm/nodes/:id/config"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/config/test-connection"},
	{method: "GET", path: "/api/gb28181/zlm/scheduler"},
	{method: "PUT", path: "/api/gb28181/zlm/scheduler"},
	{method: "GET", path: "/api/gb28181/zlm/scheduler/logs"},
	{method: "GET", path: "/api/gb28181/zlm/overview"},
	{method: "GET", path: "/api/gb28181/zlm/streams"},
	{method: "GET", path: "/api/gb28181/zlm/nodes/:id/runtime"},
	{method: "GET", path: "/api/gb28181/zlm/nodes/:id/streams"},
	{method: "GET", path: "/api/gb28181/zlm/nodes/:id/streams/detail"},
	{method: "GET", path: "/api/gb28181/zlm/nodes/:id/streams/viewers"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/streams/playback-grant"},
	{method: "GET", path: "/api/gb28181/zlm/nodes/:id/streams/snapshot"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/streams/close/preflight"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/streams/close"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/streams/force-close"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/streams/close/batch/preflight"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/streams/close/batch"},
	{method: "GET", path: "/api/gb28181/zlm/nodes/:id/sessions/network"},
	{method: "GET", path: "/api/gb28181/zlm/nodes/:id/sessions/viewers"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/sessions/kick"},
	{method: "GET", path: "/api/gb28181/zlm/nodes/:id/proxies/pull"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/proxies/pull"},
	{method: "GET", path: "/api/gb28181/zlm/nodes/:id/proxies/pull/:key"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/proxies/pull/:key/preflight"},
	{method: "DELETE", path: "/api/gb28181/zlm/nodes/:id/proxies/pull/:key"},
	{method: "GET", path: "/api/gb28181/zlm/nodes/:id/proxies/push"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/proxies/push"},
	{method: "GET", path: "/api/gb28181/zlm/nodes/:id/proxies/push/:key"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/proxies/push/:key/preflight"},
	{method: "DELETE", path: "/api/gb28181/zlm/nodes/:id/proxies/push/:key"},
	{method: "GET", path: "/api/gb28181/zlm/nodes/:id/ffmpeg-sources"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/ffmpeg-sources"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key/preflight"},
	{method: "DELETE", path: "/api/gb28181/zlm/nodes/:id/ffmpeg-sources/:key"},
	{method: "GET", path: "/api/gb28181/zlm/nodes/:id/rtp-servers"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/rtp-servers"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/rtp-servers/close/preflight"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/rtp-servers/close"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/rtp-servers/force-close"},
	{method: "GET", path: "/api/gb28181/zlm/nodes/:id/recordings/runtime/status"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/recordings/runtime/start/preflight"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/recordings/runtime/start"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/recordings/runtime/stop/preflight"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/recordings/runtime/stop"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop/preflight"},
	{method: "POST", path: "/api/gb28181/zlm/nodes/:id/recordings/runtime/force-stop"},
	{method: "GET", path: "/api/gb28181/cloud-recordings/files"},
	{method: "GET", path: "/api/gb28181/cloud-recordings/files/options"},
	{method: "POST", path: "/api/gb28181/cloud-recordings/files/batch-delete"},
	{method: "GET", path: "/api/gb28181/cloud-recordings/files/:id"},
	{method: "DELETE", path: "/api/gb28181/cloud-recordings/files/:id"},
	{method: "POST", path: "/api/gb28181/cloud-recordings/files/:id/access"},
	{method: "POST", path: "/api/gb28181/cloud-recordings/files/:id/downloads"},
	{method: "GET", path: "/api/gb28181/cloud-recordings/downloads/:taskId"},
	{method: "DELETE", path: "/api/gb28181/cloud-recordings/downloads/:taskId"},
	{method: "GET", path: "/api/gb28181/cloud-recordings/active"},
	{method: "POST", path: "/api/gb28181/cloud-recordings/active/:id/stop"},
	{method: "GET", path: "/api/gb28181/cloud-recordings/reconciliations"},
	{method: "POST", path: "/api/gb28181/cloud-recordings/reconciliations"},
	{method: "GET", path: "/api/gb28181/recording-plans"},
	{method: "POST", path: "/api/gb28181/recording-plans"},
	{method: "PATCH", path: "/api/gb28181/recording-plans/channels/:channelId/recording-mode"},
	{method: "GET", path: "/api/gb28181/recording-plans/channels/:channelId/diagnosis"},
	{method: "GET", path: "/api/gb28181/recording-plans/channels/:channelId/timeline"},
	{method: "GET", path: "/api/gb28181/recording-plans/:id"},
	{method: "PUT", path: "/api/gb28181/recording-plans/:id"},
	{method: "DELETE", path: "/api/gb28181/recording-plans/:id"},
	{method: "PATCH", path: "/api/gb28181/recording-plans/:id/status"},
	{method: "GET", path: "/api/gb28181/recording-plans/:id/assignment-options/devices"},
	{method: "GET", path: "/api/gb28181/recording-plans/:id/assignment-options/channels"},
	{method: "POST", path: "/api/gb28181/recording-plans/:id/assignments"},
	{method: "GET", path: "/api/gb28181/recording-plans/:id/channels"},
}

func normalizeMediaManagementSQL(body []byte) string {
	normalized := strings.NewReplacer("`", "", "[", "", "]", "").Replace(strings.ToLower(string(body)))
	return strings.Join(strings.Fields(normalized), " ")
}

func assertMediaManagementManifest(t *testing.T, normalized string) {
	t.Helper()
	require.Contains(t, normalized, "'/media'", "媒体管理根菜单必须按路径解析")
	for _, path := range mediaManagementMenuPaths {
		require.Containsf(t, normalized, strings.ToLower(path), "缺少媒体管理页面 %s", path)
	}
	for _, permission := range mediaManagementHighRiskPermissions {
		require.Containsf(t, normalized, permission, "缺少高风险按钮权限 %s", permission)
	}
	for _, api := range mediaManagementAPIs {
		pair := "'" + strings.ToLower(api.path) + "','" + strings.ToLower(api.method) + "'"
		mysqlFirstRowPair := "'" + strings.ToLower(api.path) + "' path,'" + strings.ToLower(api.method) + "' method"
		require.Truef(t, strings.Contains(normalized, pair) || strings.Contains(normalized, mysqlFirstRowPair),
			"缺少受保护后端 API %s %s", api.method, api.path)
	}
}

func TestMediaManagementPermissionMigrationsAreExactAndIdempotent(t *testing.T) {
	files := []string{
		"2026-08-30-media-management.sql",
		"2026-08-30-media-management-postgresql.sql",
		"2026-08-30-media-management-sqlserver.sql",
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			body, err := migrationsfs.FS.ReadFile("migrations/" + name)
			require.NoError(t, err)
			normalized := normalizeMediaManagementSQL(body)
			assertMediaManagementManifest(t, normalized)
			require.Contains(t, normalized, "where not exists", "up 必须可重复执行")
			require.Contains(t, normalized, "select distinct 'p'", "Casbin 规则必须去重")
			require.Contains(t, normalized, "join sys_menu_api ma on ma.menu_id=m.id", "Casbin 必须从精确菜单 API 绑定生成")
			require.Contains(t, normalized, "gb28181 媒体管理", "新建 API 必须带迁移所有权分组")
			require.Contains(t, normalized, "select 1,m.id from sys_menu m", "新增页面与高风险按钮只自动授予管理员角色")
			require.NotContains(t, normalized, "select rm.role_id", "不得把高风险按钮复制给普通角色")
			require.NotContains(t, normalized, "select source_role.role_id", "不得从旧页面继承高风险按钮")
			require.NotContains(t, normalized, "cross join sys_api", "禁止通过 API 全表交叉连接扩权")
			require.NotContains(t, normalized, "parent_id=140355", "父菜单 ID 必须动态解析")
			require.NotContains(t, normalized, "parent_id = 140355", "父菜单 ID 必须动态解析")
		})
	}
}

func TestMediaManagementPermissionManifestMatchesProtectedRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	gb28181routes.RegisterRoutes(engine.Group("/api"))

	want := make(map[string]struct{}, len(mediaManagementAPIs))
	for _, api := range mediaManagementAPIs {
		want[api.method+" "+api.path] = struct{}{}
	}
	// 历史迁移保持原始契约；后续受保护路由由当前按钮目录补齐。
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "resource", "database", "gb28181", "button-permissions.json"))
	require.NoError(t, err)
	var catalog struct {
		Buttons []struct {
			APIs []struct {
				Method string `json:"method"`
				Path   string `json:"path"`
			} `json:"apis"`
		} `json:"buttons"`
	}
	require.NoError(t, json.Unmarshal(body, &catalog))
	require.NotEmpty(t, catalog.Buttons)
	for _, button := range catalog.Buttons {
		for _, api := range button.APIs {
			if strings.HasPrefix(api.Path, "/api/gb28181/zlm/") ||
				strings.HasPrefix(api.Path, "/api/gb28181/cloud-recordings/") ||
				api.Path == "/api/gb28181/recording-plans" ||
				strings.HasPrefix(api.Path, "/api/gb28181/recording-plans/") {
				want[api.Method+" "+api.Path] = struct{}{}
			}
		}
	}
	got := make(map[string]struct{}, len(want))
	for _, route := range engine.Routes() {
		if strings.HasPrefix(route.Path, "/api/gb28181/zlm/") ||
			strings.HasPrefix(route.Path, "/api/gb28181/cloud-recordings/") ||
			route.Path == "/api/gb28181/recording-plans" ||
			strings.HasPrefix(route.Path, "/api/gb28181/recording-plans/") {
			got[route.Method+" "+route.Path] = struct{}{}
		}
	}
	require.Equal(t, want, got, "迁移权限清单必须与实际受保护路由一一对应；公开 capability 下载端点不属于 Casbin 清单")
}

func TestMediaManagementDownMigrationsPreserveLedgerAndRestoreLegacyMenus(t *testing.T) {
	files := []string{
		"2026-08-30-media-management-down.sql",
		"2026-08-30-media-management-postgresql-down.sql",
		"2026-08-30-media-management-sqlserver-down.sql",
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			body, err := migrationsfs.FS.ReadFile("migrations/" + name)
			require.NoError(t, err)
			normalized := normalizeMediaManagementSQL(body)
			casbin := strings.Index(normalized, "sys_casbin_rule")
			menuAPI := strings.Index(normalized, "sys_menu_api")
			roleMenu := strings.Index(normalized, "sys_role_menu")
			menu := strings.Index(normalized[roleMenu+1:], "sys_menu") + roleMenu + 1
			api := strings.LastIndex(normalized, "sys_api")
			require.True(t, casbin >= 0 && casbin < menuAPI && menuAPI < roleMenu && roleMenu < menu && menu < api,
				"down 顺序必须为 Casbin -> menu_api -> role_menu -> menu -> API")
			for _, token := range []string{"流媒体节点", "调度算法", "调度日志", "云端录像", "录像计划", "'/media'"} {
				require.Contains(t, normalized, strings.ToLower(token), "down 必须恢复原菜单字段")
			}
			require.NotContains(t, normalized, "gb_zlm_managed_resource", "菜单回滚不得删除资源归属账本")
		})
	}
}

func TestMediaManagementFreshBaselinesContainEquivalentManifest(t *testing.T) {
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("..", "..", "..", "resource", "database", name))
			require.NoError(t, err)
			normalized := normalizeMediaManagementSQL(body)
			require.Contains(t, normalized, "media-management-baseline:start")
			assertMediaManagementManifest(t, normalized)
		})
	}
}
