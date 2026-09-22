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

func TestRealtimeBusinessLogPermissionMigrations(t *testing.T) {
	for _, name := range []string{
		"2026-09-10-realtime-business-log-permissions.sql",
		"2026-09-10-realtime-business-log-permissions-postgresql.sql",
		"2026-09-10-realtime-business-log-permissions-sqlserver.sql",
	} {
		t.Run(name, func(t *testing.T) {
			body, err := migrationsfs.FS.ReadFile("migrations/" + name)
			require.NoError(t, err)
			normalized := strings.NewReplacer("`", "", "[", "", "]", "").Replace(strings.ToLower(string(body)))
			normalized = strings.Join(strings.Fields(normalized), " ")
			for _, token := range []string{"/api/gb28181/logs/stream", "/gb28181/realtime-log", "gb28181:log:view", "sys_menu_api", "sys_casbin_rule", "not exists"} {
				require.Contains(t, normalized, token)
			}
			require.Contains(t, normalized, "where m.permission='gb28181:log:view'", "API 只能绑定到独立实时日志权限")
		})
	}
}

func TestRealtimeConsoleLogMenuRenameMigrations(t *testing.T) {
	for _, name := range []string{
		"2026-09-14-realtime-console-log-menu.sql",
		"2026-09-14-realtime-console-log-menu-postgresql.sql",
		"2026-09-14-realtime-console-log-menu-sqlserver.sql",
	} {
		t.Run(name, func(t *testing.T) {
			body, err := migrationsfs.FS.ReadFile("migrations/" + name)
			require.NoError(t, err)
			normalized := strings.ToLower(string(body))
			require.Contains(t, normalized, "/gb28181/realtime-log")
			require.Contains(t, normalized, "/api/gb28181/logs/stream")
			require.Contains(t, normalized, "实时日志控制台")
			require.Contains(t, normalized, "实时控制台日志流")
		})
	}
}

func TestStreamProbeAsyncPermissionMigrations(t *testing.T) {
	for _, name := range []string{
		"2026-09-15-stream-probe-async.sql",
		"2026-09-15-stream-probe-async-postgresql.sql",
		"2026-09-15-stream-probe-async-sqlserver.sql",
	} {
		t.Run(name, func(t *testing.T) {
			body, err := migrationsfs.FS.ReadFile("migrations/" + name)
			require.NoError(t, err)
			normalized := strings.NewReplacer("`", "", "[", "", "]", "").Replace(strings.ToLower(string(body)))
			normalized = strings.Join(strings.Fields(normalized), " ")
			for _, token := range []string{
				"/api/gb28181/stream-probes/operations/:operationid",
				"gb28181:play:diagnose",
				"sys_menu_api",
				"sys_casbin_rule",
				"not exists",
			} {
				require.Contains(t, normalized, token)
			}
		})
	}
	body, err := migrationsfs.FS.ReadFile("migrations/2026-09-15-stream-probe-async-down.sql")
	require.NoError(t, err)
	normalized := strings.ToLower(string(body))
	require.Contains(t, normalized, "deleted_at")
	require.Contains(t, normalized, "/api/gb28181/stream-probes/operations/:operationid")
}

func TestStreamProbeAsyncFreshBaselinesContainQueryAPI(t *testing.T) {
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		body, err := os.ReadFile(filepath.Join("..", "..", "..", "resource", "database", name))
		require.NoError(t, err)
		normalized := strings.ToLower(string(body))
		require.Contains(t, normalized, "/api/gb28181/stream-probes/operations/:operationid")
		require.Contains(t, normalized, "gb28181:play:diagnose")
	}
}

