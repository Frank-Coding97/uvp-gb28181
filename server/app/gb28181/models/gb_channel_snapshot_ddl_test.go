package models_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

// snapshotLibraryColumns 是 gb_channel_snapshot 的全部列。
//
// 前四列是归属（平台主键 + 国标编码 + 会话）；中间五列是文件事实（名字/路径/大小/摘要/拍摄时刻）；
// 最后一列是来源与审计。⛔ CapturedAt 与 Size 必须有：图像库的时间轴与"这张图多大"都读它们，
// 缺列时 gorm 会静默回落到零值（图表显示 1970 年），不会报错。
var snapshotLibraryColumns = []string{
	"id",
	"device_id",
	"channel_id",
	"channel_code",
	"session_id",
	"file_name",
	"rel_path",
	"size",
	"md5",
	"captured_at",
	"source",
	"created_by",
	"created_at",
	"updated_at",
	"deleted_at",
}

// TestChannelSnapshotDDLIsAvailableForEverySupportedDatabase 三方言迁移都要能建出这张表，
// 且各自的幂等守卫、去重键、权限绑定齐全。
func TestChannelSnapshotDDLIsAvailableForEverySupportedDatabase(t *testing.T) {
	root := dualVersionDDLRoot(t)
	migrations := filepath.Join(root, "resource", "database", "gb28181", "migrations")

	for _, migration := range []struct {
		name, up, down, guard, identity string
	}{
		{"mysql", "2026-09-20-channel-snapshot-library.sql", "2026-09-20-channel-snapshot-library-down.sql", "if not exists", "auto_increment"},
		{"postgresql", "2026-09-20-channel-snapshot-library-postgresql.sql", "2026-09-20-channel-snapshot-library-postgresql-down.sql", "if not exists", "bigserial"},
		{"sqlserver", "2026-09-20-channel-snapshot-library-sqlserver.sql", "2026-09-20-channel-snapshot-library-sqlserver-down.sql", "object_id", "identity"},
	} {
		t.Run(migration.name, func(t *testing.T) {
			upBody, err := os.ReadFile(filepath.Join(migrations, migration.up))
			require.NoErrorf(t, err, "迁移文件缺失: %s", migration.up)
			upLower := strings.ToLower(string(upBody))

			require.Contains(t, upLower, "gb_channel_snapshot", "%s 未建落库表", migration.up)
			for _, column := range snapshotLibraryColumns {
				require.Containsf(t, upLower, column, "%s 未涉及列 %s", migration.up, column)
			}
			// ⛔ 去重键必须是**两列组合**，单列 file_name 会误伤（同名文件可能来自不同通道）。
			require.Contains(t, upLower, "uk_channel_snapshot_file", "%s 缺少去重键", migration.up)
			// 自增主键必须由各方言自己的机制给出：gorm 之外的手写 SQL 若漏了它，
			// 插入时 id 恒为 0，第二行就撞主键 —— 且报错在写入路径，不在建表时。
			require.Containsf(t, upLower, migration.identity, "%s 主键缺少自增定义 %s", migration.up, migration.identity)
			require.Containsf(t, upLower, migration.guard, "%s 缺少幂等守卫 %s", migration.up, migration.guard)

			// 读接口按 gb28181:device:snapshot 绑定（与抓拍会话面板同一个权限码），
			// 走 sys_menu_api 精确绑定 + sys_casbin_rule 去重。
			for _, token := range []string{
				"/api/gb28181/device-mgmt/snapshots/:id/content",
				"gb28181:device:snapshot",
				"sys_menu_api",
				"sys_casbin_rule",
				"select distinct 'p'",
				"not exists",
			} {
				require.Contains(t, upLower, token, "%s 权限段落缺少 %s", migration.up, token)
			}

			// 列表接口的**精确登记行**：路径用带引号的整串断言，因为
			// `device-mgmt/snapshots` 是读接口 `/snapshots/:id/content` 的前缀，
			// 只断言前缀的话"列表接口根本没登记"也能通过。
			require.Containsf(t, upLower, "'/api/gb28181/device-mgmt/snapshots'",
				"%s 未登记图像库列表接口（路径须与路由注册的后端全路径逐字相同）", migration.up)

			// 一级菜单：菜单行 + 角色授权缺一不可。
			// ⛔ 菜单行必须用**组合片段**断言（parent_id=0 + path + name + component 连在一起），
			// 不能拆成"path 单独 Contains"：授权语句里也带着那个 path，于是
			// "菜单行整条没进迁移"这条也能通过 —— 实测变异证实过（这是本批第二次踩
			// "看起来具体、其实恒真"的锚点）。
			require.Containsf(t, flatSQL(upLower), "select 0,'/gb28181/snapshot-library','snapshot-library','gb28181/snapshot-library/index'",
				"%s 缺图像库一级菜单行（parent_id=0 / type=2 / component 路径对不上）", migration.up)
			require.Containsf(t, upLower, "sys_role_menu",
				"%s 菜单段落没给角色授菜单", migration.up)

			downBody, err := os.ReadFile(filepath.Join(migrations, migration.down))
			require.NoErrorf(t, err, "迁移文件缺失: %s", migration.down)
			downLower := strings.ToLower(string(downBody))
			require.Contains(t, downLower, "deleted_at", "%s 必须用软删除语义回收 API", migration.down)
			require.Contains(t, downLower, "sys_menu_api", migration.down)
			require.Contains(t, downLower, "sys_casbin_rule", migration.down)
			require.Contains(t, downLower, "/api/gb28181/device-mgmt/snapshots/:id/content", migration.down)
			require.Contains(t, downLower, "gb_channel_snapshot", "%s 未删落库表", migration.down)
			// 菜单与授权必须一起回收：只删菜单不删 sys_role_menu 会留下孤儿授权
			// （有权限的用户身上还挂着"已经回滚掉的入口"）。
			require.Containsf(t, downLower, "'/gb28181/snapshot-library'", "%s 未回收图像库菜单行", migration.down)
			require.Containsf(t, downLower, "sys_role_menu", "%s 未回收菜单角色授权", migration.down)
		})
	}
}

