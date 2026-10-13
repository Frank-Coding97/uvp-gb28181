package models_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm/schema"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// recordCacheTable 是三方言快照与迁移脚本里都必须出现的表名。
const recordCacheTable = "gb_record_cache_task"

// recordCacheColumns 是 gb_record_cache_task 的全部列。
//
// 分四组：任务身份（id/task_id）、归属与操作人（数据权限过滤用）、
// 通道与录像段快照（通道可能被删，展示口径不能依赖 JOIN）、
// 媒体落点与产出文件、进度与终态。
//
// ⛔ 每个字段都必须显式出现：GORM 读缺列时**静默回落零值**，
// 表现是「列表里时间为 1970」「进度恒 0」这类假数据，不会报错。
var recordCacheColumns = []string{
	"id",
	"task_id",
	"owner_dept_id",
	"created_by_user",
	"created_by_name",
	"channel_id",
	"device_id",
	"channel_code",
	"channel_name",
	"device_name",
	"start_time",
	"end_time",
	"record_type",
	// record_key 是录像段标识：分片续播的每一片都要拿它重新发起 download 回放，
	// 缺了它的表现是「第一片拉完就再也拉不动」。
	"record_key",
	"download_speed",
	"node_id",
	"vhost",
	"app",
	"stream",
	"session_id",
	"file_id",
	"file_name",
	"file_path",
	"file_size",
	"cached_bytes",
	"estimated_bytes",
	"state",
	"last_error",
	"request_id",
	// cursor_at 是**分段续播游标**：单片会话的墙钟预算只有 25 分钟（平台**自设**，
	// 见 recordcache.SegmentWallBudget），超长录像必须「拉一段 → 落一片 → 从断点续拉」，
	// 这个字段就是断点。缺了它的表现是「长录像每次重来」或「进度永远停在第一片」，且不报错。
	"cursor_at",
	// segments 是**分片产出清单**（JSON 数组）。每一片是独立会话 ⇒ 独立 stream
	// ⇒ ZLM 上独立目录，不记下来的表现是「任务成功、下载到的只有最后一片」。
	"segments",
	"started_at",
	"finished_at",
	"expires_at",
	// favorite 是**收藏标记**：收藏的录像不参与保留期自动清理（只能手动删除）。
	// 缺了它的表现是「星标点了又弹回去、收藏过的录像照样被自动清理」，且不会报错。
	"favorite",
	"created_at",
	"updated_at",
}

// recordCacheIndexes 是必须存在的索引。过期清理按 expires_at 扫，列表按 state+created_at 翻页。
var recordCacheIndexes = []string{
	"uk_record_cache_task_id",
	"idx_record_cache_task_state_created",
	"idx_record_cache_task_channel_state",
	"idx_record_cache_task_dept_state",
	"idx_record_cache_task_expires",
}

// recordCacheBareDownloadPaths 是**故意不登记**进 sys_api / casbin 的两条免鉴权下载路由。
//
// 它们是「浏览器原生下载」的实际落点：凭据是后端种下的 HttpOnly 一次性 cookie
// （60 秒有效、单次消费、Path 精确锁到那一段），全程不经过 JWT / casbin。
// 登记进 sys_api 只会造成「登记了却不受控」的假象 —— 授权中间件根本不在那条线上。
//
// ⛔ 分片序号必须写在**路径**里（放进 query 的话，同一张凭据能被改成任意序号重放），
// 所以这里是两条不同的路径，而不是一条带参数的。
var recordCacheBareDownloadPaths = []string{
	"/api/gb28181/record-cache/downloads/:downloadId/content",
	"/api/gb28181/record-cache/downloads/:downloadId/segments/:index/content",
}

// recordCacheSnapshot 描述一份全量初始化脚本的方言特征。
type recordCacheSnapshot struct {
	dialect string
	file    string
	// quote 是该方言的标识符引用字符；断言列名时必须带上它，
	// 否则 `id` 会被 `task_id` 蹭绿，等于没断言。
	quote string
}

