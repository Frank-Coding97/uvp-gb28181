package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const workRecordingPermissionMigration = "2026-09-09-zzz-work-recording-permissions"

var workRecordingPermissionAPIs = []struct {
	method string
	path   string
}{
	{method: "POST", path: "/api/gb28181/work-recordings"},
	{method: "POST", path: "/api/gb28181/work-recordings/:id/stop"},
	{method: "GET", path: "/api/gb28181/work-recordings/status"},
	{method: "GET", path: "/api/gb28181/work-recordings/:id"},
}

func TestWorkRecordingPermissionMigrationsAreScopedAndIdempotent(t *testing.T) {
	for _, suffix := range []string{"", "-postgresql", "-sqlserver"} {
		t.Run(suffix, func(t *testing.T) {
			up := readWorkRecordingPermissionMigration(t, workRecordingPermissionMigration+suffix+".sql")
			down := readWorkRecordingPermissionMigration(t, workRecordingPermissionMigration+suffix+"-down.sql")
			normalizedUp := normalizeWorkRecordingPermissionSQL(up)
			normalizedDown := normalizeWorkRecordingPermissionSQL(down)

			for _, api := range workRecordingPermissionAPIs {
				require.Contains(t, normalizedUp, strings.ToLower(api.path))
				require.Contains(t, normalizedUp, strings.ToLower(api.method))
			}
			for _, permission := range []string{
				"gb28181:work-recording:start",
				"gb28181:work-recording:stop",
			} {
				require.Contains(t, normalizedUp, permission)
			}
			for _, table := range []string{"sys_api", "sys_menu", "sys_role_menu", "sys_menu_api", "sys_casbin_rule"} {
				require.Contains(t, normalizedUp, table)
			}
			require.Contains(t, normalizedUp, "where not exists")
			require.Contains(t, normalizedUp, "select distinct 'p'")
			require.Contains(t, normalizedUp, "join sys_menu_api ma on ma.menu_id=m.id")
			require.NotContains(t, normalizedUp, "cross join sys_api")
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
			for i := 0; i < 2; i++ {
				for _, statement := range splitStatements(sqliteWorkRecordingPermissionSQL(up)) {
					require.NoError(t, db.Exec(statement).Error, statement)
				}
			}

			assertWorkRecordingPermissionCounts(t, db)
		})
	}
}

func TestWorkRecordingPermissionSnapshotsContainManifest(t *testing.T) {
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database", name))
			require.NoError(t, err)
			normalized := normalizeWorkRecordingPermissionSQL(string(body))
			for _, api := range workRecordingPermissionAPIs {
				require.Contains(t, normalized, strings.ToLower(api.path))
				require.Contains(t, normalized, strings.ToLower(api.method))
			}
			for _, permission := range []string{
				"gb28181:work-recording:start",
				"gb28181:work-recording:stop",
			} {
				require.Contains(t, normalized, permission)
			}
			require.Contains(t, normalized, "work-recording")
			require.Contains(t, normalized, "sys_menu_api")
			require.Contains(t, normalized, "sys_casbin_rule")
		})
	}
}

func TestWorkRecordingPermissionMigrationSortsAfterWorkRecordingSchema(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("../../../resource/database/gb28181/migrations"))
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	ups := FilterUpFiles(names, DialectMySQL)
	baseIndex, directoryIndex, permissionIndex := -1, -1, -1
	for i, name := range ups {
		switch name {
		case "2026-09-09-work-recording.sql":
			baseIndex = i
		case "2026-09-09-zz-work-recording-directory.sql":
			directoryIndex = i
		case workRecordingPermissionMigration + ".sql":
			permissionIndex = i
		}
	}
	require.GreaterOrEqual(t, baseIndex, 0)
	require.Greater(t, directoryIndex, baseIndex)
	require.Greater(t, permissionIndex, directoryIndex)
}

func openWorkRecordingPermissionSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	return db
}

func seedWorkRecordingPermissionSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, statement := range []string{
		"CREATE TABLE sys_api(id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT, path TEXT, method TEXT, api_group TEXT, created_at TEXT, updated_at TEXT, deleted_at TEXT, created_by INTEGER)",
		"CREATE TABLE sys_menu(id INTEGER PRIMARY KEY AUTOINCREMENT, parent_id INTEGER, path TEXT, name TEXT, component TEXT, title TEXT, hide INTEGER, disable INTEGER, sort INTEGER, type INTEGER, permission TEXT, icon TEXT, created_at TEXT, updated_at TEXT, deleted_at TEXT, created_by INTEGER)",
		"CREATE TABLE sys_role_menu(role_id INTEGER, menu_id INTEGER, PRIMARY KEY(role_id, menu_id))",
		"CREATE TABLE sys_menu_api(menu_id INTEGER, api_id INTEGER, PRIMARY KEY(menu_id, api_id))",
		"CREATE TABLE sys_casbin_rule(ptype TEXT, v0 TEXT, v1 TEXT, v2 TEXT, v3 TEXT, v4 TEXT, v5 TEXT, PRIMARY KEY(ptype, v0, v1, v2, v3, v4, v5))",
		"INSERT INTO sys_menu(id, parent_id, path, name, type, permission, deleted_at) VALUES(10, 0, '/gb28181/multi-screen-playback', 'multi-screen-playback', 2, '', NULL)",
		"INSERT INTO sys_menu(id, parent_id, path, name, type, permission, deleted_at) VALUES(20, 0, '/unrelated', 'unrelated', 2, 'unrelated', NULL)",
		"INSERT INTO sys_role_menu(role_id, menu_id) VALUES(2, 20)",
		"INSERT INTO sys_api(id, path, method, deleted_at) VALUES(900, '/api/unrelated', 'GET', NULL)",
		"INSERT INTO sys_menu_api(menu_id, api_id) VALUES(20, 900)",
		"INSERT INTO sys_casbin_rule(ptype, v0, v1, v2, v3, v4, v5) VALUES('p', 'role_2', '/api/unrelated', 'GET', '*', '', '')",
	} {
		require.NoError(t, db.Exec(statement).Error, statement)
	}
}

func assertWorkRecordingPermissionCounts(t *testing.T, db *gorm.DB) {
	t.Helper()
	for query, want := range map[string]int64{
		"SELECT COUNT(*) FROM sys_api WHERE path LIKE '/api/gb28181/work-recordings%' AND deleted_at IS NULL":                                                                            4,
		"SELECT COUNT(*) FROM sys_menu WHERE permission IN ('gb28181:work-recording:start','gb28181:work-recording:stop') AND deleted_at IS NULL":                                        2,
		"SELECT COUNT(*) FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id WHERE rm.role_id=1 AND m.permission IN ('gb28181:work-recording:start','gb28181:work-recording:stop')": 2,
		"SELECT COUNT(*) FROM sys_menu_api ma JOIN sys_menu m ON m.id=ma.menu_id WHERE m.permission IN ('gb28181:work-recording:start','gb28181:work-recording:stop')":                   6,
		"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_1' AND v1 LIKE '/api/gb28181/work-recordings%'":                                                                             4,
		"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_2'":                                                                                                                         1,
	} {
		var got int64
		require.NoError(t, db.Raw(query).Scan(&got).Error, query)
		require.Equal(t, want, got, query)
	}
	for _, api := range workRecordingPermissionAPIs {
		var got int64
		require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_1' AND v1=? AND v2=?", api.path, api.method).Scan(&got).Error)
		require.Equal(t, int64(1), got, api.method+" "+api.path)
	}
	var unrelatedMenuAPI int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_menu_api WHERE menu_id=20 AND api_id=900").Scan(&unrelatedMenuAPI).Error)
	require.Equal(t, int64(1), unrelatedMenuAPI)
}

func normalizeWorkRecordingPermissionSQL(sql string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.NewReplacer("`", "", "[", "", "]", "").Replace(sql))), " ")
}

func sqliteWorkRecordingPermissionSQL(sql string) string {
	sql = strings.ReplaceAll(sql, "`", "")
	sql = strings.ReplaceAll(sql, "[", "")
	sql = strings.ReplaceAll(sql, "]", "")
	sql = strings.ReplaceAll(sql, "N'", "'")
	sql = strings.ReplaceAll(sql, "NOW()", "CURRENT_TIMESTAMP")
	sql = strings.ReplaceAll(sql, "CONCAT('role_',rm.role_id)", "('role_' || rm.role_id)")
	return sql
}

func readWorkRecordingPermissionMigration(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations", name))
	require.NoError(t, err)
	return string(body)
}
