package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const workRecordingRetireMigration = "2026-09-10-zzzz-work-recording-retire"

// 历史迁移写入的、现已随 P4 一起失效的权限点。
var workRecordingRetirePermissions = []string{
	"gb28181:work-recording:start",
	"gb28181:work-recording:stop",
	"gb28181:work-recording:form",
}

// 造出待退役记录的两个历史迁移。
var workRecordingRetireSources = []string{
	"2026-09-09-zzz-work-recording-permissions.sql",
	"2026-09-09-zzzz-work-recording-form-permissions.sql",
}

func TestWorkRecordingRetireMigrationIsThreeDialectAndFailClosed(t *testing.T) {
	for _, suffix := range []string{"", "-postgresql", "-sqlserver"} {
		t.Run(suffix, func(t *testing.T) {
			up := normalizeWorkRecordingPermissionSQL(readWorkRecordingPermissionMigration(t, workRecordingRetireMigration+suffix+".sql"))
			down := normalizeWorkRecordingPermissionSQL(readWorkRecordingPermissionMigration(t, workRecordingRetireMigration+suffix+"-down.sql"))

			// 必须命中已删除端点域内的全部记录，且不得误伤其它路径。
			require.Contains(t, up, "like '/api/gb28181/work-recordings%'")
			for _, permission := range workRecordingRetirePermissions {
				require.Contains(t, up, permission)
			}
			// 关联表无 deleted_at，只能物理删除；菜单/API 必须软删保留审计痕迹。
			require.Contains(t, up, "delete from sys_casbin_rule")
			require.Contains(t, up, "delete from sys_menu_api")
			require.Contains(t, up, "delete from sys_role_menu")
			require.Contains(t, up, "update sys_menu set deleted_at")
			require.Contains(t, up, "update sys_api set deleted_at")
			// 退役域必须收在 work-recordings 内，不能出现无条件删除。
			require.NotContains(t, up, "delete from sys_menu where")
			require.NotContains(t, up, "delete from sys_api where")

			// 退役不可逆：down 必须 fail closed。
			require.NotContains(t, down, "insert")
			require.NotContains(t, down, "update")
			require.Contains(t, down, "automatic schema rollback is disabled")
			switch suffix {
			case "":
				require.Contains(t, down, "signal sqlstate '45000'")
			case "-postgresql":
				require.Contains(t, down, "raise exception")
			case "-sqlserver":
				require.Contains(t, down, "throw 51000")
			}
		})
	}
}

func TestWorkRecordingRetireMigrationRemovesOrphanPermissionsAndIsIdempotent(t *testing.T) {
	db := openWorkRecordingPermissionSQLite(t)
	seedWorkRecordingPermissionSchema(t, db)
	// 先按历史迁移造出现网真实存在的待退役记录。
	for _, name := range workRecordingRetireSources {
		for _, statement := range splitStatements(sqliteWorkRecordingPermissionSQL(readWorkRecordingPermissionMigration(t, name))) {
			require.NoError(t, db.Exec(statement).Error, statement)
		}
	}
	// 前置断言：待退役记录确实被造出来了，否则本测试会空转通过。
	assertWorkRecordingRetirePreconditions(t, db)

	// 注意：这里必须用原始文本切分语句。归一化会把换行折叠成空格，
	// 整份文件会被当成一条语句，只有第一条 DELETE 能执行。
	up := readWorkRecordingPermissionMigration(t, workRecordingRetireMigration+".sql")
	// 跑两遍证明幂等。
	for i := 0; i < 2; i++ {
		for _, statement := range splitStatements(sqliteWorkRecordingPermissionSQL(up)) {
			require.NoError(t, db.Exec(statement).Error, statement)
		}
	}
	assertWorkRecordingRetireCounts(t, db)
}

func assertWorkRecordingRetirePreconditions(t *testing.T, db *gorm.DB) {
	t.Helper()
	for query, want := range map[string]int64{
		"SELECT COUNT(*) FROM sys_api WHERE path LIKE '/api/gb28181/work-recordings%' AND deleted_at IS NULL":                           7,
		"SELECT COUNT(*) FROM sys_menu WHERE permission LIKE 'gb28181:work-recording:%' AND deleted_at IS NULL":                         3,
		"SELECT COUNT(*) FROM sys_casbin_rule WHERE v1 LIKE '/api/gb28181/work-recordings%'":                                            7,
		"SELECT COUNT(*) FROM sys_menu_api WHERE menu_id IN (SELECT id FROM sys_menu WHERE permission LIKE 'gb28181:work-recording:%')": 13,
	} {
		var got int64
		require.NoError(t, db.Raw(query).Scan(&got).Error, query)
		require.EqualValues(t, want, got, query)
	}
}