var recordCacheSnapshots = []recordCacheSnapshot{
	{"mysql", "uvp-gb28181.sql", "`"},
	{"postgresql", "postgresql_converted.sql", `"`},
	{"sqlserver", "sqlserver_converted.sql", "["},
}

// recordCacheTableBlock 截取某张表的 CREATE TABLE 段落。
//
// ⛔ 必须截段再断言：整份快照全文匹配会被**别的表**的同名列蹭绿
// （`created_at` 这种列名全库到处都是），把「本表缺列」判成通过。
func recordCacheTableBlock(t *testing.T, text, table string) string {
	t.Helper()
	lowered := strings.ToLower(text)
	anchors := []string{
		"create table `" + table + "`",
		"create table \"" + table + "\"",
		"create table [" + table + "]",
	}
	start := -1
	for _, anchor := range anchors {
		if idx := strings.Index(lowered, anchor); idx >= 0 {
			start = idx
			break
		}
	}
	require.GreaterOrEqual(t, start, 0, "未找到 %s 的 CREATE TABLE", table)

	// 建表段落以「行首的右括号」收尾；三方言的收尾写法不同
	// （MySQL `) ENGINE=… COLLATE=…`、PG/SQLServer `);`），所以不能按 `);` 找。
	// ⛔ 必须**把收尾整行也含进来**：MySQL 的 COLLATE 就写在这一行上，
	// 截掉了就会得到「建表段没有 COLLATE」的假红。
	closing := regexp.MustCompile(`(?m)^[ \t]*\)`).FindStringIndex(lowered[start:])
	require.NotNil(t, closing, "未找到 %s 建表段的收尾括号", table)
	end := start + closing[0]
	if lineEnd := strings.Index(lowered[end:], "\n"); lineEnd >= 0 {
		end += lineEnd
	} else {
		end = len(lowered)
	}
	return text[start:end]
}

func TestRecordCacheTaskTableExistsInEverySnapshot(t *testing.T) {
	root := dualVersionDDLRoot(t)
	for _, snapshot := range recordCacheSnapshots {
		t.Run(snapshot.dialect, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, "resource", "database", snapshot.file))
			require.NoError(t, err)

			whole := strings.ToLower(string(body))
			block := strings.ToLower(recordCacheTableBlock(t, string(body), recordCacheTable))
			closeQuote := snapshot.quote
			if closeQuote == "[" {
				closeQuote = "]"
			}
			for _, column := range recordCacheColumns {
				require.Containsf(t, block, snapshot.quote+column+closeQuote,
					"%s 的建表段缺少列 %s", snapshot.file, column)
			}
			// 索引：MySQL 写在建表段内，PG / SQL Server 是段外的 CREATE INDEX，
			// 所以索引断言打在**整份文件**上（索引名全库唯一，不会蹭绿）。
			for _, index := range recordCacheIndexes {
				require.Containsf(t, whole, strings.ToLower(index), "%s 缺少索引 %s", snapshot.file, index)
			}
		})
	}
}

