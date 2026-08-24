package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	migrationsfs "uvplatform.cn/uvp-gb28181/resource/database/gb28181"
)

// contractThreshold 契约生效日期:该日期及以后的迁移文件必须
// 三方言齐全 + up/down 成对。存量文件(阈值前)豁免,只约束新文件。
const contractThreshold = "2026-08-14"

// 6.1/6.2:阈值日期及以后的每个 MySQL up 迁移,配套
// -postgresql.sql / -sqlserver.sql / -down.sql 必须齐全。
func TestMigrationFileContract(t *testing.T) {
	entries, err := migrationsfs.FS.ReadDir("migrations")
	require.NoError(t, err)

	var newFiles []string
	for _, e := range entries {
		name := e.Name()
		if len(name) < len(contractThreshold) || !strings.HasSuffix(name, ".sql") {
			continue
		}
		date := name[:len(contractThreshold)]
		if date >= contractThreshold {
			newFiles = append(newFiles, name)
		}
	}

	mysqlUps := FilterUpFiles(newFiles, DialectMySQL)
	if len(mysqlUps) == 0 {
		t.Skip("阈值日期后暂无新迁移文件,契约断言空转")
	}

	all := make(map[string]bool, len(newFiles))
	for _, name := range newFiles {
		all[name] = true
	}
	for _, up := range mysqlUps {
		base := strings.TrimSuffix(up, ".sql")
		for _, required := range []string{
			base + "-postgresql.sql",
			base + "-sqlserver.sql",
			base + "-down.sql",
		} {
			require.Truef(t, all[required], "迁移 %s 缺少配套文件 %s(三方言 + down 必须齐全)", up, required)
		}
	}
}

func TestDeviceAssignmentPermissionMigrationsUseMenuAPIBindings(t *testing.T) {
	files := []string{
		"2026-08-15-device-assignment-permissions.sql",
		"2026-08-15-device-assignment-permissions-postgresql.sql",
		"2026-08-15-device-assignment-permissions-sqlserver.sql",
	}

	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			body, err := migrationsfs.FS.ReadFile("migrations/" + name)
			require.NoError(t, err)

			normalized := strings.NewReplacer("`", "", "[", "", "]", "").Replace(strings.ToLower(string(body)))
			normalized = strings.Join(strings.Fields(normalized), " ")
			require.Contains(t, normalized, "select distinct 'p'", "同一角色可能通过多个菜单命中同一 API,写入前必须去重")
			require.Contains(t, normalized, "join sys_menu_api ma on ma.menu_id=m.id", "Casbin 规则必须复用菜单与 API 的精确绑定")
			require.NotContains(t, normalized, "cross join sys_api a where m.permission in", "全量交叉连接会生成重复或越权规则")
		})
	}
}

func TestDevicePermissionWorkbenchMigrationsCoverNewContract(t *testing.T) {
	files := []string{
		"2026-08-24-device-permission-workbench-permissions.sql",
		"2026-08-24-device-permission-workbench-permissions-postgresql.sql",
		"2026-08-24-device-permission-workbench-permissions-sqlserver.sql",
	}
	paths := []string{
		"/api/gb28181/device-mgmt/permission-workbench/summary",
		"/api/gb28181/device-mgmt/permission-workbench/devices/resolve",
		"/api/gb28181/device-mgmt/permission-workbench/grants/query",
		"/api/gb28181/device-mgmt/permission-workbench/grant-targets",
		"/api/gb28181/device-mgmt/permission-workbench/assignments",
		"/api/gb28181/device-mgmt/permission-workbench/assignments/departments",
		"/api/gb28181/device-mgmt/permission-workbench/grants/apply",
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			body, err := migrationsfs.FS.ReadFile("migrations/" + name)
			require.NoError(t, err)
			normalized := strings.NewReplacer("`", "", "[", "", "]", "").Replace(strings.ToLower(string(body)))
			normalized = strings.Join(strings.Fields(normalized), " ")
			for _, path := range paths {
				require.Contains(t, normalized, strings.ToLower(path), "工作台 API 必须全部注册")
			}
			for _, token := range []string{
				"设备权限工作台",
				"sys_menu_api",
				"sys_casbin_rule",
				"gb28181:device:assign",
				"gb28181:device:share",
				"deleted_at",
				"device-assignment",
				"/api/gb28181/device-mgmt/assign",
				"/api/gb28181/device-mgmt/assign-dept",
				"/api/gb28181/device-mgmt/device/:id/grants",
			} {
				require.Contains(t, normalized, strings.ToLower(token), "工作台迁移必须收口旧权限并绑定新权限")
			}
			require.Contains(t, normalized, "not exists", "up 迁移必须可重复执行")
			require.Contains(t, normalized, "select distinct 'p'", "Casbin 规则必须去重")
			require.Contains(t, normalized, "join sys_menu_api ma on ma.menu_id=m.id", "Casbin 规则必须复用精确菜单 API 绑定")
		})
	}
}

