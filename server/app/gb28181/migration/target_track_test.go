package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// targetTrackMigrations 是本批迁移的文件名（不含方言后缀）。
const targetTrackMigration = "2026-09-21-target-track"

// ⛔ 下面两个路径写字面量，**不**从 `models.TargetTrackAPIPath` 取值：
// 本用例要证明"迁移里登记的就是这个路径"，若从被测对象自己取值来断言自己，
// 改错了路径这条断言会跟着一起错，等于没测。
const (
	targetTrackAPIPathGet  = "/api/gb28181/device-mgmt/channel/:id/target-track"
	targetTrackTable       = "gb_device_target_track"
	targetTrackAreaColumns = "area_length,area_width,area_mid_point_x,area_mid_point_y,area_length_x,area_length_y"
)

func targetTrackDialectFiles() []struct {
	name, up, down string
	quote          func(string) string
} {
	return []struct {
		name, up, down string
		quote          func(string) string
	}{
		{"mysql", "", "-down", func(c string) string { return "`" + c + "`" }},
		{"postgresql", "-postgresql", "-postgresql-down", func(c string) string { return c }},
		{"sqlserver", "-sqlserver", "-sqlserver-down", func(c string) string { return "[" + c + "]" }},
	}
}

func targetTrackMigrationsDir() string {
	return filepath.Join("../../../resource/database/gb28181", "migrations")
}

// 三方言 up 文件都必须建出落库表，且六个框选坐标列**必须可空**。
//
// ⛔ 可空不是风格问题：Auto/Stop 不带框，把"没给框"写成 0 会让界面显示一个
// 钉在左上角的假跟踪框（窗口尺寸 0 是非法值，不是合法的"零框"）。
func TestTargetTrackDDLIsAvailableForEverySupportedDatabase(t *testing.T) {
	for _, dialect := range targetTrackDialectFiles() {
		t.Run(dialect.name, func(t *testing.T) {
			upBody, err := os.ReadFile(filepath.Join(targetTrackMigrationsDir(), targetTrackMigration+dialect.up+".sql"))
			require.NoError(t, err)
			up := strings.ToLower(string(upBody))
			require.Contains(t, up, "create table", "%s 未建表", dialect.name)
			require.Contains(t, up, targetTrackTable)
			for _, column := range strings.Split(targetTrackAreaColumns, ",") {
				pos := strings.Index(up, dialect.quote(column))
				require.GreaterOrEqualf(t, pos, 0, "%s 缺少列 %s", dialect.name, column)
				line := up[pos:]
				if at := strings.Index(line, "\n"); at >= 0 {
					line = line[:at]
				}
				require.NotContainsf(t, line, "not null",
					"%s 的 %s 被写成了 NOT NULL；nil 才是「没有框」的正确表达", dialect.name, column)
			}

			downBody, err := os.ReadFile(filepath.Join(targetTrackMigrationsDir(), targetTrackMigration+dialect.down+".sql"))
			require.NoError(t, err)
			require.Containsf(t, strings.ToLower(string(downBody)), "drop table",
				"%s 的 down 未回收落库表", dialect.name)
		})
	}
}

// 建表必须显式写 COLLATE=utf8mb4_general_ci。
//
// 只写 `DEFAULT CHARSET=utf8mb4` 时 MySQL 会取**该字符集的默认排序规则**，
// 8.0 上是 utf8mb4_0900_ai_ci —— 与本库其它表不一致，第一次 JOIN 报 1267。
// 这条断言的价值在于：漏写时**建表本身完全成功**，只有跑到某个跨表 JOIN 才炸。
func TestTargetTrackMySQLEnginePinsCollation(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(targetTrackMigrationsDir(), targetTrackMigration+".sql"))
	require.NoError(t, err)
	require.Contains(t, string(body), "COLLATE=utf8mb4_general_ci")
}