// TestChannelSnapshotPresentInEverySnapshot 三份全量快照都必须带上「本表 + 本接口权限」。
//
// 依据是 runner 的基线语义：**空版本表 + 基线探测表已存在 ⇒ 整批迁移被标记为已应用而不执行**。
// 快照里没有的物件，在用快照新建出来的库上永远不会出现（增量补不回来）——
// 表现是"服务起得来、表不存在"，运行到第一条 SELECT 才炸。所以这条不是风格问题。
//
// ⛔ 断言必须落在**本批新增的内容**上，不能落在"快照里本来就有的字符串"上：
// `gb28181:device:snapshot` 这个权限码在菜单种子里早就存在（7 处），
// 直接 `Contains(text, "gb28181:device:snapshot")` 是一条**永远为真**的假锚点
// —— 哪怕本批权限块整个没进快照，它也会绿。所以下面按 `-- …:start/end` 标记**切块**，
// 只在块内断言（块本身不存在时直接失败）。
func TestChannelSnapshotPresentInEverySnapshot(t *testing.T) {
	root := dualVersionDDLRoot(t)
	for _, file := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(file, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, "resource", "database", file))
			require.NoError(t, err)
			text := strings.ToLower(string(body))

			// 1) 建表块：块内必须有本表与几个承重列。
			table := snapshotBlock(t, text, "-- channel-snapshot-library:start", "-- channel-snapshot-library:end")
			require.Containsf(t, table, "gb_channel_snapshot",
				"%s 的建表块里没有 gb_channel_snapshot", file)
			for _, column := range []string{"rel_path", "captured_at", "channel_code", "file_name", "source"} {
				require.Containsf(t, table, column, "%s 建表块缺少列 %s", file, column)
			}

			// 2) 权限块：块内必须同时有本接口的**新路径**与绑定的权限码。
			permissions := snapshotBlock(t, text, "-- channel-snapshot-library-permissions:start",
				"-- channel-snapshot-library-permissions:end")
			require.Containsf(t, permissions, "/api/gb28181/device-mgmt/snapshots/:id/content",
				"%s 的权限块没有登记图像库读接口", file)
			require.Containsf(t, permissions, "gb28181:device:snapshot",
				"%s 的权限块没有绑定权限码", file)
			require.Containsf(t, permissions, "sys_casbin_rule",
				"%s 的权限块没有展开 casbin 规则（只登记 sys_api 还不够：接口会被鉴权中间件拒掉）", file)
			// 3) 同一块里的图像库列表接口与一级菜单。
			// ⛔ 菜单行用**组合片段**断言（parent_id=0 + path + name + component 连在一起）：
			// 授权语句里同样带着 `/gb28181/snapshot-library` 这个 path，
			// 只断言 path 的话"菜单行整条没进快照"也能通过（实测变异证实）。
			require.Containsf(t, permissions, "'/api/gb28181/device-mgmt/snapshots'",
				"%s 的权限块没有登记图像库列表接口", file)
			require.Containsf(t, flatSQL(permissions),
				"select 0,'/gb28181/snapshot-library','snapshot-library','gb28181/snapshot-library/index'",
				"%s 缺图像库一级菜单行（parent_id=0 / type=2 / component 路径对不上）", file)
			require.Containsf(t, permissions, "sys_role_menu",
				"%s 的权限块没给角色授菜单（菜单树读 sys_role_menu，缺了谁都看不到新菜单）", file)
		})
	}
}