// TestRecordCacheTaskDeclaresGeneralCollationOnMySQL 钉住 MySQL 建表必须显式写 COLLATE。
//
// 只写 `DEFAULT CHARSET=utf8mb4` 时 MySQL 会取该字符集**自己的**默认排序规则
// （8.0 = utf8mb4_0900_ai_ci），而库与既有 73 张表是 utf8mb4_general_ci：
// 两侧不一致 ⇒ `JOIN … ON a.col = b.col` 报 1267 ER_CANT_AGGREGATE_2COLLATIONS，
// 而这**只在列对列比较时炸** —— 建表、写入、单表按 id 读全正常，唯独列表查询 400。
// ⛔ `CREATE TABLE IF NOT EXISTS` 让「改完 DDL 重跑」修不好已建出来的表，所以只能钉文本。
func TestRecordCacheTaskDeclaresGeneralCollationOnMySQL(t *testing.T) {
	root := dualVersionDDLRoot(t)
	const declared = "charset=utf8mb4 collate=utf8mb4_general_ci"

	migration, err := os.ReadFile(filepath.Join(root, "resource", "database", "gb28181", "migrations",
		"2026-10-03-record-cache.sql"))
	require.NoError(t, err)
	require.Contains(t, strings.ToLower(string(migration)), declared,
		"迁移建表必须显式声明 COLLATE=utf8mb4_general_ci")

	snapshot, err := os.ReadFile(filepath.Join(root, "resource", "database", "uvp-gb28181.sql"))
	require.NoError(t, err)
	block := strings.ToLower(recordCacheTableBlock(t, string(snapshot), recordCacheTable))
	require.Contains(t, block, declared, "MySQL 快照建表段必须显式声明 COLLATE=utf8mb4_general_ci")

	// 反向：PG / SQL Server 没有 per-table collation，出现 COLLATE 反而是方言写歪了。
	for _, file := range []string{"postgresql_converted.sql", "sqlserver_converted.sql"} {
		body, err := os.ReadFile(filepath.Join(root, "resource", "database", file))
		require.NoError(t, err)
		other := strings.ToLower(recordCacheTableBlock(t, string(body), recordCacheTable))
		require.NotContains(t, other, "collate utf8mb4_general_ci", "%s 不应出现 MySQL 排序规则", file)
	}
}

