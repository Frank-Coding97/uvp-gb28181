package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const workOrderMenuMigration = "2026-09-10-zz-work-order-menu"

var workOrderMenuAPIs = []struct {
	method string
	path   string
}{
	{method: "POST", path: "/api/gb28181/work-orders"},
	{method: "GET", path: "/api/gb28181/work-orders"},
	{method: "GET", path: "/api/gb28181/work-orders/active"},
	{method: "GET", path: "/api/gb28181/work-orders/:id"},
	{method: "POST", path: "/api/gb28181/work-orders/:id/stop"},
	{method: "GET", path: "/api/gb28181/work-orders/:id/download"},
	{method: "GET", path: "/api/gb28181/work-orders/:id/files/:fileId"},
}

var workOrderPermissions = []string{
	"gb28181:work-order:create",
	"gb28181:work-order:stop",
	"gb28181:work-order:view",
}

func TestWorkOrderMenuMigrationsRegisterMenuAPIsAndAreIdempotent(t *testing.T) {
	for _, suffix := range []string{"", "-postgresql", "-sqlserver"} {
		t.Run(suffix, func(t *testing.T) {
			up := readWorkRecordingPermissionMigration(t, workOrderMenuMigration+suffix+".sql")
			down := readWorkRecordingPermissionMigration(t, workOrderMenuMigration+suffix+"-down.sql")
			normalizedUp := normalizeWorkRecordingPermissionSQL(up)
			normalizedDown := normalizeWorkRecordingPermissionSQL(down)

			require.Contains(t, normalizedUp, "/gb28181/work-orders")
			require.Contains(t, normalizedUp, "gb28181/work-orders/index", "菜单必须指向作业单页面组件")
			require.Contains(t, normalizedUp, "lucide:clipboardlist")
			for _, permission := range workOrderPermissions {
				require.Contains(t, normalizedUp, permission)
			}
			for _, api := range workOrderMenuAPIs {
				require.Contains(t, normalizedUp, strings.ToLower(api.path))
				require.Contains(t, normalizedUp, strings.ToLower(api.method))
			}
			require.Contains(t, normalizedUp, "where not exists")
			require.Contains(t, normalizedUp, "select distinct 'p'", "同一角色可能通过多个菜单命中同一 API，写入前必须去重")
			require.Contains(t, normalizedUp, "join sys_menu_api ma on ma.menu_id=m.id", "Casbin 规则必须复用菜单与 API 的精确绑定")
			require.NotContains(t, normalizedUp, "cross join sys_api")
			// 权限元数据 fail closed：down 迁移不得删除或篡改既有授权。
			require.NotContains(t, normalizedDown, "delete")
			require.NotContains(t, normalizedDown, "update")
			require.Contains(t, normalizedDown, "automatic schema rollback is disabled")
			switch suffix {
			case "":
				require.Contains(t, normalizedDown, "signal sqlstate '45000'")
			case "-postgresql":
				require.Contains(t, normalizedDown, "raise exception")
			case "-sqlserver":
				require.Contains(t, normalizedDown, "throw 51000")
			}

			db := openWorkRecordingPermissionSQLite(t)
			seedWorkRecordingPermissionSchema(t, db)
			// 跑两遍证明幂等：sys_casbin_rule 主键会让重复插入直接报错。
			for i := 0; i < 2; i++ {
				for _, statement := range splitStatements(sqliteWorkRecordingPermissionSQL(up)) {
					require.NoError(t, db.Exec(statement).Error, statement)
				}
			}
			assertWorkOrderMenuCounts(t, db)
		})
	}
}

func TestWorkOrderMenuSnapshotsContainManifest(t *testing.T) {
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database", name))
			require.NoError(t, err)
			normalized := normalizeWorkRecordingPermissionSQL(string(body))
			require.Contains(t, normalized, "/gb28181/work-orders")
			require.Contains(t, normalized, "gb28181/work-orders/index")
			for _, permission := range workOrderPermissions {
				require.Contains(t, normalized, permission)
			}
			for _, api := range workOrderMenuAPIs {
				require.Contains(t, normalized, strings.ToLower(api.path))
				require.Contains(t, normalized, strings.ToLower(api.method))
			}
		})
	}
}

// The menu has to be discoverable after the GB28181 flattening migration moved
// everything to the root level, so the new entry must sort alongside them.
func TestWorkOrderMenuMigrationFollowsTheWorkRecordingMigrations(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("../../../resource/database/gb28181/migrations"))
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	for _, tc := range []struct {
		dialect Dialect
		suffix  string
	}{
		{dialect: DialectMySQL, suffix: ""},
		{dialect: DialectPostgres, suffix: "-postgresql"},
		{dialect: DialectSQLServer, suffix: "-sqlserver"},
	} {
		files := FilterUpFiles(names, tc.dialect)
		priorName := "2026-09-10-work-recording-batch" + tc.suffix + ".sql"
		currentName := workOrderMenuMigration + tc.suffix + ".sql"
		priorIndex, currentIndex := -1, -1
		for i, name := range files {
			switch name {
			case priorName:
				priorIndex = i
			case currentName:
				currentIndex = i
			}
		}
		require.GreaterOrEqual(t, priorIndex, 0, "%s 录像台账迁移必须被扫描到", tc.dialect)
		require.Greater(t, currentIndex, priorIndex, "%s 作业单菜单迁移必须排在录像台账之后", tc.dialect)
	}
}

func assertWorkOrderMenuCounts(t *testing.T, db *gorm.DB) {
	t.Helper()
	for query, want := range map[string]int64{
		// 一张菜单 + 三个按钮权限
		"SELECT COUNT(*) FROM sys_menu WHERE deleted_at IS NULL AND (path='/gb28181/work-orders' OR permission LIKE 'gb28181:work-order:%')":                                          4,
		"SELECT COUNT(*) FROM sys_api WHERE deleted_at IS NULL AND path LIKE '/api/gb28181/work-orders%'":                                                                             7,
		"SELECT COUNT(*) FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id WHERE rm.role_id=1 AND (m.path='/gb28181/work-orders' OR m.permission LIKE 'gb28181:work-order:%')": 4,
		// view 绑 5 个 GET，create 绑 POST 建单，stop 绑 POST 结束 → 7 条精确绑定
		"SELECT COUNT(*) FROM sys_menu_api ma JOIN sys_menu m ON m.id=ma.menu_id WHERE m.permission LIKE 'gb28181:work-order:%'": 7,
		"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_1' AND v1 LIKE '/api/gb28181/work-orders%'":                         7,
	} {
		var got int64
		require.NoError(t, db.Raw(query).Scan(&got).Error, query)
		require.EqualValues(t, want, got, query)
	}
	for _, api := range workOrderMenuAPIs {
		var got int64
		require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_1' AND v1=? AND v2=?", api.path, api.method).Scan(&got).Error)
		require.EqualValues(t, 1, got, api.method+" "+api.path)
	}
	// 既有授权不能被新迁移污染。
	var untouched int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_2'").Scan(&untouched).Error)
	require.EqualValues(t, 1, untouched)
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_menu_api WHERE menu_id=20 AND api_id=900").Scan(&untouched).Error)
	require.EqualValues(t, 1, untouched)
}