func assertWorkRecordingRetireCounts(t *testing.T, db *gorm.DB) {
	t.Helper()
	for query, want := range map[string]int64{
		// 活跃记录清零
		"SELECT COUNT(*) FROM sys_api WHERE path LIKE '/api/gb28181/work-recordings%' AND deleted_at IS NULL":   0,
		"SELECT COUNT(*) FROM sys_menu WHERE permission LIKE 'gb28181:work-recording:%' AND deleted_at IS NULL": 0,
		// 关联与授权清零
		"SELECT COUNT(*) FROM sys_casbin_rule WHERE v1 LIKE '/api/gb28181/work-recordings%'":                                             0,
		"SELECT COUNT(*) FROM sys_menu_api WHERE menu_id IN (SELECT id FROM sys_menu WHERE permission LIKE 'gb28181:work-recording:%')":  0,
		"SELECT COUNT(*) FROM sys_role_menu WHERE menu_id IN (SELECT id FROM sys_menu WHERE permission LIKE 'gb28181:work-recording:%')": 0,
		// 菜单与 API 只是软删，行仍在（保留审计痕迹）
		"SELECT COUNT(*) FROM sys_api WHERE path LIKE '/api/gb28181/work-recordings%' AND deleted_at IS NOT NULL":   7,
		"SELECT COUNT(*) FROM sys_menu WHERE permission LIKE 'gb28181:work-recording:%' AND deleted_at IS NOT NULL": 3,
		// 无关授权不得被波及
		"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_2'":            1,
		"SELECT COUNT(*) FROM sys_menu_api WHERE menu_id=20 AND api_id=900": 1,
		"SELECT COUNT(*) FROM sys_role_menu WHERE role_id=2 AND menu_id=20": 1,
		// 作业单权限不受影响（种子库没有，仍应为 0，证明没有误删/误建）
		"SELECT COUNT(*) FROM sys_casbin_rule WHERE v1 LIKE '/api/gb28181/work-orders%'": 0,
	} {
		var got int64
		require.NoError(t, db.Raw(query).Scan(&got).Error, query)
		require.EqualValues(t, want, got, query)
	}
}

// 退役迁移必须排在作业单菜单迁移之后：作业单先接管，旧权限再退场。
func TestWorkRecordingRetireMigrationRunsAfterTheWorkOrderMenuMigration(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("../../../resource/database/gb28181/migrations"))
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	for _, dialect := range []Dialect{DialectMySQL, DialectPostgres, DialectSQLServer} {
		files := FilterUpFiles(names, dialect)
		menuIndex, retireIndex := -1, -1
		for i, name := range files {
			switch {
			case strings.HasPrefix(name, workOrderMenuMigration+"-") || name == workOrderMenuMigration+".sql":
				menuIndex = i
			case strings.HasPrefix(name, workRecordingRetireMigration+"-") || name == workRecordingRetireMigration+".sql":
				retireIndex = i
			}
		}
		require.GreaterOrEqual(t, menuIndex, 0, "%s 作业单菜单迁移必须被扫描到", dialect)
		require.GreaterOrEqual(t, retireIndex, 0, "%s 退役迁移必须被扫描到", dialect)
		require.Greater(t, retireIndex, menuIndex, "%s 退役迁移必须排在作业单菜单迁移之后", dialect)
	}
}

// 三个快照文件必须带上退役语句，否则"快照库"与"迁移库"结果不一致：
// 前者仍留着指向已删除端点的孤儿权限，后者已清掉。
func TestWorkRecordingRetireSnapshotsCarryTheCleanup(t *testing.T) {
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database", name))
			require.NoError(t, err)
			normalized := normalizeWorkRecordingPermissionSQL(string(body))
			require.Contains(t, normalized, "delete from sys_casbin_rule where v1 like '/api/gb28181/work-recordings%'")
			require.Contains(t, normalized, "update sys_menu set deleted_at")
			require.Contains(t, normalized, "update sys_api set deleted_at")
		})
	}
}
