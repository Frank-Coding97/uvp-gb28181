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

// channelSnapshotMigrations 是本批迁移的文件名（不含方言后缀）。
const channelSnapshotMigration = "2026-09-20-channel-snapshot-library"

// channelSnapshotAPIPath 是图像库读图接口的全路径。
// ⛔ 与 `models.SnapshotLibraryAPIPath` 是同一个值（那边由路由常量派生）。这里写字面量是
// 故意的：本用例要证明"迁移文件里登记的就是这个路径"，若从被测对象自己取值来断言自己，
// 改错了路径这条断言会跟着一起错，等于没测。
const channelSnapshotAPIPath = "/api/gb28181/device-mgmt/snapshots/:id/content"

// channelSnapshotListAPIPath / menuPath / menuComponent 同理写死字面量：断言的是
// "迁移与快照里登记的确实是这些值"，不能从被测对象自己取值来断言自己。
//
// ⛔ menuComponent 不是随便起的名字：前端 `web/src/router/route-output.ts` 用
// `import.meta.glob("@/views/**/*.vue")` 的 key（去掉 `views/` 与 `.vue`）与菜单行的
// component 列**逐字比对**，写歪一个字符的表现是"菜单在、点开空白"，且前后端都不报错。
const (
	channelSnapshotListAPIPath   = "/api/gb28181/device-mgmt/snapshots"
	channelSnapshotMenuPath      = "/gb28181/snapshot-library"
	channelSnapshotMenuName      = "snapshot-library"
	channelSnapshotMenuComponent = "gb28181/snapshot-library/index"
)

// flatMenuSQL 把三方言的引号/标识符引用风格归一（SQL Server 的 `N'…'` 与 `[标识符]`），
// 好让"同一段菜单语句在不在"用三份文件共用的一条断言表达。
// ⛔ 归一化只做这一层：断言的目的是"这条语句在不在"，不是"方言写得对不对"。
func flatMenuSQL(text string) string {
	return strings.NewReplacer("n'", "'", "[", "", "]", "").Replace(strings.ToLower(text))
}

// TestChannelSnapshotLibraryFreshInstallUsesTheMigrationBlock 三份**全量快照**都必须带
// 「建表块 + 权限块」，且三方言 up 文件里都有同名物件。
//
// 依据是 runner 的基线语义（见 migration/runner.go）：空版本表 + 基线探测表已存在
// ⇒ **整批迁移被标记成已应用而一条都不执行**。所以快照里没有的物件在快照新建出来的
// 库上永远不会出现，增量迁移补不回来 —— 表现是"服务起得来、用到就报 no such table"。
func TestChannelSnapshotLibraryFreshInstallUsesTheMigrationBlock(t *testing.T) {
	for suffix, snapshot := range map[string]string{
		"":            "uvp-gb28181.sql",
		"-postgresql": "postgresql_converted.sql",
		"-sqlserver":  "sqlserver_converted.sql",
	} {
		t.Run(snapshot, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database", snapshot))
			require.NoError(t, err)
			text := string(body)

			require.Contains(t, text, "-- channel-snapshot-library:start", "%s 缺少建表块", snapshot)
			require.Contains(t, text, "-- channel-snapshot-library:end", "%s 建表块未闭合", snapshot)
			require.Contains(t, text, "-- channel-snapshot-library-permissions:start", "%s 缺少权限块", snapshot)
			require.Contains(t, text, "-- channel-snapshot-library-permissions:end", "%s 权限块未闭合", snapshot)
			require.Contains(t, text, "gb_channel_snapshot")
			require.Contains(t, text, channelSnapshotAPIPath)
			// 菜单行与列表接口也必须进快照：漏了它们的表现是"用快照新建的库上，
			// 图像库表在、接口通，但菜单栏里没有入口"，而且增量迁移补不回来。
			// ⛔ 菜单行用**组合片段**（parent_id=0 + path + name + component 连在一起）：
			// 授权语句里同样带着那个 path，只断言 path 的话"菜单行整条没进快照"也能通过。
			require.Containsf(t, flatMenuSQL(text),
				"select 0,'"+channelSnapshotMenuPath+"','"+channelSnapshotMenuName+"','"+channelSnapshotMenuComponent+"'",
				"%s 缺图像库一级菜单行（parent_id=0 / type=2 / component 路径对不上）", snapshot)
			require.Contains(t, text, channelSnapshotListAPIPath, "%s 缺少图像库列表接口", snapshot)

			up, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations",
				channelSnapshotMigration+suffix+".sql"))
			require.NoError(t, err)
			require.Contains(t, string(up), "gb_channel_snapshot")
			require.Contains(t, string(up), "-- channel-snapshot-library-permissions:start",
				"%s 缺少权限段标记（行为测试按这个标记切分）", snapshot)
			require.Containsf(t, flatMenuSQL(string(up)),
				"select 0,'"+channelSnapshotMenuPath+"','"+channelSnapshotMenuName+"','"+channelSnapshotMenuComponent+"'",
				"%s 迁移缺图像库一级菜单行", snapshot)
		})
	}
}