func TestZLMNodeEnabledMySQLPermissionComparisonNormalizesCollation(t *testing.T) {
	files := []struct {
		name string
		read func() ([]byte, error)
	}{
		{
			name: "incremental migration",
			read: func() ([]byte, error) {
				return migrationsfs.FS.ReadFile("migrations/2026-09-18-zlm-node-enabled.sql")
			},
		},
		{
			name: "fresh MySQL baseline",
			read: func() ([]byte, error) {
				return os.ReadFile(filepath.Join("..", "..", "..", "resource", "database", "uvp-gb28181.sql"))
			},
		},
	}

	for _, file := range files {
		t.Run(file.name, func(t *testing.T) {
			body, err := file.read()
			require.NoError(t, err)
			normalized := strings.NewReplacer("`", "", "\n", " ", "\t", " ").Replace(strings.ToLower(string(body)))
			normalized = strings.Join(strings.Fields(normalized), " ")
			require.Contains(t, normalized, "convert(p.v1 using utf8mb4) collate utf8mb4_unicode_ci=a.path")
			require.Contains(t, normalized, "convert(p.v2 using utf8mb4) collate utf8mb4_unicode_ci=a.method")
		})
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

// normalizeSQL 抹平三方言的差异:去掉标识符包裹(反引号 / 方括号)、
// 去掉 SQL Server 的 N 字面量前缀,并把空白折叠为单空格,便于对同一段语义做跨方言断言。
func normalizeSQL(body string) string {
	replaced := strings.NewReplacer("`", "", "[", "", "]", "", "n'", "'").Replace(strings.ToLower(body))
	return strings.Join(strings.Fields(replaced), " ")
}

// 菜单改名收口:三方言 up 必须把「实时日志控制台」改写为「运行日志」,
// 且一律按权限码定位 —— 该菜单 path 已被 2026-09-15 迁移改过,
// 继续按 /gb28181/realtime-log 定位会静默命中 0 行。
func TestRealtimeLogMenuTitleRenameMigrations(t *testing.T) {
	files := []string{
		"2026-09-21-rename-realtime-log-menu-title.sql",
		"2026-09-21-rename-realtime-log-menu-title-postgresql.sql",
		"2026-09-21-rename-realtime-log-menu-title-sqlserver.sql",
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			body, err := migrationsfs.FS.ReadFile("migrations/" + name)
			require.NoError(t, err)
			normalized := normalizeSQL(string(body))
			require.Contains(t, normalized, "set title='运行日志'", "up 必须把菜单标题改写为运行日志")
			require.NotContains(t, normalized, "set title='实时日志控制台'", "up 不得写回旧名")
			require.Contains(t, normalized, "permission='gb28181:log:view'", "菜单 path 已迁移,必须按权限码定位")
			require.NotContains(t, normalized, "where path=", "按 path 定位会静默命中 0 行")
			require.Contains(t, normalized, "deleted_at is null", "必须排除软删除行")
		})
	}
}

func TestRealtimeLogMenuTitleRenameDownMigrations(t *testing.T) {
	files := []string{
		"2026-09-21-rename-realtime-log-menu-title-down.sql",
		"2026-09-21-rename-realtime-log-menu-title-postgresql-down.sql",
		"2026-09-21-rename-realtime-log-menu-title-sqlserver-down.sql",
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			body, err := migrationsfs.FS.ReadFile("migrations/" + name)
			require.NoError(t, err)
			normalized := normalizeSQL(string(body))
			require.Contains(t, normalized, "set title='实时日志控制台'", "down 必须能回退到旧名")
			require.Contains(t, normalized, "and title='运行日志'", "down 只能改回由本迁移改名的行,不得误伤其它菜单")
			require.Contains(t, normalized, "permission='gb28181:log:view'")
		})
	}
}

// 三份全量基线都要带上改名块,且必须排在「种菜块」之后 ——
// 基线是按文件顺序执行的,改名块若排在前面,末尾的种菜会把名字改回旧名。
func TestRealtimeLogMenuRenameFreshBaselinesApplyRenameLast(t *testing.T) {
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("..", "..", "..", "resource", "database", name))
			require.NoError(t, err)
			normalized := normalizeSQL(string(body))
			seeded := strings.Index(normalized, "'gb28181/realtime-log/index','实时日志控制台'")
			renamed := strings.Index(normalized, "realtime-log-menu-rename:start")
			require.GreaterOrEqual(t, seeded, 0, "基线必须仍然种出该菜单,否则新装环境没有这条菜单")
			require.GreaterOrEqual(t, renamed, 0, "全量基线必须包含改名块,否则新装环境停留在旧名")
			require.Greater(t, renamed, seeded, "改名块必须排在种菜块之后,否则基线跑完名字会被改回旧名")
			require.Contains(t, normalized[renamed:], "set title='运行日志'")
		})
	}
}

