package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	migrationsfs "uvplatform.cn/uvp-gb28181/resource/database/gb28181"
)

const mediaZLMAdminParityMigration = "2026-08-30-zlm-media-workbench-v3"

var mediaZLMAdminDirectMenus = []mediaWorkbenchMenu{
	{path: "/gb28181/zlm/overview", component: "gb28181/zlm/ClusterOverview", title: "集群总览"},
	{path: "/gb28181/zlm/nodes", component: "gb28181/zlm/NodeList", title: "节点管理"},
	{path: "/gb28181/zlm/runtime", component: "gb28181/zlm/RuntimeOverview", title: "总览"},
	{path: "/gb28181/zlm/streams", component: "gb28181/zlm/StreamManagement", title: "流管理"},
	{path: "/gb28181/zlm/sessions", component: "gb28181/zlm/SessionManagement", title: "会话管理"},
	{path: "/gb28181/zlm/proxies", component: "gb28181/zlm/ProxyManagement", title: "拉流/推流代理"},
	{path: "/gb28181/zlm/ffmpeg-sources", component: "gb28181/zlm/FFmpegSources", title: "FFmpeg 源"},
	{path: "/gb28181/zlm/rtp-servers", component: "gb28181/zlm/RTPServices", title: "RTP 服务"},
	{path: "/gb28181/zlm/config", component: "gb28181/zlm/ServerConfig", title: "服务器配置"},
	{path: "/gb28181/zlm/scheduler", component: "gb28181/zlm/SchedulerStrategy", title: "调度策略"},
	{path: "/gb28181/zlm/scheduler/logs", component: "gb28181/zlm/SchedulerLog", title: "调度日志"},
}

func assertMediaZLMAdminParity(t *testing.T, normalized string) {
	t.Helper()
	require.Contains(t, normalized, "zlm-admin-parity-v3:start")
	require.Contains(t, normalized, "zlm-admin-parity-v3:end")
	require.Contains(t, normalized, "'/media'", "流媒体根菜单必须按路径解析")
	for _, menu := range mediaZLMAdminDirectMenus {
		require.Contains(t, normalized, strings.ToLower(menu.path))
		require.Contains(t, normalized, strings.ToLower(menu.component))
		require.Contains(t, normalized, strings.ToLower(menu.title))
	}
	for _, path := range []string{"/media/overview", "/media/monitoring", "/media/ingress", "/media/recordings", "/media/nodes", "/media/scheduling", "/media/nodes/:id"} {
		require.Contains(t, normalized, path, "V2 工作台菜单必须退出可见导航")
	}
	for _, recordingPath := range []string{"/gb28181/cloud-recordings", "/gb28181/recording-schedules", "/gb28181/device-mgmt/index", "/gb28181/device-mgmt"} {
		require.Contains(t, normalized, recordingPath, "国标录像菜单必须恢复到原 GB28181 父级")
	}
	require.Contains(t, normalized, "gb28181/cloud-recordings/index")
	require.Contains(t, normalized, "gb28181/recording-schedules/index")
	require.NotContains(t, normalized, "gb28181/zlm/workbench/recordingcenter")
	for _, forbidden := range []string{"sys_menu_api", "sys_api", "sys_casbin_rule", "gb_zlm_managed_resource", "meta_node", "gb_recording"} {
		require.NotContainsf(t, normalized, forbidden, "V3 只重排导航，不得修改 %s", forbidden)
	}
}

func TestMediaZLMAdminParityMigrationsRestoreDirectNavigation(t *testing.T) {
	for _, suffix := range []string{".sql", "-postgresql.sql", "-sqlserver.sql"} {
		name := mediaZLMAdminParityMigration + suffix
		t.Run(name, func(t *testing.T) {
			body, err := migrationsfs.FS.ReadFile("migrations/" + name)
			require.NoError(t, err)
			assertMediaZLMAdminParity(t, normalizeMediaManagementSQL(body))
		})
	}
}

func TestMediaZLMAdminParityFreshBaselinesMatchIncrementalState(t *testing.T) {
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("..", "..", "..", "resource", "database", name))
			require.NoError(t, err)
			normalized := normalizeMediaManagementSQL(body)
			start := strings.LastIndex(normalized, "zlm-admin-parity-v3:start")
			end := strings.LastIndex(normalized, "zlm-admin-parity-v3:end")
			require.True(t, start >= 0 && start < end, "fresh baseline 必须包含 V3 最终导航")
			assertMediaZLMAdminParity(t, normalized[start:end+len("zlm-admin-parity-v3:end")])
		})
	}
}

func TestMediaZLMAdminParityRunsAfterWorkbenchV2(t *testing.T) {
	entries, err := migrationsfs.FS.ReadDir("migrations")
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries { names = append(names, entry.Name()) }
	for _, tc := range []struct{ dialect Dialect; v2, v3 string }{
		{DialectMySQL, mediaWorkbenchV2Migration + ".sql", mediaZLMAdminParityMigration + ".sql"},
		{DialectPostgres, mediaWorkbenchV2Migration + "-postgresql.sql", mediaZLMAdminParityMigration + "-postgresql.sql"},
		{DialectSQLServer, mediaWorkbenchV2Migration + "-sqlserver.sql", mediaZLMAdminParityMigration + "-sqlserver.sql"},
	} {
		files := FilterUpFiles(names, tc.dialect)
		index := func(name string) int { for i, file := range files { if file == name { return i } }; return -1 }
		require.Greater(t, index(tc.v3), index(tc.v2), "%s V3 必须在 V2 之后执行", tc.dialect)
	}
}