// TestRecordCacheColumnsMatchTheModel 模型字段与快照列集合必须逐字一致。
// 方向是双向的：模型多字段 ⇒ 运行时读不到列（静默零值）；快照多列 ⇒ 模型漏了字段。
func TestRecordCacheColumnsMatchTheModel(t *testing.T) {
	parsed, err := schema.Parse(&gbmodels.GbRecordCacheTask{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)
	require.ElementsMatch(t, recordCacheColumns, parsed.DBNames)

	root := dualVersionDDLRoot(t)
	body, err := os.ReadFile(filepath.Join(root, "resource", "database", "uvp-gb28181.sql"))
	require.NoError(t, err)
	block := strings.ToLower(recordCacheTableBlock(t, string(body), recordCacheTable))
	for _, column := range parsed.DBNames {
		require.Containsf(t, block, "`"+column+"`", "快照建表段缺少模型字段 %s", column)
	}
}

// TestRecordCachePermissionSeedsMatchTheContractConstants 把「常量源」钉到迁移/快照的字面量上。
//
// 菜单四件套与权限字符串同时被三处消费（迁移种子 / 前端菜单 / 路由与 casbin），
// 任一处漂了都**不报错**：path 漂 ⇒ 菜单在但点开空白；API path 漂 ⇒ 接口恒 403。
// 所以断言必须打在**字面量**上，不能拿常量自比自。
func TestRecordCachePermissionSeedsMatchTheContractConstants(t *testing.T) {
	// 常量本身先固定下来（改动常量必须是有意的）。
	require.Equal(t, 140610, gbmodels.RecordCacheMenuID)
	require.Equal(t, "/gb28181/record-cache", gbmodels.RecordCacheMenuPath)
	require.Equal(t, "gb28181-record-cache", gbmodels.RecordCacheMenuName)
	require.Equal(t, "gb28181/record-cache/index", gbmodels.RecordCacheMenuComponent)
	require.Equal(t, "录像缓存", gbmodels.RecordCacheMenuTitle)
	require.Equal(t, "/api/gb28181/record-cache/tasks", gbmodels.RecordCacheAPIBase)
	require.Equal(t, "gb28181:device-record:download", gbmodels.DeviceRecordDownloadPermission)

	root := dualVersionDDLRoot(t)
	for _, snapshot := range recordCacheSnapshots {
		t.Run(snapshot.dialect, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, "resource", "database", snapshot.file))
			require.NoError(t, err)
			// ⛔ 先小写再归一：flatSQL 认的是小写 `n'`，直接对大写原文调用它，
			// SQL Server 的 `N'/api/…'` 不会被归一，断言就变成「只有 SQL Server 假红」。
			// ⛔ text 已小写，因此期望字面量也必须小写后再比（`:taskId` 这种驼峰会假红）。
			lower := func(s string) string { return "'" + strings.ToLower(s) + "'" }
			text := flatSQL(strings.ToLower(string(body)))

			// 菜单四件套
			require.Contains(t, text, lower(gbmodels.RecordCacheMenuPath), "菜单 path 未进快照")
			require.Contains(t, text, lower(gbmodels.RecordCacheMenuName), "菜单 name 未进快照")
			require.Contains(t, text, lower(gbmodels.RecordCacheMenuComponent), "菜单 component 未进快照")
			require.Contains(t, text, lower(gbmodels.RecordCacheMenuTitle), "菜单标题未进快照")

			// 六个按钮权限点
			for _, permission := range []string{
				gbmodels.RecordCachePermissionView,
				gbmodels.RecordCachePermissionCreate,
				gbmodels.RecordCachePermissionCancel,
				gbmodels.RecordCachePermissionDownload,
				gbmodels.RecordCachePermissionDelete,
				gbmodels.RecordCachePermissionFavorite,
			} {
				require.Containsf(t, text, lower(permission), "权限点 %s 未进快照", permission)
			}

			// API 登记 —— ⛔ 三样一起断言（path + method + api_group），少一样都能被蹭绿：
			//   - 只写 `'/gb28181/record-cache/tasks'`：会被 `'/api/gb28181/record-cache/tasks'` 蹭绿 ⇒ 必须带引号；
			//   - 只写 `'<path>'`：会被**同路径的 casbin 授权行**蹭绿
			//     （`'role_1', '<path>', '<method>'`）——「sys_api 登记整段丢了、只剩 casbin」
			//     也能通过（2026-10-05 用删除式变异实测确认，两处断言都漏）。
			//   sys_api 行的形状是 `(<id>, '<title>', '<path>', '<METHOD>', '<api_group>', …)`，
			//   api_group 紧跟在 method 后面；casbin 行同一位置是 `'*'` ⇒ 三元组能唯一定位**登记**行。
			for _, row := range []struct{ path, method string }{
				{gbmodels.RecordCacheCreateAPIPath, "POST"},
				{gbmodels.RecordCacheListAPIPath, "GET"},
				{gbmodels.RecordCacheDetailAPIPath, "GET"},
				{gbmodels.RecordCacheCancelAPIPath, "POST"},
				{gbmodels.RecordCacheDeleteAPIPath, "DELETE"},
				{gbmodels.RecordCacheContentAPIPath, "GET"},
				// 票据下载这组（2026-10-05 补登记）：签发 / 查状态 / 取消三件套。
				{gbmodels.RecordCacheDownloadTicketAPIPath, "POST"},
				{gbmodels.RecordCacheDownloadStatusAPIPath, "GET"},
				{gbmodels.RecordCacheDownloadStatusAPIPath, "DELETE"},
				// 收藏 / 取消收藏。
				{gbmodels.RecordCacheFavoriteAPIPath, "POST"},
			} {
				require.Containsf(t, text,
					lower(row.path)+", '"+strings.ToLower(row.method)+"', '录像缓存'",
					"接口登记 %s %s 未进快照", row.method, row.path)
			}

			// casbin 管理员授权：每条 path+method 都要有（含 list 与 detail 共用的基路径）。
			// ⛔ 必须带上 `'role_1', ` 前缀：只写 `'<path>', '<method>'` 会被 **sys_api 种子蹭绿**
			// —— sys_api 行就是 `(634, '下载…', '/api/…/content', 'GET', …)` 这个形状，
			// 于是「casbin 授权整段丢了」也能通过（已用删除式变异实测确认）。
			for _, grant := range []struct{ path, method string }{
				{gbmodels.RecordCacheCreateAPIPath, "POST"},
				{gbmodels.RecordCacheListAPIPath, "GET"},
				{gbmodels.RecordCacheDetailAPIPath, "GET"},
				{gbmodels.RecordCacheCancelAPIPath, "POST"},
				{gbmodels.RecordCacheDeleteAPIPath, "DELETE"},
				{gbmodels.RecordCacheContentAPIPath, "GET"},
				{gbmodels.RecordCacheDownloadTicketAPIPath, "POST"},
				{gbmodels.RecordCacheDownloadStatusAPIPath, "GET"},
				{gbmodels.RecordCacheDownloadStatusAPIPath, "DELETE"},
				{gbmodels.RecordCacheFavoriteAPIPath, "POST"},
			} {
				require.Containsf(t, text, "'role_1', "+lower(grant.path)+", '"+strings.ToLower(grant.method)+"'",
					"casbin 未授权 %s %s", grant.method, grant.path)
			}

			// 反向：免鉴权的裸引擎下载路由**不得**登记。
			// 它们的凭据是 HttpOnly cookie 里的一次性票据（60 秒、单次消费、Path 锁到那一段），
			// 不经过 JWT / casbin —— 登记了只会造成「登记了却不受控」的假象。
			// ⛔ 同仓 cloud-recordings 的两条同类路由同样未登记，可对照。
			for _, bare := range recordCacheBareDownloadPaths {
				require.NotContainsf(t, text, lower(bare),
					"免鉴权路由 %s 不该被登记（见 2026-10-05-record-cache-ticket-apis.sql 的说明）", bare)
			}
		})
	}
}

