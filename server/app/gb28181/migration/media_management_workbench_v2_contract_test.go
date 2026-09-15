package migration

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	migrationsfs "uvplatform.cn/uvp-gb28181/resource/database/gb28181"
)

const mediaWorkbenchV2Migration = "2026-08-30-zlm-media-workbench-v2"

type mediaWorkbenchMenu struct {
	path      string
	component string
	title     string
}

var mediaWorkbenchMenus = []mediaWorkbenchMenu{
	{path: "/media/overview", component: "gb28181/zlm/workbench/MediaOverview", title: "媒体总览"},
	{path: "/media/monitoring", component: "gb28181/zlm/workbench/MediaMonitoring", title: "媒体监控"},
	{path: "/media/ingress", component: "gb28181/zlm/workbench/IngressManagement", title: "接入管理"},
	{path: "/media/recordings", component: "gb28181/zlm/workbench/RecordingCenter", title: "录制中心"},
	{path: "/media/nodes", component: "gb28181/zlm/workbench/NodeManagement", title: "节点管理"},
	{path: "/media/scheduling", component: "gb28181/zlm/workbench/SchedulingManagement", title: "调度管理"},
	{path: "/media/nodes/:id", component: "gb28181/zlm/workbench/nodes/NodeDetail", title: "节点详情"},
}

var mediaWorkbenchLegacyPaths = []string{
	"/gb28181/zlm/overview",
	"/gb28181/zlm/runtime",
	"/gb28181/zlm/streams",
	"/gb28181/zlm/sessions",
	"/gb28181/zlm/proxies",
	"/gb28181/zlm/ffmpeg-sources",
	"/gb28181/zlm/rtp-servers",
	"/gb28181/cloud-recordings",
	"/gb28181/recording-schedules",
	"/gb28181/zlm/nodes",
	"/gb28181/zlm/nodes/:id",
	"/gb28181/zlm/config",
	"/gb28181/zlm/scheduler",
	"/gb28181/zlm/scheduler/logs",
}

var mediaWorkbenchLegacyComponents = map[string]string{
	"/gb28181/zlm/overview":        "gb28181/zlm/ClusterOverview",
	"/gb28181/zlm/runtime":         "gb28181/zlm/RuntimeOverview",
	"/gb28181/zlm/streams":         "gb28181/zlm/StreamManagement",
	"/gb28181/zlm/sessions":        "gb28181/zlm/SessionManagement",
	"/gb28181/zlm/proxies":         "gb28181/zlm/ProxyManagement",
	"/gb28181/zlm/ffmpeg-sources":  "gb28181/zlm/FFmpegSources",
	"/gb28181/zlm/rtp-servers":     "gb28181/zlm/RTPServices",
	"/gb28181/cloud-recordings":    "gb28181/cloud-recordings/index",
	"/gb28181/recording-schedules": "gb28181/recording-schedules/index",
	"/gb28181/zlm/nodes":           "gb28181/zlm/NodeList",
	"/gb28181/zlm/nodes/:id":       "gb28181/zlm/NodeDetail",
	"/gb28181/zlm/config":          "gb28181/zlm/ServerConfig",
	"/gb28181/zlm/scheduler":       "gb28181/zlm/SchedulerStrategy",
	"/gb28181/zlm/scheduler/logs":  "gb28181/zlm/SchedulerLog",
}

var mediaWorkbenchRoleUnions = map[string][]string{
	"/media":            mediaWorkbenchLegacyPaths,
	"/media/overview":   {"/gb28181/zlm/overview"},
	"/media/monitoring": {"/gb28181/zlm/runtime", "/gb28181/zlm/streams", "/gb28181/zlm/sessions"},
	"/media/ingress":    {"/gb28181/zlm/proxies", "/gb28181/zlm/ffmpeg-sources", "/gb28181/zlm/rtp-servers"},
	"/media/recordings": {"/gb28181/cloud-recordings", "/gb28181/recording-schedules"},
	"/media/nodes":      {"/gb28181/zlm/nodes", "/gb28181/zlm/nodes/:id", "/gb28181/zlm/config"},
	"/media/nodes/:id":  {"/gb28181/zlm/nodes", "/gb28181/zlm/nodes/:id", "/gb28181/zlm/config"},
	"/media/scheduling": {"/gb28181/zlm/scheduler", "/gb28181/zlm/scheduler/logs"},
}

func readMediaWorkbenchMigration(t *testing.T, name string) string {
	t.Helper()
	body, err := migrationsfs.FS.ReadFile("migrations/" + name)
	require.NoError(t, err)
	return normalizeMediaManagementSQL(body)
}