// TestChannelSnapshotLibraryPermissionMigrationIsScopedAndReversible 三方言的权限段都是纯 DML
// （建表是方言 DDL，由 models 包的文本断言覆盖），所以可以直接在 sqlite 上：
// 连跑两遍验幂等 → 查计数 → 跑 down 两遍验无残留。
//
// ⛔ 种子刻意留着「只有 ptz:view 的角色」与「只有不相关权限的角色」两行：
// 少了它们，用例只能证明"插入成功"，不能证明**没有越权** ——
// 把图像读权限顺手发给所有设备类权限角色，是这类迁移最典型也最危险的错
// （图片是监控画面，越权读等于把摄像机画面给出去）。
func TestChannelSnapshotLibraryPermissionMigrationIsScopedAndReversible(t *testing.T) {
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
				// role 1 同时有 device:snapshot 与 ptz:view；role 2 只有 ptz:view；
				// role 3 只有一个不相关的权限。用户菜单也在，用于证明"只按 permission 精确绑定"。
				// ⛔ menu 43 是**同 permission 但 type=1（目录）**的行，并且**必须**在
				// sys_role_menu 里有归属（role 2）—— 少这一行，"级联授权不过滤 type=3"
				// 就不产生任何可观测差异，那条断言变成恒真（变异自检抓出来的第 3 个假锚点）。
				"INSERT INTO sys_menu(id,permission,type) VALUES(40,'unrelated',3),(41,'gb28181:device:snapshot',3),(42,'gb28181:ptz:view',3),(43,'gb28181:device:snapshot',1)",
				"INSERT INTO sys_role_menu(role_id,menu_id) VALUES(1,41),(1,42),(2,42),(2,43),(3,40)",
				"INSERT INTO sys_api(id,path,method) VALUES(90,'/api/gb28181/device-mgmt/channel/:id/video-params','GET')",
				"CREATE TABLE gb_channel_snapshot(id INTEGER PRIMARY KEY,channel_code TEXT,file_name TEXT)",
				"INSERT INTO gb_channel_snapshot(id,channel_code,file_name) VALUES(1,'34020000001320000001','shot.jpg')",
			} {
				require.NoError(t, db.Exec(statement).Error)
			}

			path := filepath.Join("../../../resource/database/gb28181/migrations", channelSnapshotMigration+suffix)
			upBytes, err := os.ReadFile(path + ".sql")
			require.NoError(t, err)
			sections := strings.Split(string(upBytes), "-- channel-snapshot-library-permissions:start")
			require.Len(t, sections, 2, "up 迁移缺少权限段标记")

			run := func(body string) {
				// 三方言归一到 sqlite：N'…' 前缀去掉、CONCAT 换成 || 。
				body = strings.ReplaceAll(body, "N'", "'")
				body = strings.ReplaceAll(body, "CONCAT('role_',rm.role_id)", "'role_' || rm.role_id")
				body = strings.ReplaceAll(body, "CONCAT(N'role_',rm.role_id)", "'role_' || rm.role_id")
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
				// 1 条种子（video-params）+ 读图接口 + 列表接口。
				"SELECT COUNT(*) FROM sys_api": 3,
				// 读图与列表绑同一个抓拍按钮 ⇒ 2 行。
				"SELECT COUNT(*) FROM sys_menu_api": 2,
				// role 1 拿到"读图"与"列表"两条 casbin 规则。
				"SELECT COUNT(*) FROM sys_casbin_rule":                                                           2,
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_1' AND v2='GET'":                            2,
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v1='/api/gb28181/device-mgmt/snapshots/:id/content'": 1,
				// ⛔ 列表接口那条路径与读图接口**不通配**（`/snapshots` 不等于 `/snapshots/:id/content`），
				// 各占一行；写成通配会让"两条路"缩成一条，而少的是哪条在这里看得见。
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v1='/api/gb28181/device-mgmt/snapshots'": 1,
				// ⛔ 没有 device:snapshot 的角色一条都不许拿到（不越权）。
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_2'": 0,
				"SELECT COUNT(*) FROM sys_casbin_rule WHERE v0='role_3'": 0,
				// 菜单 43 是同 permission 但 type=1（目录），不许被当成按钮绑上。
				"SELECT COUNT(*) FROM sys_menu_api WHERE menu_id=43": 0,
				// 图像库一级菜单：1 行有效，且必须是"顶级菜单"（parent_id=0 / type=2）——
				// 挂错父级会让它变成某个页面下的子项，菜单栏里照样"看不到"。
				"SELECT COUNT(*) FROM sys_menu WHERE path='/gb28181/snapshot-library' AND deleted_at IS NULL":                         1,
				"SELECT COUNT(*) FROM sys_menu WHERE path='/gb28181/snapshot-library' AND type=2 AND parent_id=0":                     1,
				"SELECT COUNT(*) FROM sys_menu WHERE path='/gb28181/snapshot-library' AND component='gb28181/snapshot-library/index'": 1,
				// 菜单可见性：role 1 必须拿到（菜单树是 role_menu 驱动的，少了这行菜单谁都看不到）；
				// role 2（只有 ptz:view）与 role 3（无关权限）一条都不许有 —— 否则是**越权给入口**。
				// ⛔ role 2 同时持有 menu 43（同 permission 的目录行），所以这里还顺带证明了
				// "级联授权只按 type=3 的按钮推导"：漏掉 type=3 时 role 2 会凭空拿到图像库入口。
				"SELECT COUNT(*) FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id WHERE m.path='/gb28181/snapshot-library' AND rm.role_id=1":        1,
				"SELECT COUNT(*) FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id WHERE m.path='/gb28181/snapshot-library' AND rm.role_id IN (2,3)": 0,
				// 5 行种子（含 role 2 那行 type=1 目录归属）+ 图像库那 1 行。
				"SELECT COUNT(*) FROM sys_role_menu": 6,
			} {
				var count int64
				require.NoError(t, db.Raw(query).Scan(&count).Error)
				require.Equal(t, want, count, query)
			}

			// ⭐ 「内置管理员（role 1）恒可见」是**独立契约**，不能被"按抓拍按钮推导"那条盖住。
			// 做法：把 role 1 的按钮授权与已授的菜单行都删掉，再跑一遍 up —— 此时
			// role 1 还能拿到菜单，只可能是第一条（硬编码 role 1）在起作用。
			// ⛔ 少了它，某个把 admin 按钮授权手工改过的部署里，图像库菜单会凭空消失
			//    （而两条授权"或"在一起时，删掉任意一条都不会有任何用例报警）。
			require.NoError(t, db.Exec(
				"DELETE FROM sys_role_menu WHERE role_id=1 AND menu_id IN (SELECT id FROM sys_menu WHERE path='/gb28181/snapshot-library')").Error)
			require.NoError(t, db.Exec("DELETE FROM sys_role_menu WHERE role_id=1 AND menu_id=41").Error)
			run(sections[1])
			var adminMenuRows int64
			require.NoError(t, db.Raw(
				"SELECT COUNT(*) FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id WHERE m.path='/gb28181/snapshot-library' AND rm.role_id=1").
				Scan(&adminMenuRows).Error)
			require.Equal(t, int64(1), adminMenuRows, "内置管理员必须恒能看到图像库菜单")
			// 复原：把 role 1 的抓拍按钮授权还回去，后面的 down 计数断言才与初始 seed 一致。
			require.NoError(t, db.Exec("INSERT INTO sys_role_menu(role_id,menu_id) VALUES(1,41)").Error)

			downBytes, err := os.ReadFile(path + "-down.sql")
			require.NoError(t, err)
			// 落库表的回收集在方言 DDL 里（sqlite 执行不了 SQL Server 那份），
			// 所以按文本断言三份 down 都回收了它 —— 只断言 DML 会漏掉这个物件。
			require.Contains(t, string(downBytes), "gb_channel_snapshot", "down 未回收落库表")
			run(string(downBytes))
			run(string(downBytes))

			for query, want := range map[string]int64{
				"SELECT COUNT(*) FROM sys_api WHERE deleted_at IS NULL": 1,
				"SELECT COUNT(*) FROM sys_menu_api":                     0,
				"SELECT COUNT(*) FROM sys_casbin_rule":                  0,
				// ⛔ 这里必须带 `deleted_at IS NULL`：图像库菜单行与 sys_api 同用**软删**回收，
				// 用 `COUNT(*)` 会把软删行也算进去，于是"down 没回收干净"这条也能通过 ——
				// 而它恰恰是这条用例要防的。
				"SELECT COUNT(*) FROM sys_menu WHERE deleted_at IS NULL": 4,
				// 软删那 1 行必须真的在（down 回收了菜单，只是留痕）。
				"SELECT COUNT(*) FROM sys_menu WHERE path='/gb28181/snapshot-library' AND deleted_at IS NOT NULL": 1,
				"SELECT COUNT(*) FROM sys_role_menu": 5,
				// ⛔ 授权行（物理删）不许有孤儿：软删的菜单行下面还挂着授权，
				// 会让"已经回滚掉了的入口"继续出现在有权限的用户身上。
				"SELECT COUNT(*) FROM sys_role_menu rm JOIN sys_menu m ON m.id=rm.menu_id WHERE m.path='/gb28181/snapshot-library'": 0,
				// 该表仍在，因为上面的方言 DDL 被跳过了；它的删除由文本断言覆盖。
				"SELECT COUNT(*) FROM gb_channel_snapshot": 1,
			} {
				var count int64
				require.NoError(t, db.Raw(query).Scan(&count).Error)
				require.Equal(t, want, count, query)
			}
		})
	}
}