// 三份**全量快照**都必须带「建表块 + 权限块」。
//
// 依据 runner 的基线语义：空版本表 + 基线探测表已存在 ⇒ **整批迁移被标记成已应用而
// 一条都不执行**。所以快照里没有的物件在快照新建出来的库上永远不会出现，增量迁移补不回来
// —— 表现是"服务起得来、用到就报 no such table / 接口恒 403"。
func TestTargetTrackFreshInstallUsesTheMigrationBlock(t *testing.T) {
	for _, dialect := range targetTrackDialectFiles() {
		snapshot := map[string]string{
			"":            "uvp-gb28181.sql",
			"-postgresql": "postgresql_converted.sql",
			"-sqlserver":  "sqlserver_converted.sql",
		}[dialect.up]
		t.Run(snapshot, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database", snapshot))
			require.NoError(t, err)
			text := string(body)

			require.Contains(t, text, "-- target-track:start", "%s 缺少建表块", snapshot)
			require.Contains(t, text, "-- target-track:end", "%s 建表块未闭合", snapshot)
			require.Contains(t, text, "-- target-track-permissions:start", "%s 缺少权限块", snapshot)
			require.Contains(t, text, "-- target-track-permissions:end", "%s 权限块未闭合", snapshot)
			require.Contains(t, text, targetTrackTable, "%s 缺少落库表", snapshot)
			require.Contains(t, text, targetTrackAPIPathGet, "%s 缺少目标跟踪接口", snapshot)

			// ⛔ 只断言表名不够：快照里"有那张表"但**少一列**同样是致命的
			// （gorm 读它报 unknown column，而增量迁移补不回来）。而且六列必须可空 ——
			// Auto/Stop 不带框，NOT NULL 会把"没有框"逼成 0。
			start := strings.Index(strings.ToLower(text), "-- target-track:start")
			require.GreaterOrEqual(t, start, 0)
			definition := strings.ToLower(text[start:])
			if end := strings.Index(definition, "-- target-track:end"); end >= 0 {
				definition = definition[:end]
			}
			for _, column := range strings.Split(targetTrackAreaColumns, ",") {
				pos := strings.Index(definition, dialect.quote(column))
				require.GreaterOrEqualf(t, pos, 0, "%s 的表定义缺少列 %s", snapshot, column)
				line := definition[pos:]
				if at := strings.Index(line, "\n"); at >= 0 {
					line = line[:at]
				}
				require.NotContainsf(t, line, "not null",
					"%s 的 %s 被写成了 NOT NULL；nil 才是「没有框」的正确表达", snapshot, column)
			}

			up, err := os.ReadFile(filepath.Join(targetTrackMigrationsDir(), targetTrackMigration+dialect.up+".sql"))
			require.NoError(t, err)
			require.Contains(t, string(up), "-- target-track-permissions:start",
				"%s 缺少权限段标记（行为测试按这个标记切分）", snapshot)
		})
	}
}