// TestRecordCacheMigrationIsIdempotentAndComplete 迁移脚本用于把新物件补进**已有开发库**，
// 因此必须可重复执行（`IF NOT EXISTS` / `WHERE NOT EXISTS`），
// 且内容与三方言快照一致 —— 否则开发库与新建库会分叉。
func TestRecordCacheMigrationIsIdempotentAndComplete(t *testing.T) {
	root := dualVersionDDLRoot(t)
	path := filepath.Join(root, "resource", "database", "gb28181", "migrations", "2026-10-03-record-cache.sql")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	text := strings.ToLower(string(raw))

	require.Contains(t, text, "create table if not exists `"+recordCacheTable+"`", "建表必须幂等")
	// 每一条 INSERT 都要带守卫，否则复跑会撞主键（菜单/权限是 fail-closed 的重要资产，
	// 半途失败留下的半截数据比不执行更糟）。
	require.Equal(t, strings.Count(text, "insert into"), strings.Count(text, "where not exists"),
		"每条 INSERT 都必须有 WHERE NOT EXISTS 守卫")

	// 常量与迁移文本里的字面量逐字对齐（路由/前端/casbin 都按这些字符串定位）。
	for _, literal := range []string{
		gbmodels.RecordCacheMenuPath,
		gbmodels.RecordCacheMenuComponent,
		gbmodels.RecordCacheMenuTitle,
		gbmodels.RecordCacheAPIBase,
		gbmodels.RecordCacheContentAPIPath,
		gbmodels.RecordCachePermissionView,
		gbmodels.RecordCachePermissionCreate,
		gbmodels.RecordCachePermissionCancel,
		gbmodels.RecordCachePermissionDownload,
		gbmodels.RecordCachePermissionDelete,
	} {
		require.Containsf(t, string(raw), "'"+literal+"'", "迁移里缺少字面量 %s", literal)
	}

	// 设备录像回放页的入口权限点（140442）必须挂上创建/进度接口，
	// 否则只在回放页有权限的用户点「缓存到服务器」直接 403。
	require.Contains(t, text, "140442", "创建/进度接口未挂到设备录像下载权限点")
}