// 菜单补图标:三方言 up 必须把 icon 写成 lucide 值,且定位锚点不能退化成 path。
// 背景:该菜单建行时漏了 icon,同组其余四项的 icon 都由各自建菜单迁移带上。
func TestRealtimeLogMenuIconMigrations(t *testing.T) {
	files := []string{
		"2026-09-21-realtime-log-menu-icon.sql",
		"2026-09-21-realtime-log-menu-icon-postgresql.sql",
		"2026-09-21-realtime-log-menu-icon-sqlserver.sql",
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			body, err := migrationsfs.FS.ReadFile("migrations/" + name)
			require.NoError(t, err)
			normalized := normalizeSQL(string(body))
			require.Contains(t, normalized, "set icon='lucide:terminal'")
			require.Contains(t, normalized, "permission='gb28181:log:view'", "菜单 path 已迁移,必须按权限码定位")
			require.NotContains(t, normalized, "where path=", "按 path 定位会静默命中 0 行")
			require.Contains(t, normalized, "deleted_at is null")
			require.Contains(t, normalized, "icon is null", "原值为空串或 NULL,幂等条件两种都要覆盖")
		})
	}
}

func TestRealtimeLogMenuIconDownMigrations(t *testing.T) {
	files := []string{
		"2026-09-21-realtime-log-menu-icon-down.sql",
		"2026-09-21-realtime-log-menu-icon-postgresql-down.sql",
		"2026-09-21-realtime-log-menu-icon-sqlserver-down.sql",
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			body, err := migrationsfs.FS.ReadFile("migrations/" + name)
			require.NoError(t, err)
			normalized := normalizeSQL(string(body))
			require.Contains(t, normalized, "set icon=''", "该菜单补图标前就是空串,down 恢复为 ''")
			require.Contains(t, normalized, "and icon='lucide:terminal'", "down 只能清理由本迁移写入的行")
			require.Contains(t, normalized, "permission='gb28181:log:view'")
		})
	}
}

// ⭐ 把「迁移写进 DB 的 lucide 名」与「前端白名单」钉在一起。
// menu-item-icon.vue 的回落链是 lucide icon → svg_icon → icon 组件:
// 写一个没注册的名字 ⇒ 三条全落空 ⇒ 图标区整块不渲染,且不报错、不回退 svg_icon。
// 只检查本迁移涉及的 lucide 名,不扫全库 —— 基线历史块里仍有已退役菜单的
// Waypoints / Settings2 等未注册名,扫全量会误报。
func TestRealtimeLogMenuIconIsRegisteredInFrontendWhitelist(t *testing.T) {
	whitelist, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "web", "src", "utils", "lucide-menu-icons.ts"))
	require.NoError(t, err)
	source := string(whitelist)

	pattern := regexp.MustCompile(`lucide:([A-Za-z0-9]+)`)
	for _, name := range []string{
		"2026-09-21-realtime-log-menu-icon.sql",
		"2026-09-21-realtime-log-menu-icon-postgresql.sql",
		"2026-09-21-realtime-log-menu-icon-sqlserver.sql",
	} {
		t.Run(name, func(t *testing.T) {
			body, err := migrationsfs.FS.ReadFile("migrations/" + name)
			require.NoError(t, err)
			matches := pattern.FindAllStringSubmatch(string(body), -1)
			require.NotEmpty(t, matches, "迁移里必须出现 lucide: 图标名")
			for _, m := range matches {
				iconName := m[1]
				require.Contains(t, source, "\n  "+iconName+",\n",
					"图标 %s 未注册进 lucide-menu-icons.ts 白名单(import 与对象两处都要),否则菜单静默无图标", iconName)
			}
		})
	}
}

func TestRealtimeLogMenuIconFreshBaselinesCarryIcon(t *testing.T) {
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("..", "..", "..", "resource", "database", name))
			require.NoError(t, err)
			normalized := normalizeSQL(string(body))
			iconBlock := strings.Index(normalized, "realtime-log-menu-icon:start")
			require.GreaterOrEqual(t, iconBlock, 0, "全量基线必须包含补图标块,否则新装环境该菜单仍无图标")
			require.Contains(t, normalized[iconBlock:], "set icon='lucide:terminal'")
		})
	}
}