func assertMediaWorkbenchUpContract(t *testing.T, normalized string) {
	t.Helper()
	require.Contains(t, normalized, "media-workbench-v2:start")
	require.Contains(t, normalized, "media-workbench-v2:end")
	require.Contains(t, normalized, "gb28181/zlm/workbench/mediaentry")
	require.Contains(t, normalized, "where not exists", "up 必须可重复执行")
	require.NotRegexp(t, regexp.MustCompile(`\b140[0-9]+\b`), normalized, "V2 菜单 ID 必须动态解析")

	for _, menu := range mediaWorkbenchMenus {
		require.Contains(t, normalized, strings.ToLower(menu.path))
		require.Contains(t, normalized, strings.ToLower(menu.component))
		require.Contains(t, normalized, strings.ToLower(menu.title))
	}
	for _, path := range mediaWorkbenchLegacyPaths {
		require.Contains(t, normalized, strings.ToLower(path))
	}
	require.GreaterOrEqual(t, strings.Count(normalized, "gb28181/zlm/workbench/legacymediaroute"), len(mediaWorkbenchLegacyPaths),
		"每个旧路由都必须改为兼容桥组件")

	for target, sources := range mediaWorkbenchRoleUnions {
		marker := "workspace-role-union:" + strings.ToLower(target)
		require.Contains(t, normalized, marker)
		markerAt := strings.Index(normalized, marker)
		nextMarker := strings.Index(normalized[markerAt+len(marker):], "workspace-role-union:")
		section := normalized[markerAt:]
		if nextMarker >= 0 {
			section = normalized[markerAt : markerAt+len(marker)+nextMarker]
		}
		for _, source := range sources {
			require.Containsf(t, section, strings.ToLower(source), "%s 必须继承 %s 的角色并集", target, source)
		}
		require.Contains(t, section, "not exists", "角色关系必须幂等")
	}

	for _, forbidden := range []string{"sys_menu_api", "sys_api", "sys_casbin_rule", "gb_zlm_managed_resource"} {
		require.NotContainsf(t, normalized, forbidden, "V2 仅重排导航，不得修改 %s", forbidden)
	}
}

func assertMediaWorkbenchDownContract(t *testing.T, normalized string) {
	t.Helper()
	require.Contains(t, normalized, "media-workbench-v2:down:start")
	require.Contains(t, normalized, "media-workbench-v2:down:end")
	roleMenuAt := strings.Index(normalized, "delete")
	menuAt := strings.LastIndex(normalized, "delete")
	require.True(t, roleMenuAt >= 0 && roleMenuAt < menuAt, "down 必须先删角色关系再删 V2 菜单")
	for _, menu := range mediaWorkbenchMenus {
		require.Contains(t, normalized, strings.ToLower(menu.path))
	}
	for path, component := range mediaWorkbenchLegacyComponents {
		require.Contains(t, normalized, strings.ToLower(path))
		require.Contains(t, normalized, strings.ToLower(component))
	}
	for _, forbidden := range []string{"sys_menu_api", "sys_api", "sys_casbin_rule", "gb_zlm_managed_resource", "meta_node", "gb_recording"} {
		require.NotContainsf(t, normalized, forbidden, "down 不得修改权限锚点或业务数据 %s", forbidden)
	}
}

func TestMediaManagementWorkbenchV2MigrationsPreservePermissionAnchors(t *testing.T) {
	upFiles := []string{
		mediaWorkbenchV2Migration + ".sql",
		mediaWorkbenchV2Migration + "-postgresql.sql",
		mediaWorkbenchV2Migration + "-sqlserver.sql",
	}
	downFiles := []string{
		mediaWorkbenchV2Migration + "-down.sql",
		mediaWorkbenchV2Migration + "-postgresql-down.sql",
		mediaWorkbenchV2Migration + "-sqlserver-down.sql",
	}
	for _, name := range upFiles {
		t.Run(name, func(t *testing.T) { assertMediaWorkbenchUpContract(t, readMediaWorkbenchMigration(t, name)) })
	}
	for _, name := range downFiles {
		t.Run(name, func(t *testing.T) { assertMediaWorkbenchDownContract(t, readMediaWorkbenchMigration(t, name)) })
	}
}

func TestMigrationFilesDiscoverMediaManagementWorkbenchV2InDialectOrder(t *testing.T) {
	entries, err := migrationsfs.FS.ReadDir("migrations")
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	cases := []struct {
		dialect Dialect
		up      string
	}{
		{dialect: DialectMySQL, up: mediaWorkbenchV2Migration + ".sql"},
		{dialect: DialectPostgres, up: mediaWorkbenchV2Migration + "-postgresql.sql"},
		{dialect: DialectSQLServer, up: mediaWorkbenchV2Migration + "-sqlserver.sql"},
	}
	for _, tc := range cases {
		files := FilterUpFiles(names, tc.dialect)
		index := -1
		for i, name := range files {
			if name == tc.up {
				index = i
				break
			}
		}
		require.Positive(t, index, "%s V2 up 必须被发现且排在已有迁移之后", tc.dialect)
		require.Equal(t, tc.up, files[index])
		_, err = migrationsfs.FS.ReadFile("migrations/" + DownFileName(tc.up))
		require.NoError(t, err, "%s down 必须能按 runner 规则定位", tc.dialect)
	}
}

func TestFreshBaselineContainsMediaManagementWorkbenchV2(t *testing.T) {
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("..", "..", "..", "resource", "database", name))
			require.NoError(t, err)
			normalized := normalizeMediaManagementSQL(body)
			start := strings.LastIndex(normalized, "media-workbench-v2:start")
			end := strings.LastIndex(normalized, "media-workbench-v2:end")
			require.True(t, start >= 0 && start < end, "fresh baseline 必须直接包含完整 V2 目标状态")
			assertMediaWorkbenchUpContract(t, normalized[start:end+len("media-workbench-v2:end")])
		})
	}
}