// TestRecordCacheTicketAPIMigrationIsIdempotentAndComplete 钉住「票据下载」这组接口的增量迁移。
//
// ⛔ 为什么单独一条（而不是并进 2026-10-03 那条）：它是给**已有开发库**补登记用的，
// 必须能重复执行 —— 半途失败留下的半截授权比不执行更糟（菜单/权限是 fail-closed 的资产）。
//
// ⛔ 变更的必须正好是**鉴权**的那三条：签发票据 / 查状态 / 取消下载。
// 另外两条免鉴权（`…/downloads/:downloadId/content` 与 `…/segments/:index/content`）
// 凭的是 HttpOnly 一次性 cookie，不经过 JWT / casbin，故意不登记（同仓 cloud-recordings 亦然）。
func TestRecordCacheTicketAPIMigrationIsIdempotentAndComplete(t *testing.T) {
	root := dualVersionDDLRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "resource", "database", "gb28181", "migrations",
		"2026-10-05-record-cache-ticket-apis.sql"))
	require.NoError(t, err)
	text := strings.ToLower(string(raw))

	// 3 条 sys_api + 3 条 sys_menu_api + 3 条 sys_casbin_rule，每条都要带守卫。
	require.Equal(t, 9, strings.Count(text, "insert into"), "应恰好 3 条接口 + 3 条挂载 + 3 条授权")
	require.Equal(t, strings.Count(text, "insert into"), strings.Count(text, "where not exists"),
		"每条 INSERT 都必须有 WHERE NOT EXISTS 守卫")

	// sys_api 登记行 —— ⛔ 与快照那条同理，必须连 method 与 api_group 一起断言：
	// 只断言 `'<path>'` 会被**同一个文件里的 casbin 授权行**蹭绿
	// （`'role_1', '<path>', '<method>'` ⇒「接口登记写歪了、只有授权对」也能通过，已用变异实测确认）。
	for _, row := range []struct{ path, method string }{
		{gbmodels.RecordCacheDownloadTicketAPIPath, "POST"},
		{gbmodels.RecordCacheDownloadStatusAPIPath, "GET"},
		{gbmodels.RecordCacheDownloadStatusAPIPath, "DELETE"},
	} {
		require.Containsf(t, text, "'"+strings.ToLower(row.path)+"', '"+strings.ToLower(row.method)+"', '录像缓存'",
			"迁移里的接口登记 %s %s 未按 sys_api 行形状出现", row.method, row.path)
	}
	for _, grant := range []struct{ path, method string }{
		{gbmodels.RecordCacheDownloadTicketAPIPath, "POST"},
		{gbmodels.RecordCacheDownloadStatusAPIPath, "GET"},
		{gbmodels.RecordCacheDownloadStatusAPIPath, "DELETE"},
	} {
		require.Containsf(t, text, "'role_1', '"+strings.ToLower(grant.path)+"', '"+strings.ToLower(grant.method)+"'",
			"迁移未授权 %s %s", grant.method, grant.path)
	}
	require.Contains(t, text, "140610", "新接口未挂到录像缓存菜单")

	// 反向：免鉴权的那两条不得出现在登记语句里（注释里提到它们是允许的，
	// 所以只断言**带引号**的字面量 —— 登记一定是带引号的）。
	for _, bare := range recordCacheBareDownloadPaths {
		require.NotContainsf(t, text, "'"+strings.ToLower(bare)+"'",
			"免鉴权路由 %s 不该被登记", bare)
	}
}

