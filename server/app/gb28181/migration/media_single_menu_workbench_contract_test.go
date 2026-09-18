package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	migrationsfs "uvplatform.cn/uvp-gb28181/resource/database/gb28181"
)

const mediaSingleMenuMigration = "2026-09-01-zlm-single-menu-workbench"
const mediaGlobalSidebarMigration = "2026-09-02-zlm-global-sidebar-menu"

var mediaSingleMenuWorkspaces = []mediaWorkbenchMenu{
	{path: "/media/overview", component: "gb28181/zlm/workbench/MediaOverview", title: "运行总览"},
	{path: "/media/monitoring", component: "gb28181/zlm/workbench/MediaMonitoring", title: "流与会话"},
	{path: "/media/ingress", component: "gb28181/zlm/workbench/IngressManagement", title: "接入管理"},
	{path: "/media/nodes", component: "gb28181/zlm/workbench/NodeManagement", title: "节点管理"},
	{path: "/media/scheduling", component: "gb28181/zlm/workbench/SchedulingManagement", title: "调度管理"},
}

func TestMediaSingleMenuWorkbenchFreshBaselines(t *testing.T) {
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("..", "..", "..", "resource", "database", name))
			require.NoError(t, err)
			normalized := normalizeMediaManagementSQL(body)
			start := strings.LastIndex(normalized, "zlm-single-menu-workbench:start")
			end := strings.LastIndex(normalized, "zlm-single-menu-workbench:end")
			require.True(t, start >= 0 && start < end, "fresh baseline 必须包含单菜单工作台最终态")
			section := normalized[start : end+len("zlm-single-menu-workbench:end")]
			for _, workspace := range mediaSingleMenuWorkspaces {
				require.Contains(t, section, strings.ToLower(workspace.path))
				require.Contains(t, section, strings.ToLower(workspace.component))
			}
		})
	}
}

func TestMediaSingleMenuWorkbenchMigrations(t *testing.T) {
	for _, suffix := range []string{".sql", "-postgresql.sql", "-sqlserver.sql"} {
		name := mediaSingleMenuMigration + suffix
		t.Run(name, func(t *testing.T) {
			body, err := migrationsfs.FS.ReadFile("migrations/" + name)
			require.NoError(t, err)
			normalized := normalizeMediaManagementSQL(body)
			require.Contains(t, normalized, "zlm-single-menu-workbench:start")
			require.Contains(t, normalized, "zlm-single-menu-workbench:end")
			require.Contains(t, normalized, "'/media'")
			require.Contains(t, normalized, "'/media/overview'")
			for _, workspace := range mediaSingleMenuWorkspaces {
				require.Contains(t, normalized, strings.ToLower(workspace.path))
				require.Contains(t, normalized, strings.ToLower(workspace.component))
				require.Contains(t, normalized, strings.ToLower(workspace.title))
			}
			for _, path := range mediaWorkbenchLegacyPaths {
				if path == "/gb28181/cloud-recordings" || path == "/gb28181/recording-schedules" {
					continue
				}
				require.Contains(t, normalized, strings.ToLower(path), "旧 ZL 地址必须保留兼容桥")
			}
			require.NotContains(t, normalized, "/gb28181/cloud-recordings", "云端录像保持独立菜单")
			require.NotContains(t, normalized, "/gb28181/recording-schedules", "录像计划保持独立菜单")
			for _, forbidden := range []string{"sys_menu_api", "sys_api", "sys_casbin_rule", "gb_zlm_managed_resource", "gb_recording"} {
				require.NotContainsf(t, normalized, forbidden, "导航迁移不得修改 %s", forbidden)
			}
		})
	}
}

func TestMediaSingleMenuWorkbenchDownMigrationsRestoreDirectPages(t *testing.T) {
	for _, suffix := range []string{"-down.sql", "-postgresql-down.sql", "-sqlserver-down.sql"} {
		name := mediaSingleMenuMigration + suffix
		t.Run(name, func(t *testing.T) {
			body, err := migrationsfs.FS.ReadFile("migrations/" + name)
			require.NoError(t, err)
			normalized := normalizeMediaManagementSQL(body)
			require.Contains(t, normalized, "zlm-single-menu-workbench:down:start")
			require.Contains(t, normalized, "zlm-single-menu-workbench:down:end")
			for _, menu := range mediaZLMAdminDirectMenus {
				require.Contains(t, normalized, strings.ToLower(menu.path))
				require.Contains(t, normalized, strings.ToLower(menu.component))
			}
			require.NotContains(t, normalized, "/gb28181/cloud-recordings")
			require.NotContains(t, normalized, "/gb28181/recording-schedules")
		})
	}
}

func TestMediaGlobalSidebarMigrationsExposeCanonicalWorkspaces(t *testing.T) {
	for _, suffix := range []string{".sql", "-postgresql.sql", "-sqlserver.sql"} {
		name := mediaGlobalSidebarMigration + suffix
		t.Run(name, func(t *testing.T) {
			body, err := migrationsfs.FS.ReadFile("migrations/" + name)
			require.NoError(t, err)
			normalized := normalizeMediaManagementSQL(body)
			require.Contains(t, normalized, "zlm-global-sidebar-menu:start")
			require.Contains(t, normalized, "zlm-global-sidebar-menu:end")
			if suffix == ".sql" {
				require.NotContains(t, normalized, "set parent_id=(select min(id) from sys_menu", "MySQL 不允许在更新 sys_menu 时直接从同表子查询")
			}
			for _, workspace := range mediaSingleMenuWorkspaces {
				where := "where path='" + strings.ToLower(workspace.path) + "'"
				at := strings.Index(normalized, where)
				require.Greater(t, at, 0)
				start := strings.LastIndex(normalized[:at], "update sys_menu")
				require.GreaterOrEqual(t, start, 0)
				require.Contains(t, normalized[start:at], "hide=0")
			}
			require.Contains(t, normalized, "hide=1")
			require.Contains(t, normalized, "path='/media/nodes/:id'")
		})
	}
}
