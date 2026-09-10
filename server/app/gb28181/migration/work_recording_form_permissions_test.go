package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const workRecordingFormPermissionMigration = "2026-09-09-zzzz-work-recording-form-permissions"

var workRecordingFormPermissionAPIs = []struct {
	method string
	path   string
}{
	{method: "GET", path: "/api/gb28181/work-recordings"},
	{method: "GET", path: "/api/gb28181/work-recordings/:id/form"},
	{method: "PUT", path: "/api/gb28181/work-recordings/:id/form"},
}

func TestWorkRecordingFormPermissionMigrationsAreIdempotentAndScoped(t *testing.T) {
	for _, suffix := range []string{"", "-postgresql", "-sqlserver"} {
		t.Run(suffix, func(t *testing.T) {
			up := readWorkRecordingPermissionMigration(t, workRecordingFormPermissionMigration+suffix+".sql")
			down := readWorkRecordingPermissionMigration(t, workRecordingFormPermissionMigration+suffix+"-down.sql")
			normalizedUp := normalizeWorkRecordingPermissionSQL(up)
			normalizedDown := normalizeWorkRecordingPermissionSQL(down)

			for _, api := range workRecordingFormPermissionAPIs {
				require.Contains(t, normalizedUp, strings.ToLower(api.path))
				require.Contains(t, normalizedUp, strings.ToLower(api.method))
			}
			require.Contains(t, normalizedUp, "gb28181:work-recording:form")
			require.Contains(t, normalizedUp, "sys_menu_api")
			require.Contains(t, normalizedUp, "sys_casbin_rule")
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
			prior := readWorkRecordingPermissionMigration(t, workRecordingPermissionMigration+suffix+".sql")
			for _, statement := range splitStatements(sqliteWorkRecordingPermissionSQL(prior)) {
				require.NoError(t, db.Exec(statement).Error, statement)
			}
			for i := 0; i < 2; i++ {
				for _, statement := range splitStatements(sqliteWorkRecordingPermissionSQL(up)) {
					require.NoError(t, db.Exec(statement).Error, statement)
				}
			}
			assertWorkRecordingFormPermissionCounts(t, db)
		})
	}
}

func TestWorkRecordingFormPermissionSnapshotsContainManifest(t *testing.T) {
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database", name))
			require.NoError(t, err)
			normalized := normalizeWorkRecordingPermissionSQL(string(body))
			for _, api := range workRecordingFormPermissionAPIs {
				require.Contains(t, normalized, strings.ToLower(api.path))
				require.Contains(t, normalized, strings.ToLower(api.method))
			}
			require.Contains(t, normalized, "gb28181:work-recording:form")
			require.Contains(t, normalized, "编辑作业表单")
		})
	}
}

func TestWorkRecordingFormPermissionMigrationSortsAfterStartStopPermissions(t *testing.T) {
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
		priorName := workRecordingPermissionMigration + tc.suffix + ".sql"
		currentName := workRecordingFormPermissionMigration + tc.suffix + ".sql"
		priorIndex, currentIndex := -1, -1
		for i, name := range files {
			switch name {
			case priorName:
				priorIndex = i
			case currentName:
				currentIndex = i
			}
		}
		require.GreaterOrEqual(t, priorIndex, 0, "%s prior permission migration must be discovered", tc.dialect)
		require.Greater(t, currentIndex, priorIndex, "%s form permission migration must run after start/stop permissions", tc.dialect)
		require.FileExists(t, filepath.Join("../../../resource/database/gb28181/migrations", strings.TrimSuffix(currentName, ".sql")+"-down.sql"))
	}
}

func assertWorkRecordingFormPermissionCounts(t *testing.T, db interface {
	Raw(sql string, values ...interface{}) *gorm.DB
}) {
	t.Helper()
	queries := map[string]int64{
		"SELECT COUNT(*) FROM sys_api WHERE path LIKE '/api/gb28181/work-recordings%' AND deleted_at IS NULL":                                                                                                          7,
		"SELECT COUNT(*) FROM sys_menu WHERE permission IN ('gb28181:work-recording:start','gb28181:work-recording:stop','gb28181:work-recording:form') AND deleted_at IS NULL":                                        3,
		"SELECT COUNT(*) FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id WHERE rm.role_id=1 AND m.permission IN ('gb28181:work-recording:start','gb28181:work-recording:stop','gb28181:work-recording:form')": 3,
		"SELECT COUNT(*) FROM sys_menu_api ma JOIN sys_menu m ON m.id=ma.menu_id WHERE m.permission IN ('gb28181:work-recording:start','gb28181:work-recording:stop','gb28181:work-recording:form')":                   13,
		"SELECT COUNT(*) FROM sys_menu_api ma JOIN sys_menu m ON m.id=ma.menu_id WHERE m.permission IN ('gb28181:work-recording:start','gb28181:work-recording:stop')":                   10,
		"SELECT COUNT(*) FROM sys_menu_api ma JOIN sys_menu m ON m.id=ma.menu_id JOIN sys_api a ON a.id=ma.api_id WHERE m.permission IN ('gb28181:work-recording:start','gb28181:work-recording:stop') AND a.method='PUT'": 0,
		"SELECT COUNT(*) FROM sys_menu_api ma JOIN sys_menu m ON m.id=ma.menu_id JOIN sys_api a ON a.id=ma.api_id WHERE m.permission='gb28181:work-recording:form' AND a.method='PUT'": 1,
		"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_1' AND v1 LIKE '/api/gb28181/work-recordings%'":                                                                                                           7,
		"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_2'": 1,
	}
	for query, want := range queries {
		var got int64
		require.NoError(t, db.Raw(query).Scan(&got).Error, query)
		require.Equal(t, want, got, query)
	}
	for _, api := range workRecordingFormPermissionAPIs {
		var got int64
		require.NoError(t, db.Raw("SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_1' AND v1=? AND v2=?", api.path, api.method).Scan(&got).Error)
		require.Equal(t, int64(1), got, api.method+" "+api.path)
	}
}