// TestRecordCacheFavoriteMigrationIsIdempotentAndComplete 钉住「收藏」这条增量迁移。
//
// 三件事必须同时成立，缺任何一件都不会报错、只会静默错：
//  1. favorite 列**幂等**加上（MySQL 无 ADD COLUMN IF NOT EXISTS），否则复跑 1060 中断整批迁移；
//  2. 接口与授权登记齐（否则接口恒 403，且前端星标点了没反应）；
//  3. 收藏按钮权限点进库（否则前端拿不到 `gb28181:record-cache:favorite`，星标列整个不显示）。
func TestRecordCacheFavoriteMigrationIsIdempotentAndComplete(t *testing.T) {
	root := dualVersionDDLRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "resource", "database", "gb28181", "migrations",
		"2026-10-05-record-cache-favorite.sql"))
	require.NoError(t, err)
	text := strings.ToLower(string(raw))

	// 每条 INSERT 都要有守卫：菜单/权限是 fail-closed 资产，半途失败留下的半截数据比不执行更糟。
	// 本次一共 5 条：sys_menu / sys_role_menu / sys_api / sys_menu_api / sys_casbin_rule。
	require.Equal(t, 5, strings.Count(text, "insert into"), "应恰好 5 条登记/授权 INSERT")
	require.Equal(t, strings.Count(text, "insert into"), strings.Count(text, "where not exists"),
		"每条 INSERT 都必须有 WHERE NOT EXISTS 守卫")

	// 加列必须自己判存在性并动态执行。
	require.Contains(t, text, "information_schema.columns", "必须自己判列是否存在（MySQL 无 ADD COLUMN IF NOT EXISTS）")
	require.Contains(t, text, "prepare", "必须动态执行 DDL")
	require.Contains(t, text, "`favorite`", "迁移必须覆盖 favorite 列")
	require.Contains(t, text, "tinyint", "favorite 必须是 tinyint（与全库布尔列口径一致）")
	// 反向：不允许裸 ADD COLUMN（无存在性判断的写法复跑必炸）。
	require.NotRegexp(t, regexp.MustCompile(`(?m)^\s*alter table[^;]*add column`), text,
		"不允许裸 ADD COLUMN：MySQL 复跑会 1060 中断整批迁移")

	// 接口登记行 —— ⛔ 与快照那条同理，必须连 method 与 api_group 一起断言：
	// 只断言 `'<path>'` 会被同一文件里的 casbin 授权行蹭绿。
	require.Containsf(t, text, "'"+strings.ToLower(gbmodels.RecordCacheFavoriteAPIPath)+"', 'post', '录像缓存'",
		"迁移里的接口登记 POST %s 未按 sys_api 行形状出现", gbmodels.RecordCacheFavoriteAPIPath)
	require.Containsf(t, text, "'role_1', '"+strings.ToLower(gbmodels.RecordCacheFavoriteAPIPath)+"', 'post'",
		"迁移未授权 POST %s", gbmodels.RecordCacheFavoriteAPIPath)
	require.Contains(t, text, "140610", "新接口未挂到录像缓存菜单")
	// 收藏按钮权限点必须进库（前端按它决定星标列显不显示）。
	require.Contains(t, text, "140615", "收藏按钮菜单未登记")
	require.Containsf(t, text, "'"+strings.ToLower(gbmodels.RecordCachePermissionFavorite)+"'",
		"权限点 %s 未进迁移", gbmodels.RecordCachePermissionFavorite)

	// 三方言快照都要有 favorite 列。
	for _, snapshot := range recordCacheSnapshots {
		t.Run(snapshot.dialect, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, "resource", "database", snapshot.file))
			require.NoError(t, err)
			block := strings.ToLower(recordCacheTableBlock(t, string(body), recordCacheTable))
			closeQuote := snapshot.quote
			if closeQuote == "[" {
				closeQuote = "]"
			}
			require.Containsf(t, block, snapshot.quote+"favorite"+closeQuote,
				"%s 建表段缺少 favorite", snapshot.file)
		})
	}
}

