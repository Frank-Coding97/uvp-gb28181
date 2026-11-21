package migration

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	migrationsfs "uvplatform.com/uvp-gb28181/resource/database/gb28181"
)

// contractThreshold 契约生效日期:该日期及以后的迁移文件必须
// 三方言齐全 + up/down 成对。存量文件(阈值前)豁免,只约束新文件。
const contractThreshold = "2026-08-14"

// 6.1/6.2:阈值日期及以后的每个 MySQL up 迁移,配套
// -postgresql.sql / -sqlserver.sql / -down.sql 必须齐全。
func TestMigrationFileContract(t *testing.T) {
	entries, err := migrationsfs.FS.ReadDir("migrations")
	// 历史迁移已归档清理,embed 中可能整体没有 migrations 目录 —— 那是预期状态,
	// 不是契约违规(详见 runner.go 的 embedSource.UpFiles)。
	if errors.Is(err, fs.ErrNotExist) {
		t.Skip("历史迁移已归档清理,embed 中暂无 migrations 目录,契约断言空转")
	}
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

// sqlWithoutComments 去掉整行/行尾 "--" 注释后压成单行小写。
// 仅用于断言语句体里有没有 DELETE/INSERT —— 这些迁移文件不含带 "--" 的字符串字面量。
func sqlWithoutComments(body string) string {
	var b strings.Builder
	for _, line := range strings.Split(body, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.ToLower(strings.Join(strings.Fields(b.String()), " "))
}

// 2026-11-10 的权限基线补齐是 NOT EXISTS 型「只补不改」迁移，配套 down 必须保持 no-op。
//
// 为什么：被补的 5 个接口与 32 条 casbin 策略**本来就在 baseline/seeds 里**，新装库自带，
// 只有更早升级上来的老库缺。up 只补恰好缺的那几条，事后无法判定补了哪些
// （两表都没有能区分"基线导入"与"本次插入"的标记列）⇒ 按书面清单删必然误删。
// 实测：全新基线库上误跑旧 down，sys_api −5、sys_menu_api −5、sys_casbin_rule −32；
// 老库 up→down 后 sys_casbin_rule 比升级前**净少 24 条**（把无关的既有授权删了，
// 反而让前端继续 403）。
//
// 2026-11-21 的种子补齐同理（灌的按钮/权限码/接口映射/openapi 锁行在 r9 基线里全部自带），
// 而且它的旧 down 更危险：删掉 sys_openapi_security_state 那行会让下一次
// -migrate-down 的前置校验 fail-closed，降级链路自己卡死自己。
//
// 这是一道反向护栏：谁把 down 改回 DELETE/UPDATE，这里立刻红，逼他重读上面这段。
func TestBaselineBackfillDownStaysNoop(t *testing.T) {
	cases := []struct {
		name    string // 迁移前缀
		downs   []string
		applies []string // 需要断言是 NOT EXISTS 型幂等补齐的 up 文件
	}{
		{
			name: "2026-11-10 权限基线补齐",
			downs: []string{
				"2026-11-10-permission-baseline-backfill-down.sql",
				"2026-11-10-permission-baseline-backfill-postgresql-down.sql",
				"2026-11-10-permission-baseline-backfill-sqlserver-down.sql",
				"2026-11-10-permission-baseline-backfill-sqlite-down.sql",
			},
			applies: []string{
				"2026-11-10-permission-baseline-backfill.sql",
				"2026-11-10-permission-baseline-backfill-postgresql.sql",
				"2026-11-10-permission-baseline-backfill-sqlserver.sql",
				"2026-11-10-permission-baseline-backfill-sqlite.sql",
			},
		},
		{
			name: "2026-11-21 SQLite 种子补齐",
			downs: []string{
				"2026-11-21-sqlite-seed-catchup-down.sql",
				"2026-11-21-sqlite-seed-catchup-sqlite-down.sql",
			},
			applies: []string{
				"2026-11-21-sqlite-seed-catchup.sql",
				"2026-11-21-sqlite-seed-catchup-postgresql.sql",
				"2026-11-21-sqlite-seed-catchup-sqlserver.sql",
				"2026-11-21-sqlite-seed-catchup-sqlite.sql",
			},
		},
		{
			// 与 2026-11-10 的 down 有重叠路径，两个都 no-op 才自洽。
			name: "2026-10-29 地图视图权限补齐",
			downs: []string{
				"2026-10-29-map-view-permissions-down.sql",
				"2026-10-29-map-view-permissions-postgresql-down.sql",
				"2026-10-29-map-view-permissions-sqlserver-down.sql",
				"2026-10-29-map-view-permissions-sqlite-down.sql",
			},
			applies: []string{
				"2026-10-29-map-view-permissions.sql",
				"2026-10-29-map-view-permissions-postgresql.sql",
				"2026-10-29-map-view-permissions-sqlserver.sql",
				"2026-10-29-map-view-permissions-sqlite.sql",
			},
		},
		{
			name: "2026-10-13 级联发布组权限补齐",
			downs: []string{
				"2026-10-13-cascade-publish-group-permission-down.sql",
				"2026-10-13-cascade-publish-group-permission-postgresql-down.sql",
				"2026-10-13-cascade-publish-group-permission-sqlserver-down.sql",
				"2026-10-13-cascade-publish-group-permission-sqlite-down.sql",
			},
			applies: []string{
				"2026-10-13-cascade-publish-group-permission.sql",
				"2026-10-13-cascade-publish-group-permission-postgresql.sql",
				"2026-10-13-cascade-publish-group-permission-sqlserver.sql",
				"2026-10-13-cascade-publish-group-permission-sqlite.sql",
			},
		},
		{
			name: "2026-10-18 HTTPS 管理权限补齐",
			downs: []string{
				"2026-10-18-https-management-down.sql",
				"2026-10-18-https-management-postgresql-down.sql",
				"2026-10-18-https-management-sqlserver-down.sql",
				"2026-10-18-https-management-sqlite-down.sql",
			},
			applies: []string{
				"2026-10-18-https-management.sql",
				"2026-10-18-https-management-postgresql.sql",
				"2026-10-18-https-management-sqlserver.sql",
				"2026-10-18-https-management-sqlite.sql",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, upName := range c.applies {
				up, err := migrationsfs.FS.ReadFile("migrations/" + upName)
				require.NoError(t, err, "缺少 %s", upName)
				require.Contains(t, sqlWithoutComments(string(up)), "not exists",
					"%s 已不再是 NOT EXISTS 型幂等补齐 —— down 为 no-op 的前提变了，请重新评估", upName)
			}
			for _, downName := range c.downs {
				down, err := migrationsfs.FS.ReadFile("migrations/" + downName)
				require.NoError(t, err, "缺少 %s", downName)
				body := sqlWithoutComments(string(down))
				require.Contains(t, body, "select 1", "%s 应保持 no-op（SELECT 1）", downName)
				for _, forbidden := range []string{"delete", "insert", "update"} {
					require.NotContains(t, body, forbidden,
						"%s 不许含 %s：按清单硬删会连基线自带的接口登记/授权一起删掉，详见文件内注释",
						downName, strings.ToUpper(forbidden))
				}
			}
		})
	}
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

// ⭐ 断言对象已从「迁移块」换成「种子行」。
//
// 旧写法找的是基线里逐字内嵌的 `-- realtime-log-menu-icon:start` 块，并断言块里
// 有 `set icon='lucide:terminal'`。基线改成「从开发库 dump」之后不再内嵌迁移正文
// ——新装基线里这条菜单是一行**种子数据**（INSERT ... VALUES），图标与权限码写在
// 同一个元组里。契约（新装环境该菜单必须有图标）没变，换的是它的载体。
//
// 判据锚在**权限码**而不是 path：该菜单 path 被 2026-09-15 迁移改过，按
// /gb28181/realtime-log 定位会静默命中 0 行 —— 与 TestRealtimeLogMenuTitleRename
// 同一条教训。
func TestRealtimeLogMenuIconFreshBaselinesCarryIcon(t *testing.T) {
	const permission = "gb28181:log:view"
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("..", "..", "..", "resource", "database", name))
			require.NoError(t, err)
			menu := menuSeedTuple(t, normalizeSQL(string(body)), permission)
			require.Contains(t, menu, "lucide:terminal",
				"实时日志菜单的种子行必须带 lucide:Terminal 图标,否则新装环境该菜单无图标")
		})
	}
}

// menuSeedTuple 从归一化后的基线文本里取出 permission 为该值的那条菜单种子元组。
//
// 归一化把种子压成 `insert into sys_menu (...) values (140491, ...), (140492, ...)`，
// 所以按 `), (` 切开，每个片段就是一条菜单。跨表也不会误命中：权限码只出现在
// sys_menu.permission 这一列（sys_menu_api / sys_casbin_rule 存的是 id 与接口路径）。
func menuSeedTuple(t *testing.T, normalized, permission string) string {
	t.Helper()
	needle := "'" + permission + "'"
	for _, tuple := range strings.Split(normalized, "), (") {
		if strings.Contains(tuple, needle) {
			return tuple
		}
	}
	require.FailNowf(t, "基线里找不到该菜单",
		"新装基线必须含 permission=%s 的菜单种子行", permission)
	return ""
}