// 三方言的权限段都是纯 DML（建表是方言 DDL，由上面的文本断言覆盖），
// 所以可以直接在 sqlite 上：连跑两遍验幂等 → 查计数 → 跑 down 两遍验无残留。
//
// ⛔ 种子刻意留着「只有 ptz:view 的角色」「只有 ptz:control 的角色」「只有不相关权限的角色」
// 三类：少了它们，用例只能证明"插入成功"，不能证明**没有越权**。
//
// ⛔ 本迁移**新建 sys_menu 权限点了吗**？没有 —— 读/写分别复用 gb28181:ptz:view 与
// gb28181:ptz:control。所以 down **不许**去删那两个 sys_menu 行（它们服务着一整族云台接口），
// 下面的断言把这一点钉住：down 之后那两个权限点必须还在。
func TestTargetTrackPermissionMigrationIsScopedAndReversible(t *testing.T) {
	for _, dialect := range targetTrackDialectFiles() {
		t.Run(dialect.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			raw, err := db.DB()
			require.NoError(t, err)
			raw.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = raw.Close() })

			for _, statement := range []string{
				"CREATE TABLE sys_api(id INTEGER PRIMARY KEY,title TEXT,path TEXT,method TEXT,api_group TEXT,created_at TEXT,updated_at TEXT,created_by INTEGER,deleted_at TEXT)",
				"CREATE TABLE sys_menu(id INTEGER PRIMARY KEY,parent_id INTEGER,path TEXT,name TEXT,component TEXT,title TEXT,hide INTEGER,disable INTEGER,sort INTEGER,type INTEGER,permission TEXT,icon TEXT,created_at TEXT,updated_at TEXT,created_by INTEGER,deleted_at TEXT)",
				"CREATE TABLE sys_role_menu(role_id INTEGER,menu_id INTEGER)",
				"CREATE TABLE sys_menu_api(menu_id INTEGER,api_id INTEGER)",
				"CREATE TABLE sys_casbin_rule(ptype TEXT,v0 TEXT,v1 TEXT,v2 TEXT,v3 TEXT,v4 TEXT,v5 TEXT)",
				// role 1 同时有 ptz:view 与 ptz:control；role 2 只有 ptz:view；
				// role 3 只有 ptz:control；role 4 只有一个不相关的权限。
				// ⛔ menu 44 / 45 是**同 permission 但 type=1（目录）**的行，且**必须**
				// 在 sys_role_menu 里有归属 —— 少这一行，"级联授权不过滤 type=3"就不产生
				// 任何可观测差异，那条断言变成恒真。
				"INSERT INTO sys_menu(id,permission,type) VALUES(40,'unrelated',3),(41,'gb28181:ptz:view',3),(42,'gb28181:ptz:control',3),(43,'gb28181:other',3),(44,'gb28181:ptz:view',1),(45,'gb28181:ptz:control',1)",
				"INSERT INTO sys_role_menu(role_id,menu_id) VALUES(1,41),(1,42),(2,41),(3,42),(4,43),(2,44),(3,45)",
				"INSERT INTO sys_api(id,path,method) VALUES(90,'/api/gb28181/device-mgmt/channel/:id/video-params','GET')",
				"CREATE TABLE gb_device_target_track(id INTEGER PRIMARY KEY,target_code TEXT)",
				"INSERT INTO gb_device_target_track(id,target_code) VALUES(1,'34020000001320000001')",
			} {
				require.NoError(t, db.Exec(statement).Error)
			}

			base := filepath.Join(targetTrackMigrationsDir(), targetTrackMigration+dialect.up)
			upBytes, err := os.ReadFile(base + ".sql")
			require.NoError(t, err)
			sections := strings.Split(string(upBytes), "-- target-track-permissions:start")
			require.Len(t, sections, 2, "up 迁移缺少权限段标记")

			run := func(body string) {
				// 三方言归一到 sqlite：N'…' 前缀去掉、标识符引号去掉、CONCAT 换成 ||。
				body = strings.ReplaceAll(body, "N'", "'")
				body = strings.NewReplacer("`", "", "[", "", "]", "").Replace(body)
				body = strings.ReplaceAll(body, "CONCAT('role_',rm.role_id)", "'role_' || rm.role_id")
				for _, statement := range splitStatements(body) {
					// ⛔ 只跑纯 DML。建表/删表是**方言 DDL**：SQL Server 的
					// `IF OBJECT_ID(...) IS NULL BEGIN … END` sqlite 直接语法错。
					if isDialectDDL(statement) {
						continue
					}
					require.NoError(t, db.Exec(statement).Error, statement)
				}
			}

			run(sections[1])
			run(sections[1]) // 第二遍必须完全幂等

			for query, want := range map[string]int64{
				// 1 条种子（video-params）+ GET + POST。
				"SELECT COUNT(*) FROM sys_api": 3,
				// 读挂 ptz:view 那条按钮、写挂 ptz:control 那条按钮 ⇒ 各 1 行。
				"SELECT COUNT(*) FROM sys_menu_api":                  2,
				"SELECT COUNT(*) FROM sys_menu_api WHERE menu_id=41": 1,
				"SELECT COUNT(*) FROM sys_menu_api WHERE menu_id=42": 1,
				// ⛔ 目录行（type=1）不许被当成按钮绑上：44/45 两条都必须是 0。
				"SELECT COUNT(*) FROM sys_menu_api WHERE menu_id IN (44,45)": 0,
				// role 1 拿到 GET+POST 两条；role 2 只有 view ⇒ 只有 GET；
				// role 3 只有 control ⇒ 只有 POST；role 4（无关权限）一条都不许拿。
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_1'":               2,
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_2' AND v2='GET'":  1,
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_2' AND v2='POST'": 0,
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_3' AND v2='POST'": 1,
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_3' AND v2='GET'":  0,
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_4'":               0,
				// 本迁移**不新建**任何 sys_menu 行：6 行种子就是全部。
				"SELECT COUNT(*) FROM sys_menu": 6,
				// casbin 规则的路径必须钉在目标跟踪接口上，不是通配。
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v1='" + targetTrackAPIPathGet + "'": 4,
			} {
				var count int64
				require.NoError(t, db.Raw(query).Scan(&count).Error)
				require.Equal(t, want, count, query)
			}

			downBytes, err := os.ReadFile(base + "-down.sql")
			require.NoError(t, err)
			run(string(downBytes))
			run(string(downBytes))

			for query, want := range map[string]int64{
				"SELECT COUNT(*) FROM sys_api WHERE deleted_at IS NULL": 1,
				"SELECT COUNT(*) FROM sys_menu_api":                     0,
				"SELECT COUNT(*) FROM sys_casbin_rule":                  0,
				// ⛔ down **不许**回收复用来的权限点：它们是别的云台接口的宿主，
				// 删掉等于顺手废掉平台所有云台读写。
				"SELECT COUNT(*) FROM sys_menu WHERE permission IN ('gb28181:ptz:view','gb28181:ptz:control')": 4,
				"SELECT COUNT(*) FROM sys_role_menu": 7,
			} {
				var count int64
				require.NoError(t, db.Raw(query).Scan(&count).Error)
				require.Equal(t, want, count, query)
			}
		})
	}
}
