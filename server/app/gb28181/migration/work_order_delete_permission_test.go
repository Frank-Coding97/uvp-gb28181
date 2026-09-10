package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const workOrderDeleteMigration = "2026-09-10-zzzzz-work-order-delete-permission"

var workOrderDeleteAPIs = []struct {
	method string
	path   string
}{
	{method: "DELETE", path: "/api/gb28181/work-orders/:id"},
	{method: "POST", path: "/api/gb28181/work-orders/batch-delete"},
}

// 删除是独立于「结束录像」的动作，需要自己的权限点与 Casbin 规则，
// 否则只有超管能删，普通作业员点删除会直接 403。
func TestWorkOrderDeletePermissionMigrationRegistersAndIsIdempotent(t *testing.T) {
	for _, suffix := range []string{"", "-postgresql", "-sqlserver"} {
		t.Run(suffix, func(t *testing.T) {
			up := readWorkRecordingPermissionMigration(t, workOrderDeleteMigration+suffix+".sql")
			down := readWorkRecordingPermissionMigration(t, workOrderDeleteMigration+suffix+"-down.sql")
			normalizedUp := normalizeWorkRecordingPermissionSQL(up)
			normalizedDown := normalizeWorkRecordingPermissionSQL(down)

			require.Contains(t, normalizedUp, "gb28181:work-order:delete")
			require.Contains(t, normalizedUp, "permission_gb28181_work_order_delete")
			for _, api := range workOrderDeleteAPIs {
				require.Contains(t, normalizedUp, strings.ToLower(api.path))
				require.Contains(t, normalizedUp, strings.ToLower(api.method))
			}
			require.Contains(t, normalizedUp, "where not exists")
			require.Contains(t, normalizedUp, "select distinct 'p'", "同一角色可能通过多个菜单命中同一 API，写入前必须去重")
			require.Contains(t, normalizedUp, "join sys_menu_api ma on ma.menu_id=m.id", "Casbin 规则必须复用菜单与 API 的精确绑定")
			require.NotContains(t, normalizedUp, "cross join sys_api")
			// down 必须 fail closed：已授予的删除权限不能被自动回滚悄悄收回。
			require.NotContains(t, normalizedDown, "delete")
			require.NotContains(t, normalizedDown, "update")
			require.Contains(t, normalizedDown, "automatic schema rollback is disabled")

			db := openWorkRecordingPermissionSQLite(t)
			seedWorkRecordingPermissionSchema(t, db)
			for i := 0; i < 2; i++ {
				for _, statement := range splitStatements(sqliteWorkRecordingPermissionSQL(up)) {
					require.NoError(t, db.Exec(statement).Error, statement)
				}
			}
			assertWorkOrderDeleteCounts(t, db)
		})
	}
}

func TestWorkOrderDeleteSnapshotsContainTheDeletePermission(t *testing.T) {
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database", name))
			require.NoError(t, err)
			normalized := normalizeWorkRecordingPermissionSQL(string(body))
			require.Contains(t, normalized, "gb28181:work-order:delete")
			for _, api := range workOrderDeleteAPIs {
				require.Contains(t, normalized, strings.ToLower(api.path))
				require.Contains(t, normalized, strings.ToLower(api.method))
			}
		})
	}
}

// 权限必须在「旧录像权限退役」之后补，否则退役迁移会把同一批规则又清掉。
func TestWorkOrderDeleteMigrationSortsAfterTheRetirement(t *testing.T) {
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
		retireName := "2026-09-10-zzzz-work-recording-retire" + tc.suffix + ".sql"
		currentName := workOrderDeleteMigration + tc.suffix + ".sql"
		retireIndex, currentIndex := -1, -1
		for i, name := range files {
			switch name {
			case retireName:
				retireIndex = i
			case currentName:
				currentIndex = i
			}
		}
		require.GreaterOrEqual(t, retireIndex, 0, "%s 旧权限退役迁移必须被扫描到", tc.dialect)
		require.Greater(t, currentIndex, retireIndex, "%s 删除权限迁移必须排在旧权限退役之后", tc.dialect)
	}
}

func assertWorkOrderDeleteCounts(t *testing.T, db *gorm.DB) {
	t.Helper()
	for query, want := range map[string]int64{
		"SELECT COUNT(*) FROM sys_menu WHERE deleted_at IS NULL AND permission='gb28181:work-order:delete'":                                        1,
		"SELECT COUNT(*) FROM sys_api WHERE deleted_at IS NULL AND path LIKE '/api/gb28181/work-orders%'":                                          2,
		"SELECT COUNT(*) FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id WHERE rm.role_id=1 AND m.permission='gb28181:work-order:delete'": 1,
		"SELECT COUNT(*) FROM sys_menu_api ma JOIN sys_menu m ON m.id=ma.menu_id WHERE m.permission='gb28181:work-order:delete'":                   2,
		"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_1' AND v1 LIKE '/api/gb28181/work-orders%'":                                           2,
		"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_2'":                                                                                   1,
		"SELECT COUNT(*) FROM sys_menu_api WHERE menu_id=20 AND api_id=900":                                                                        1,
	} {
		var got int64
		require.NoError(t, db.Raw(query).Scan(&got).Error, query)
		require.EqualValues(t, want, got, query)
	}
	for _, api := range workOrderDeleteAPIs {
		var got int64
		require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_1' AND v1=? AND v2=?", api.path, api.method).Scan(&got).Error)
		require.EqualValues(t, 1, got, api.method+" "+api.path)
	}
}
