package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const cascadeReconnectRetireMigration = "2026-09-11-cascade-reconnect-retire"

const cascadeReconnectAPIPath = "/api/gb28181/cascade/platforms/:id/reconnect"
const cascadeReconnectPermission = "gb28181:cascade:reconnect"

func TestCascadeReconnectRetireMigrationIsThreeDialectAndFailClosed(t *testing.T) {
	for _, suffix := range []string{"", "-postgresql", "-sqlserver"} {
		t.Run(suffix, func(t *testing.T) {
			up := normalizeWorkRecordingPermissionSQL(readWorkRecordingPermissionMigration(t, cascadeReconnectRetireMigration+suffix+".sql"))
			down := normalizeWorkRecordingPermissionSQL(readWorkRecordingPermissionMigration(t, cascadeReconnectRetireMigration+suffix+"-down.sql"))

			// 必须精确命中已删除的端点与权限，且不得误伤其它路径。
			require.Contains(t, up, cascadeReconnectAPIPath)
			require.Contains(t, up, cascadeReconnectPermission)
			// 关联表无 deleted_at，只能物理删除；菜单/API 必须软删保留审计痕迹。
			require.Contains(t, up, "delete from sys_casbin_rule")
			require.Contains(t, up, "delete from sys_menu_api")
			require.Contains(t, up, "delete from sys_role_menu")
			require.Contains(t, up, "update sys_menu set deleted_at")
			require.Contains(t, up, "update sys_api set deleted_at")
			// 退役域必须收在 reconnect 内，不能出现无条件删除。
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

func TestCascadeReconnectRetireMigrationRemovesOrphanPermissionsAndIsIdempotent(t *testing.T) {
	db := openWorkRecordingPermissionSQLite(t)
	seedWorkRecordingPermissionSchema(t, db)
	// 直接造出现网真实存在的待退役记录（等价于 2026-08-10 级联权限种子的产物；
	// 该种子含 MySQL 会话变量，sqlite 跑不了，所以用等价 INSERT 代替）。
	seedCascadeReconnectRetireFixtures(t, db)
	// 前置断言：待退役记录确实被造出来了，否则本测试会空转通过。
	assertCascadeReconnectRetirePreconditions(t, db)

	// 注意：这里必须用原始文本切分语句。归一化会把换行折叠成空格，
	// 整份文件会被当成一条语句，只有第一条 DELETE 能执行。
	up := readWorkRecordingPermissionMigration(t, cascadeReconnectRetireMigration+".sql")
	// 跑两遍证明幂等。
	for i := 0; i < 2; i++ {
		for _, statement := range splitStatements(sqliteWorkRecordingPermissionSQL(up)) {
			require.NoError(t, db.Exec(statement).Error, statement)
		}
	}
	assertCascadeReconnectRetireCounts(t, db)
}

// 与 2026-08-10 级联权限种子的产物等价：reconnect 按钮 + API + 两种绑定 + Casbin；
// 另造一条同族级联 GET 记录用于断言不误伤。
func seedCascadeReconnectRetireFixtures(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, statement := range []string{
		"INSERT INTO sys_menu(id, parent_id, title, type, permission, deleted_at) VALUES(140380, 10, '重连国标级联', 3, '" + cascadeReconnectPermission + "', NULL)",
		"INSERT INTO sys_menu(id, parent_id, title, type, permission, deleted_at) VALUES(140375, 0, '国标级联', 2, '', NULL)",
		"INSERT INTO sys_api(id, title, path, method, deleted_at) VALUES(325, '重连级联平台', '" + cascadeReconnectAPIPath + "', 'POST', NULL)",
		"INSERT INTO sys_api(id, title, path, method, deleted_at) VALUES(320, '查看级联平台列表', '/api/gb28181/cascade/platforms', 'GET', NULL)",
		"INSERT INTO sys_menu_api(menu_id, api_id) VALUES(140380, 325)",
		"INSERT INTO sys_menu_api(menu_id, api_id) VALUES(140375, 320)",
		"INSERT INTO sys_role_menu(role_id, menu_id) VALUES(1, 140380)",
		"INSERT INTO sys_casbin_rule(ptype, v0, v1, v2, v3, v4, v5) VALUES('p', 'role_1', '" + cascadeReconnectAPIPath + "', 'POST', '*', '', '')",
		"INSERT INTO sys_casbin_rule(ptype, v0, v1, v2, v3, v4, v5) VALUES('p', 'role_1', '/api/gb28181/cascade/platforms', 'GET', '*', '', '')",
	} {
		require.NoError(t, db.Exec(statement).Error, statement)
	}
}

func assertCascadeReconnectRetirePreconditions(t *testing.T, db *gorm.DB) {
	t.Helper()
	for query, want := range map[string]int64{
		"SELECT COUNT(*) FROM sys_api WHERE path='" + cascadeReconnectAPIPath + "' AND method='POST' AND deleted_at IS NULL":    1,
		"SELECT COUNT(*) FROM sys_menu WHERE permission='" + cascadeReconnectPermission + "' AND deleted_at IS NULL":            1,
		"SELECT COUNT(*) FROM sys_casbin_rule WHERE v1='" + cascadeReconnectAPIPath + "' AND v2='POST'":                          1,
		"SELECT COUNT(*) FROM sys_menu_api WHERE menu_id IN (SELECT id FROM sys_menu WHERE permission='" + cascadeReconnectPermission + "')": 1,
	} {
		var got int64
		require.NoError(t, db.Raw(query).Scan(&got).Error, query)
		require.EqualValues(t, want, got, query)
	}
}

func assertCascadeReconnectRetireCounts(t *testing.T, db *gorm.DB) {
	t.Helper()
	for query, want := range map[string]int64{
		// 活跃记录清零
		"SELECT COUNT(*) FROM sys_api WHERE path='" + cascadeReconnectAPIPath + "' AND method='POST' AND deleted_at IS NULL": 0,
		"SELECT COUNT(*) FROM sys_menu WHERE permission='" + cascadeReconnectPermission + "' AND deleted_at IS NULL":         0,
		// 关联与授权清零
		"SELECT COUNT(*) FROM sys_casbin_rule WHERE v1='" + cascadeReconnectAPIPath + "' AND v2='POST'":                                    0,
		"SELECT COUNT(*) FROM sys_menu_api WHERE menu_id IN (SELECT id FROM sys_menu WHERE permission='" + cascadeReconnectPermission + "')": 0,
		"SELECT COUNT(*) FROM sys_role_menu WHERE menu_id IN (SELECT id FROM sys_menu WHERE permission='" + cascadeReconnectPermission + "')": 0,
		// 菜单与 API 只是软删，行仍在（保留审计痕迹）
		"SELECT COUNT(*) FROM sys_api WHERE path='" + cascadeReconnectAPIPath + "' AND method='POST' AND deleted_at IS NOT NULL": 1,
		"SELECT COUNT(*) FROM sys_menu WHERE permission='" + cascadeReconnectPermission + "' AND deleted_at IS NOT NULL":         1,
		// 无关授权不得被波及
		"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_2'":            1,
		"SELECT COUNT(*) FROM sys_menu_api WHERE menu_id=20 AND api_id=900": 1,
		"SELECT COUNT(*) FROM sys_role_menu WHERE role_id=2 AND menu_id=20": 1,
		// 其余级联权限不受影响（种子库由同一历史迁移写入）
		"SELECT COUNT(*) FROM sys_api WHERE path LIKE '/api/gb28181/cascade/platforms' AND method='GET' AND deleted_at IS NULL": 1,
		"SELECT COUNT(*) FROM sys_casbin_rule WHERE v1='/api/gb28181/cascade/platforms' AND v2='GET'":                           1,
	} {
		var got int64
		require.NoError(t, db.Raw(query).Scan(&got).Error, query)
		require.EqualValues(t, want, got, query)
	}
}

// 三个快照文件必须带上退役语句，否则"快照库"与"迁移库"结果不一致：
// 前者仍留着指向已删除端点的孤儿权限，后者已清掉。
func TestCascadeReconnectRetireSnapshotsCarryTheCleanup(t *testing.T) {
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database", name))
			require.NoError(t, err)
			normalized := normalizeWorkRecordingPermissionSQL(string(body))
			require.Contains(t, normalized, "delete from sys_casbin_rule where v1='"+cascadeReconnectAPIPath+"' and v2='post'")
			require.Contains(t, normalized, "update sys_menu set deleted_at")
			require.Contains(t, normalized, "update sys_api set deleted_at")
			// 快照里的种子仍会写入 reconnect 记录，因此退役语句必须排在种子之后。
			seedIndex := strings.Index(normalized, "insert into sys_casbin_rule")
			cleanupIndex := strings.Index(normalized, "delete from sys_casbin_rule where v1='"+cascadeReconnectAPIPath+"'")
			require.GreaterOrEqual(t, seedIndex, 0)
			require.Greater(t, cleanupIndex, seedIndex)
		})
	}
}
