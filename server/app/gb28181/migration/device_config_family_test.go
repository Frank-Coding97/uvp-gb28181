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

// TestDeviceConfigFamilyFreshInstallUsesTheMigrationBlock 新装库的快照必须含
// **同语义**的建表与权限块 —— 见 models/gb_device_config_ddl_test.go 里那段
// runner 基线语义的说明（空版本表 ⇒ 全部迁移被标记已应用而不执行）。
//
// ⛔ 这里断言的是「同一批物件」而不是「逐字节相同」：快照里的建表语句按各方言惯例
// 加了反引号 / NOW() / ON CONFLICT，与迁移文件不逐字一致，语义一致即可。
func TestDeviceConfigFamilyFreshInstallUsesTheMigrationBlock(t *testing.T) {
	for suffix, snapshot := range map[string]string{
		"":            "uvp-gb28181.sql",
		"-postgresql": "postgresql_converted.sql",
		"-sqlserver":  "sqlserver_converted.sql",
	} {
		t.Run(snapshot, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database", snapshot))
			require.NoError(t, err)
			text := string(body)

			require.Contains(t, text, "-- device-config-family:start", "%s 缺少建表块", snapshot)
			require.Contains(t, text, "-- device-config-family:end", "%s 建表块未闭合", snapshot)
			require.Contains(t, text, "-- device-config-family-permissions:start", "%s 缺少权限块", snapshot)
			require.Contains(t, text, "-- device-config-family-permissions:end", "%s 权限块未闭合", snapshot)
			require.Contains(t, text, "gb_device_config")
			require.Contains(t, text, deviceConfigFamilyAPIPath)

			// 另三个方言的 up 文件也必须存在同名块，否则「快照与迁移同源」这句话就没有依据。
			up, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations",
				"2026-09-19-device-config-family"+suffix+".sql"))
			require.NoError(t, err)
			require.Contains(t, string(up), "gb_device_config")
		})
	}
}

const deviceConfigFamilyAPIPath = "/api/gb28181/device-mgmt/channel/:id/device-configs"

// isDialectDDL 报告一条语句是否是**方言相关的结构语句**，sqlite 跑不了或跑了会
// 改变本用例的前提（把待断言的表删掉）。
//
// ⛔ 这里只做前缀判断，不做语义解析 —— 它唯一的用途是把 DDL 从 sqlite 执行序列里摘掉，
// 判断错的最坏后果是某条 DML 没被跑到（用例变弱），不会把错的实现判成对的。
func isDialectDDL(statement string) bool {
	lowered := strings.ToLower(strings.TrimSpace(statement))
	for _, prefix := range []string{
		"create table", "create index", "create unique index",
		"drop table", "alter table", "if object_id", "if not exists",
	} {
		if strings.HasPrefix(lowered, prefix) {
			return true
		}
	}
	return false
}

// TestDeviceConfigFamilyPermissionMigrationIsScopedAndReversible 三方言的权限段
// 都是纯 DML，可以直接在 sqlite 上连跑两遍验幂等，再跑 down 验无残留。
//
// ⛔ 覆盖面刻意留着「不相关的菜单」和「只有读权限的角色」两行种子：少了它们，
// 这段测试只能证明"插入成功"，不能证明**没有越权**（把 control 权限也发给只有
// view 的角色，是这类迁移最典型的错）。
func TestDeviceConfigFamilyPermissionMigrationIsScopedAndReversible(t *testing.T) {
	for _, suffix := range []string{"", "-postgresql", "-sqlserver"} {
		name := "mysql"
		switch suffix {
		case "-postgresql":
			name = "postgresql"
		case "-sqlserver":
			name = "sqlserver"
		}
		t.Run(name, func(t *testing.T) {
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
				// role 1 同时有 view 与 control；role 2 只有 view；role 3 只有一个不相关的权限。
				"INSERT INTO sys_menu(id,permission,type) VALUES(40,'unrelated',3),(41,'gb28181:ptz:view',3),(42,'gb28181:ptz:control',3)",
				"INSERT INTO sys_role_menu(role_id,menu_id) VALUES(1,41),(1,42),(2,41),(3,40)",
				"INSERT INTO sys_api(id,path,method) VALUES(90,'/api/gb28181/device-mgmt/channel/:id/video-params','GET')",
				"CREATE TABLE gb_device_config(id INTEGER PRIMARY KEY,device_id INTEGER,target_code TEXT,config_type TEXT)",
				"INSERT INTO gb_device_config(id,device_id,target_code,config_type) VALUES(1,1,'34020000001320000010','OSDConfig')",
			} {
				require.NoError(t, db.Exec(statement).Error)
			}

			path := filepath.Join("../../../resource/database/gb28181/migrations", "2026-09-19-device-config-family"+suffix)
			upBytes, err := os.ReadFile(path + ".sql")
			require.NoError(t, err)
			sections := strings.Split(string(upBytes), "-- device-config-family-permissions:start")
			require.Len(t, sections, 2, "up 迁移缺少权限段标记")

			run := func(body string) {
				// 三方言归一到 sqlite：N'…' 前缀去掉、CONCAT 换成 || 。
				body = strings.ReplaceAll(body, "N'", "'")
				body = strings.ReplaceAll(body, "CONCAT('role_',rm.role_id)", "'role_' || rm.role_id")
				for _, statement := range splitStatements(body) {
					// ⛔ 只跑纯 DML。建表/删表是**方言 DDL**：SQL Server 的
					// `IF OBJECT_ID(...) IS NULL BEGIN … END` sqlite 直接语法错。
					// 各表的建/删由 models 包的 DDL 测试按方言文本断言存在性，
					// 本用例只证明「权限三件事的 DML 幂等且可逆」。
					if isDialectDDL(statement) {
						continue
					}
					require.NoError(t, db.Exec(statement).Error, statement)
				}
			}

			run(sections[1])
			run(sections[1]) // 第二遍必须完全幂等

			for query, want := range map[string]int64{
				// 1 条种子（video-params 那条）+ 本迁移新增 2 条。
				"SELECT COUNT(*) FROM sys_api":                                         3,
				"SELECT COUNT(*) FROM sys_menu_api":                                    2,
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_1'":               2,
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_2'":               1,
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_2' AND v2='POST'": 0,
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_3'":               0,
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v1 LIKE '%/video-params'":  0,
			} {
				var count int64
				require.NoError(t, db.Raw(query).Scan(&count).Error)
				require.Equal(t, want, count, query)
			}

			downBytes, err := os.ReadFile(path + "-down.sql")
			require.NoError(t, err)
			// 落库表的回收集在方言 DDL 里（sqlite 执行不了 SQL Server 那份），
			// 所以按文本断言三份 down 都回收了它 —— 只断言 DML 会漏掉这个物件。
			require.Contains(t, string(downBytes), "gb_device_config", "down 未回收落库表")
			run(string(downBytes))
			run(string(downBytes))

			for query, want := range map[string]int64{
				"SELECT COUNT(*) FROM sys_api WHERE deleted_at IS NULL": 1,
				"SELECT COUNT(*) FROM sys_menu_api":                     0,
				"SELECT COUNT(*) FROM sys_casbin_rule":                  0,
				"SELECT COUNT(*) FROM sys_menu":                         3,
				"SELECT COUNT(*) FROM sys_role_menu":                    4,
				// 该表仍在，因为上面的方言 DDL 被跳过了；它的删除由文本断言覆盖。
				"SELECT COUNT(*) FROM gb_device_config": 1,
			} {
				var count int64
				require.NoError(t, db.Raw(query).Scan(&count).Error)
				require.Equal(t, want, count, query)
			}
		})
	}
}