// TestChannelSnapshotDeclaresGeneralCollationOnMySQL 建表语句必须**显式**声明排序规则。
//
// ⛔ 为什么这不是风格问题：MySQL 的规则是"给了 CHARACTER SET 但没给 COLLATE ⇒
// 取**该字符集的默认排序规则**"，而 8.0 里 utf8mb4 的默认是 `utf8mb4_0900_ai_ci` ——
// **不是**这个库的默认（`uvp_gb28181` 默认 `utf8mb4_general_ci`，仓库里 73 张表也都显式写了它）。
// 漏写时建表**不报任何错**，错误延迟到第一次 JOIN：
//
//	`ch.channel_id = s.channel_code` 两侧排序规则不同 ⇒ MySQL 1267 Illegal mix of collations
//	⇒ 图像库列表接口恒 400，而日志里只有一行 db.query_failed，看不出"少写了一个子句"。
//
// ⛔ 而 `CREATE TABLE IF NOT EXISTS` 让"改完 DDL 重跑"也修不好**已经建出来的表** ——
// 所以这条必须钉在文本上：行为测试只跑权限段（DDL 段是方言语法，在 sqlite 上被跳过），
// 覆盖不到建表收尾那一行。（2026-09-20 实际踩到，见技能 §25。）
func TestChannelSnapshotDeclaresGeneralCollationOnMySQL(t *testing.T) {
	root := dualVersionDDLRoot(t)
	const declared = "charset=utf8mb4 collate=utf8mb4_general_ci"

	// 1) 增量迁移：空库靠它建表。
	migration := filepath.Join(root, "resource", "database", "gb28181", "migrations",
		"2026-09-20-channel-snapshot-library.sql")
	body, err := os.ReadFile(migration)
	require.NoError(t, err)
	require.Contains(t, strings.ToLower(string(body)), declared,
		"迁移建表必须显式声明 COLLATE=utf8mb4_general_ci（只写 CHARSET 会落到 0900_ai_ci，JOIN 时报 1267）")

	// 2) MySQL 全量快照：快照库靠它建表 —— 快照缺了的表现是"服务起得来、第一次查询才炸"。
	snapshot := filepath.Join(root, "resource", "database", "uvp-gb28181.sql")
	body, err = os.ReadFile(snapshot)
	require.NoError(t, err)
	table := snapshotBlock(t, strings.ToLower(string(body)),
		"-- channel-snapshot-library:start", "-- channel-snapshot-library:end")
	require.Contains(t, table, declared,
		"快照建表块必须显式声明 COLLATE=utf8mb4_general_ci")
	// 反向：不带 COLLATE 的收尾写法必须一处都不剩（本块将来若再加表，也得同规格）。
	require.NotContains(t, table, "engine=innodb default charset=utf8mb4;",
		"快照里仍有未声明 COLLATE 的建表收尾")
}