func TestDevicePermissionWorkbenchMigrationDownFilesDoNotRestoreLegacyBindings(t *testing.T) {
	files := []string{
		"2026-08-24-device-permission-workbench-permissions-down.sql",
		"2026-08-24-device-permission-workbench-permissions-postgresql-down.sql",
		"2026-08-24-device-permission-workbench-permissions-sqlserver-down.sql",
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			body, err := migrationsfs.FS.ReadFile("migrations/" + name)
			require.NoError(t, err)
			normalized := strings.NewReplacer("`", "", "[", "", "]", "").Replace(strings.ToLower(string(body)))
			normalized = strings.Join(strings.Fields(normalized), " ")
			require.Contains(t, normalized, "deleted_at", "down 必须使用可恢复的软删除语义")
			require.Contains(t, normalized, "sys_menu_api")
			require.Contains(t, normalized, "sys_casbin_rule")
			require.Contains(t, normalized, "/api/gb28181/device-mgmt/permission-workbench/")
			for _, legacy := range []string{
				"insert into sys_api",
				"insert into `sys_api`",
				"insert into [sys_api]",
				"assign-dept",
				"device/:id/grants",
			} {
				require.NotContains(t, normalized, legacy, "down 不得重新写入或恢复旧 API 绑定")
			}
		})
	}
}

func TestDevicePermissionWorkbenchFreshBaselinesContainPermissions(t *testing.T) {
	files := []string{
		"uvp-gb28181.sql",
		"postgresql_converted.sql",
		"sqlserver_converted.sql",
	}
	paths := []string{
		"/api/gb28181/device-mgmt/permission-workbench/summary",
		"/api/gb28181/device-mgmt/permission-workbench/devices/resolve",
		"/api/gb28181/device-mgmt/permission-workbench/grants/query",
		"/api/gb28181/device-mgmt/permission-workbench/grant-targets",
		"/api/gb28181/device-mgmt/permission-workbench/assignments",
		"/api/gb28181/device-mgmt/permission-workbench/assignments/departments",
		"/api/gb28181/device-mgmt/permission-workbench/grants/apply",
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("..", "..", "..", "resource", "database", name))
			require.NoError(t, err)
			normalized := strings.NewReplacer("`", "", "[", "", "]", "").Replace(strings.ToLower(string(body)))
			normalized = strings.Join(strings.Fields(normalized), " ")
			for _, path := range paths {
				require.Contains(t, normalized, path, "全量基线必须包含工作台 API，避免空版本表基线化后跳过权限")
			}
			for _, token := range []string{
				"设备权限工作台",
				"device-assignment",
				"gb28181:device:assign",
				"gb28181:device:share",
				"sys_menu_api",
				"sys_casbin_rule",
			} {
				require.Contains(t, normalized, strings.ToLower(token))
			}
		})
	}
}

func TestSIPTraceBusinessSemanticMigrationsContainEquivalentColumns(t *testing.T) {
	files := []string{
		"2026-08-24-sip-trace-business-semantics.sql",
		"2026-08-24-sip-trace-business-semantics-postgresql.sql",
		"2026-08-24-sip-trace-business-semantics-sqlserver.sql",
	}

	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			body, err := migrationsfs.FS.ReadFile("migrations/" + name)
			require.NoError(t, err)
			normalized := strings.ToLower(string(body))
			for _, field := range []string{"from_id", "to_id", "business_code", "business_type", "business_confidence"} {
				require.Contains(t, normalized, field)
			}
			require.Contains(t, normalized, "idx_gb_sip_trace_business_occurred")
		})
	}

	sqlServer, err := migrationsfs.FS.ReadFile("migrations/2026-08-24-sip-trace-business-semantics-sqlserver.sql")
	require.NoError(t, err)
	require.Contains(t, strings.ToLower(string(sqlServer)), "[business_type] nvarchar(64)")
}