// TestRecordCacheSessionIDIsTextNotInteger 钉住 session_id 的**类型**是字符型。
//
// ⛔ 只断言列名存在是不够的：回放会话 ID 形如 `pb-a1b2c3`，写进 `bigint unsigned`
// 会被静默落成 0（GORM 对数字列遇到非数字字符串不会报错），
// 表现是「取消任务找不到会话，通道被占满 30 分钟才自动释放」——
// 一个纯类型错误伪装成超时问题。
func TestRecordCacheSessionIDIsTextNotInteger(t *testing.T) {
	root := dualVersionDDLRoot(t)

	// 模型侧：SessionID 必须是指针字符型（可空 ⇒ 尚未建会话）。
	parsed, err := schema.Parse(&gbmodels.GbRecordCacheTask{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)
	field := parsed.LookUpField("SessionID")
	require.NotNil(t, field, "模型缺少 SessionID 字段")
	require.Equal(t, "*string", field.FieldType.String(), "SessionID 必须是指针字符型（pb-xxxx 存不进整型，整型又会丢可空语义）")

	// 迁移侧：必须有一条把 bigint 改成 varchar 的幂等迁移。
	raw, err := os.ReadFile(filepath.Join(root, "resource", "database", "gb28181", "migrations",
		"2026-10-03-record-cache-segment-state.sql"))
	require.NoError(t, err)
	text := strings.ToLower(string(raw))
	require.Contains(t, text, "information_schema.columns", "必须自己判类型（MySQL 无 MODIFY COLUMN IF EXISTS）")
	require.Contains(t, text, "bigint", "判据必须是「当前是 bigint 才改」")
	require.Contains(t, text, "varchar(64)", "新的类型必须是 varchar(64)")
	require.NotRegexp(t, regexp.MustCompile(`(?m)^\s*alter table[^;]*modify column`), text,
		"不允许裸 MODIFY COLUMN：MySQL 复跑会反复重建列")

	// 三方言快照侧：列本身要存在（类型由 IR 生成，此处只保证没漏）。
	for _, snapshot := range recordCacheSnapshots {
		t.Run(snapshot.dialect, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, "resource", "database", snapshot.file))
			require.NoError(t, err)
			block := strings.ToLower(recordCacheTableBlock(t, string(body), recordCacheTable))
			require.Contains(t, block, "session_id", "%s 建表段缺少 session_id", snapshot.file)
		})
	}
}

// TestRecordCacheSegmentStateMigrationAddsColumnsIdempotently 钉住「分段续播状态」这条增量迁移
// （cursor_at 断点 + segments 分片清单 + session_id 类型修正）。
//
// ⛔ MySQL 没有 `ALTER TABLE … ADD COLUMN IF NOT EXISTS`（那是 MariaDB 扩展），
// 所以这条迁移的正确性完全靠**自己判存在性**：一旦写成裸 `ADD COLUMN`，
// 第二次执行就报 `1060 Duplicate column name`，把整批迁移中断在半路。
// 这里同时钉住「存在性判断」与「动态执行」两个要件，并对三方言快照做列断言。
func TestRecordCacheSegmentStateMigrationAddsColumnsIdempotently(t *testing.T) {
	root := dualVersionDDLRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "resource", "database", "gb28181", "migrations",
		"2026-10-03-record-cache-segment-state.sql"))
	require.NoError(t, err)
	text := strings.ToLower(string(raw))

	require.Contains(t, text, "information_schema.columns", "必须自己判列是否存在（MySQL 无 ADD COLUMN IF NOT EXISTS）")
	require.Contains(t, text, "prepare", "必须动态执行 DDL")
	require.Contains(t, text, "`cursor_at`", "迁移必须覆盖 cursor_at 列")
	require.Contains(t, text, "`segments`", "迁移必须覆盖 segments 列（否则前几片的文件路径找不回来）")
	// 反向：不允许出现裸 ADD COLUMN / 裸 MODIFY COLUMN（无存在性判断的写法复跑必炸）。
	require.NotRegexp(t, regexp.MustCompile(`(?m)^\s*alter table[^;]*add column`), text,
		"不允许裸 ADD COLUMN：MySQL 复跑会 1060 中断整批迁移")
	require.NotRegexp(t, regexp.MustCompile(`(?m)^\s*alter table[^;]*modify column`), text,
		"不允许裸 MODIFY COLUMN：MySQL 复跑会反复重建列")

	// 三方言快照都要有这两列。
	for _, snapshot := range recordCacheSnapshots {
		t.Run(snapshot.dialect, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, "resource", "database", snapshot.file))
			require.NoError(t, err)
			block := strings.ToLower(recordCacheTableBlock(t, string(body), recordCacheTable))
			closeQuote := snapshot.quote
			if closeQuote == "[" {
				closeQuote = "]"
			}
			for _, column := range []string{"cursor_at", "segments", "record_key"} {
				require.Containsf(t, block, snapshot.quote+column+closeQuote,
					"%s 建表段缺少 %s", snapshot.file, column)
			}
		})
	}
}
