package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// storageCardFormatMigration 是本批迁移的文件名（不含方言后缀）。
const storageCardFormatMigration = "2026-09-20-storage-card-format-permission"

// storageCardFormatAPIPath 是格式化接口的全路径。
// ⛔ 与 `models.StorageCardFormatAPIPath` 是同一个值（那边由路由常量派生）。这里写字面量是
// 故意的：本用例要证明"迁移文件里登记的就是这个路径"，若从被测对象自己取值来断言自己，
// 改错了路径这条断言会跟着一起错，等于没测。
const storageCardFormatAPIPath = "/api/gb28181/device-mgmt/channel/:id/storage-cards/format"

// storageCardFormatPermissionCode 是被授权的按钮权限码。
const storageCardFormatPermissionCode = "gb28181:device:format_sd"

// TestStorageCardFormatPermissionIsScopedIdempotentAndReversible 三方言的权限段都是纯 DML
// （本批没有建表），所以可以直接在 sqlite 上：连跑两遍验幂等 → 查计数
// → 注入一条"同 permission 但 type=1 的目录行"再跑一遍验 type=3 门禁 → 跑 down 两遍验无残留。
//
// ⛔ 种子刻意留着「只有 device:control 的角色」与「只有不相关权限的角色」两行：
// 少了它们，用例只能证明"插入成功"，不能证明**没有把权限发错人** ——
// 而"把存储卡格式化发给了所有能按录像的账号"正是这类迁移最危险、也最像"顺手就对了"的错
// （本权限码存在的**唯一理由**就是它不能与 device:control 同权）。
//
// ⛔ 那条同 permission 的目录行必须**后置注入**，不能放进种子：
// 种子阶段存在它会让 sys_menu 的 `NOT EXISTS (... permission=...)` 判成"已存在"
// ⇒ 整个按钮菜单都不插入，后面的断言全在测一件没发生的事（第一版就这么写错并被本用例抓到）。
func TestStorageCardFormatPermissionIsScopedIdempotentAndReversible(t *testing.T) {
	for _, suffix := range []string{"", "-postgresql", "-sqlserver"} {
		name := "mysql"
		switch suffix {
		case "-postgresql":
			name = "postgresql"
		case "-sqlserver":
			name = "sqlserver"
		}
		t.Run(name, func(t *testing.T) {
			db := openWorkRecordingPermissionSQLite(t)
			for _, statement := range []string{
				"CREATE TABLE sys_api(id INTEGER PRIMARY KEY AUTOINCREMENT,title TEXT,path TEXT,method TEXT,api_group TEXT,created_at TEXT,updated_at TEXT,created_by INTEGER,deleted_at TEXT)",
				"CREATE TABLE sys_menu(id INTEGER PRIMARY KEY AUTOINCREMENT,parent_id INTEGER,path TEXT,name TEXT,component TEXT,title TEXT,hide INTEGER,disable INTEGER,sort INTEGER,type INTEGER,permission TEXT,icon TEXT,created_at TEXT,updated_at TEXT,created_by INTEGER,deleted_at TEXT)",
				"CREATE TABLE sys_role_menu(role_id INTEGER,menu_id INTEGER)",
				"CREATE TABLE sys_menu_api(menu_id INTEGER,api_id INTEGER)",
				"CREATE TABLE sys_casbin_rule(ptype TEXT,v0 TEXT,v1 TEXT,v2 TEXT,v3 TEXT,v4 TEXT,v5 TEXT)",
				// 一条无关 API 作为"旁人"，用来断言迁移没把别人的行也动了。
				"INSERT INTO sys_api(id,title,path,method,api_group,created_at,updated_at,created_by) VALUES(90,'无关接口','/api/unrelated','GET','测试',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1)",
				// 41=device:control（另一个权限码，绝不能顺带拿到格式化），42=ptz:view。
				// ⛔ 这里**不能**放"同 permission 的新按钮"或"同 permission 的目录行"：
				// 前者让 sys_menu 的 NOT EXISTS 判成已存在（整段不执行），后者同理。
				"INSERT INTO sys_menu(id,parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) VALUES(40,0,'','unrelated','','无关',1,0,1,3,'unrelated','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1),(41,0,'','GbDeviceControl','','设备控制',1,0,1,3,'gb28181:device:control','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1),(42,0,'','GbPTZView','','云台查看',1,0,1,3,'gb28181:ptz:view','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1)",
				// role 2 有 device:control（能按录像、能改配置），role 3 只有无关权限。
				"INSERT INTO sys_role_menu(role_id,menu_id) VALUES(2,41),(3,40)",
			} {
				require.NoError(t, db.Exec(statement).Error, statement)
			}

			path := filepath.Join("../../../resource/database/gb28181/migrations", storageCardFormatMigration+suffix)
			upBytes, err := os.ReadFile(path + ".sql")
			require.NoError(t, err)

			run := func(body string) {
				// 三方言归一到 sqlite：N'…' 前缀去掉、方括号标识符还原、CONCAT 换成 || 。
				body = strings.ReplaceAll(body, "N'", "'")
				body = strings.ReplaceAll(body, "[", "")
				body = strings.ReplaceAll(body, "]", "")
				body = strings.ReplaceAll(body, "CONCAT('role_',rm.role_id)", "'role_' || rm.role_id")
				for _, statement := range splitStatements(body) {
					require.NoError(t, db.Exec(statement).Error, statement)
				}
			}

			require.Contains(t, string(upBytes), "-- storage-card-format-permissions:start",
				"up 迁移缺少权限段起点标记")
			require.Contains(t, string(upBytes), "-- storage-card-format-permissions:end",
				"up 迁移缺少权限段终点标记")

			sections := strings.Split(string(upBytes), "-- storage-card-format-permissions:start")
			require.Len(t, sections, 2, "up 迁移缺少权限段标记（行为测试按它切分）")
			permissionSection := sections[1]
			run(permissionSection)
			run(permissionSection) // 第二遍必须完全幂等

			buttonMenuID := "(SELECT id FROM sys_menu WHERE permission='" + storageCardFormatPermissionCode + "' AND type=3 AND deleted_at IS NULL)"
			for query, want := range map[string]int64{
				// 1 条种子 + 本迁移新增 1 条。
				"SELECT COUNT(*) FROM sys_api WHERE deleted_at IS NULL":                            2,
				"SELECT COUNT(*) FROM sys_menu WHERE deleted_at IS NULL":                           4,
				"SELECT COUNT(*) FROM sys_menu_api":                                                1,
				"SELECT COUNT(*) FROM sys_casbin_rule":                                             1,
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_1'":                           1,
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v2='POST'":                             1,
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v1='" + storageCardFormatAPIPath + "'": 1,
				// 内置管理员拿到按钮菜单的授权（拿到的是**按钮**，不是别的同 permission 行）。
				"SELECT COUNT(*) FROM sys_role_menu WHERE role_id=1 AND menu_id=" + buttonMenuID: 1,
				// ⛔ 只有 device:control 的角色一条都不许拿到（这是本权限码存在的理由）。
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_2'": 0,
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_3'": 0,
			} {
				var count int64
				require.NoError(t, db.Raw(query).Scan(&count).Error)
				require.Equal(t, want, count, query)
			}

			// ---- type=3 门禁：后置注入同 permission 的目录行，再跑一遍 ----
			// ⛔ 目录行用 id=99 而不是 43：43 会被 sys_menu 的自增水位占掉（种子到 42，本迁移插入即 43）。
			// ⛔ 只按 permission 关联（不看 type）的实现会在这里多出 sys_menu_api 行 ——
			// 表现是"一个隐藏导航壳被凭空绑上按钮接口与 casbin 规则"。
			for _, statement := range []string{
				"INSERT INTO sys_menu(id,parent_id,path,name,component,title,hide,disable,sort,type,permission,icon,created_at,updated_at,created_by) VALUES(99,0,'','GbStorageCardFormatDir','','格式化存储卡(目录)',1,0,1,1,'" + storageCardFormatPermissionCode + "','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1)",
				"INSERT INTO sys_role_menu(role_id,menu_id) VALUES(1,99)",
			} {
				require.NoError(t, db.Exec(statement).Error, statement)
			}
			run(permissionSection)
			for query, want := range map[string]int64{
				"SELECT COUNT(*) FROM sys_menu_api":                      1,
				"SELECT COUNT(*) FROM sys_casbin_rule":                   1,
				"SELECT COUNT(*) FROM sys_menu_api WHERE menu_id=99":     0,
				"SELECT COUNT(*) FROM sys_role_menu WHERE menu_id=99":    1,
				"SELECT COUNT(*) FROM sys_menu WHERE deleted_at IS NULL": 5,
			} {
				var count int64
				require.NoError(t, db.Raw(query).Scan(&count).Error)
				require.Equal(t, want, count, query)
			}

			downBytes, err := os.ReadFile(path + "-down.sql")
			require.NoError(t, err)
			run(string(downBytes))
			run(string(downBytes)) // down 也必须幂等

			for query, want := range map[string]int64{
				"SELECT COUNT(*) FROM sys_api WHERE deleted_at IS NULL": 1,
				"SELECT COUNT(*) FROM sys_menu_api":                     0,
				"SELECT COUNT(*) FROM sys_casbin_rule":                  0,
				// sys_menu 走软删：行还在（含后置注入的 43），只是不可见。
				"SELECT COUNT(*) FROM sys_menu":                          5,
				"SELECT COUNT(*) FROM sys_menu WHERE deleted_at IS NULL": 3,
				// 授权行被清掉，原有两条种子授权不受影响。
				"SELECT COUNT(*) FROM sys_role_menu": 2,
			} {
				var count int64
				require.NoError(t, db.Raw(query).Scan(&count).Error)
				require.Equal(t, want, count, query)
			}
		})
	}
}

// TestStorageCardFormatPermissionIncludedInFreshSchemas 三份**全量快照**都必须带这段权限。
//
// 依据是 runner 的基线语义（见 migration/runner.go）：空版本表 + 基线探测表已存在
// ⇒ **整批迁移被标记成已应用而一条都不执行**。所以快照里没有的物件在快照新建出来的
// 库上永远不会出现，增量迁移补不回来 —— 表现是"服务起得来、接口恒 403"。
func TestStorageCardFormatPermissionIncludedInFreshSchemas(t *testing.T) {
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		body, err := os.ReadFile(filepath.Join("../../../resource/database", name))
		require.NoError(t, err)
		for _, token := range []string{
			"-- storage-card-format-permissions:start",
			"-- storage-card-format-permissions:end",
			storageCardFormatPermissionCode,
			storageCardFormatAPIPath,
		} {
			require.Contains(t, string(body), token, "%s 缺少 %s", name, token)
		}
	}
}