// TestSnapshotLibraryPathConstantsMatchTheMigrationLiterals 把「常量源」与「迁移里写死的字面量」
// 钉成同一个值。这是本批唯一一处**跨包唯一契约**：
//
//	路由注册用常量、鉴权中间件按 path+method 查 casbin、casbin 规则由迁移里的 sys_api 生成。
//	三方分叉时**不报任何错**，表现只有一种 —— 接口通但恒 403（path 对不上），
//	或者接口压根没注册在迁移登记的那条路径上。
//
// ⛔ 为什么必须在这里断言**字面量**：路由测试里的 `routes["GET "+常量]` 是拿被测对象断言
// 被测对象 —— 常量与路由一起改歪时两侧同时变，照样绿。而迁移文本里写的是字面量，
// 所以只有把常量比到**字面量**上，才真正覆盖"路由那条路 == 迁移登记的那条路"。
// （这条是变异自检抓出来的：把 `SnapshotListRoutePath` 改成 `/snapshot`，routes 包全绿。）
func TestSnapshotLibraryPathConstantsMatchTheMigrationLiterals(t *testing.T) {
	require.Equal(t, "/api/gb28181/device-mgmt/snapshots", gbmodels.SnapshotListAPIPath,
		"列表接口全路径必须与迁移里 sys_api 登记的字面量一致")
	require.Equal(t, "/snapshots", gbmodels.SnapshotListRoutePath)
	require.Equal(t, "/api/gb28181/device-mgmt/snapshots/:id/content", gbmodels.SnapshotLibraryAPIPath,
		"读图接口全路径必须与迁移里 sys_api 登记的字面量一致")
	require.NotEqual(t, gbmodels.SnapshotLibraryAPIPath, gbmodels.SnapshotListAPIPath,
		"两条路径撞在一起了：读图接口会被列表接口顶掉")
	// 菜单四件套同理：component 是前端组件路径契约（import.meta.glob 的 key 逐字比对），
	// path/name 是菜单行主键口径，写歪的表现是"菜单在、点开空白"。
	require.Equal(t, "/gb28181/snapshot-library", gbmodels.SnapshotLibraryMenuPath)
	require.Equal(t, "snapshot-library", gbmodels.SnapshotLibraryMenuName)
	require.Equal(t, "gb28181/snapshot-library/index", gbmodels.SnapshotLibraryMenuComponent)
	require.Equal(t, "图像库", gbmodels.SnapshotLibraryMenuTitle)
}

// flatSQL 把三方言的引号风格归一，好让"同一段 SQL 片段"在三份快照上用同一条断言：
//
//	SQL Server 的 `N'…'` 前缀 → 去掉；`[标识符]` 的方括号 → 去掉。
//
// ⛔ 只归一**引号与标识符引用**这一层，不动别的：这条断言的目的是"这条菜单语句在不在"，
// 不是"方言写得对不对"（后者由各方言的 up 文件断言覆盖）。归一化过宽会让断言失去分辨力。
func flatSQL(text string) string {
	return strings.NewReplacer("n'", "'", "[", "", "]", "").Replace(text)
}

// snapshotBlock 取出 `-- <start>` 与 `-- <end>` 两个标记之间的内容（去首尾空白）。
// 标记缺失直接判失败 —— 这正是"本批内容没进快照"的判据。
func snapshotBlock(t *testing.T, text, start, end string) string {
	t.Helper()
	startIndex := strings.Index(text, start)
	require.GreaterOrEqualf(t, startIndex, 0, "快照缺少块标记 %s", start)
	rest := text[startIndex:]
	endIndex := strings.Index(rest, end)
	require.GreaterOrEqualf(t, endIndex, 0, "快照的块标记 %s 未闭合", start)
	return rest[:endIndex]
}
