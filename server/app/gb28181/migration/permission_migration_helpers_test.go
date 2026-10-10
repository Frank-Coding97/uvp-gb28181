package migration

// 权限类迁移的**测试辅助**（helper，不含测试用例本身）。
//
// 为什么要单独一个文件：`cascade_reconnect_retire_test.go` 复用了这批 helper，
// 而它们的定义原本在 `work_recording_permissions_test.go` 里 —— 那个文件只存在于
// `custom/ren` 与 `release/1.1.0-win10-trial-*` 分支，因为它同时依赖
// `2026-09-09-zzz-work-recording-permissions*.sql` 那批迁移，而那批迁移**不在 develop 上**。
// 移植 `6b91e337`（级联重连退役）时只带了用例、没带 helper，于是 develop 上
// 这个包**直接编译不过**：`undefined: normalizeWorkRecordingPermissionSQL`，
// 整个 `app/gb28181/migration` 包的测试都跑不了。
//
// 处置：只把 helper 摘出来放这里，**不带** work-recording 的用例 ——
// 这样包能编译、`cascade_reconnect_retire_*` 三个用例能跑，
// 而将来若真把那批迁移移植过来，直接删掉本文件即可（源分支有同名 helper）。
//
// ⚠️ 文件名**故意不叫** `work_recording_permissions_test.go`：那个名字留给源分支的文件，
// 避免移植时撞名（同名文件 + 同名函数 = 重复声明）。

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

// openWorkRecordingPermissionSQLite 开一个内存 sqlite 用于真跑迁移语句。
// `SetMaxOpenConns(1)` 是必须的：内存库在连接关闭时销毁，多连接会各自看到不同的库。
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

// seedWorkRecordingPermissionSchema 建出权限迁移要动的五张表 + 若干**无关记录**。
// 无关记录（menu 20 / api 900 / role_2）用来断言"退役不得误伤旁人"。
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

// readWorkRecordingPermissionMigration 从交付资源目录读一个迁移文件。
// ⚠️ 路径是相对本包目录的三级上跳（`server/app/gb28181/migration` → `server/`），
// 所以 `go test` 必须在本包目录内跑（用包路径跑即可，Go 会把工作目录设成包目录）。
func readWorkRecordingPermissionMigration(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations", name))
	require.NoError(t, err)
	return string(body)
}

// normalizeWorkRecordingPermissionSQL 折叠大小写/空白/方言引号，让"文本包含"断言
// 不受排版与方言写法影响。注意它会把换行折成空格 —— 所以**不能**用它切分语句
// （切分要用原始文本，见 `splitStatements`）。
func normalizeWorkRecordingPermissionSQL(sql string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.NewReplacer("`", "", "[", "", "]", "").Replace(sql))), " ")
}

// sqliteWorkRecordingPermissionSQL 把 MySQL/SQLServer 方言的写法改写成 sqlite 能跑的等价物：
// 引号、`N'...'` 前缀、`NOW()`、以及 `CONCAT(...)`。
func sqliteWorkRecordingPermissionSQL(sql string) string {
	sql = strings.ReplaceAll(sql, "`", "")
	sql = strings.ReplaceAll(sql, "[", "")
	sql = strings.ReplaceAll(sql, "]", "")
	sql = strings.ReplaceAll(sql, "N'", "'")
	sql = strings.ReplaceAll(sql, "NOW()", "CURRENT_TIMESTAMP")
	sql = strings.ReplaceAll(sql, "CONCAT('role_',rm.role_id)", "('role_' || rm.role_id)")
	return sql
}
