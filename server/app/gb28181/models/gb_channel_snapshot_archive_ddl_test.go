package models_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// snapshotArchiveMigration 是「把批量下载接口补进已有开发库」的幂等迁移文件名。
const snapshotArchiveMigration = "2026-11-04-snapshot-archive-download.sql"

// snapshotArchiveAPIPathLiteral 是批量下载接口的全路径字面量。
//
// ⛔ 刻意不直接引用 `gbmodels.SnapshotArchiveAPIPath` 来断言它自己 —— 那样改歪了常量
// 这条断言也跟着歪，等于没测。常量与字面量的一致性由
// TestSnapshotArchiveRouteConstantsMatchRegistrations 单独钉住。
const snapshotArchiveAPIPathLiteral = "/api/gb28181/device-mgmt/snapshots/archive"

// TestSnapshotArchiveRouteConstantsMatchRegistrations 常量必须与「真正注册进鉴权中间件的
// 全路径」逐字一致。
//
// ⛔ 这不是重复断言：路由注册用的是组内相对路径 + gin 组前缀拼出来的，
// 而 sys_api 登记用的是全路径常量。两者一旦不一致，表现是**接口通但恒 403**
// （或登记了却永远匹配不到），两边都不报错、日志里也看不出因果。
func TestSnapshotArchiveRouteConstantsMatchRegistrations(t *testing.T) {
	require.Equal(t, "/snapshots/archive", gbmodels.SnapshotArchiveRoutePath)
	require.Equal(t, snapshotArchiveAPIPathLiteral, gbmodels.SnapshotArchiveAPIPath)
	require.Equal(t, "/api/gb28181/device-mgmt"+gbmodels.SnapshotArchiveRoutePath,
		gbmodels.SnapshotArchiveAPIPath)
	// ⛔ 必须与读图接口**不同路径**：两者共用一条路径时，`/snapshots/:id/content`
	// 会把 `archive` 当成 id 吃掉（id 解析失败 ⇒ 恒 404），而路由注册阶段不会报错。
	require.NotEqual(t, gbmodels.SnapshotLibraryAPIPath, gbmodels.SnapshotArchiveAPIPath)
	// ⛔ 也不能挂在列表路径上：`GET /snapshots` 已被列表占用，同一个 (path, method)
	// 在 gin 里会直接 panic（重复注册），在 sys_api 里会变成一条登记管两个语义。
	require.NotEqual(t, gbmodels.SnapshotListAPIPath, gbmodels.SnapshotArchiveAPIPath)
}

// TestSnapshotArchiveIsRegisteredInEveryDialectSnapshot 三方言**全量脚本**都要有这条接口的
// 登记、挂载与授权。
//
// 依据：迁移目录已退役（embed FS 为空），空版本表 + 基线探测表存在时整批迁移被标记已应用
// ⇒ 快照里没有的物件在新建库上永远不会出现。
func TestSnapshotArchiveIsRegisteredInEveryDialectSnapshot(t *testing.T) {
	root := dualVersionDDLRoot(t)
	for _, snapshot := range []string{
		"uvp-gb28181.sql",
		"postgresql_converted.sql",
		"sqlserver_converted.sql",
	} {
		t.Run(snapshot, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(root, "resource", "database", snapshot))
			require.NoError(t, err)
			text := strings.ToLower(string(raw))
			// SQL Server 用 N'…'|写字符串、[标识符] 包裹，先归一再去断言。
			text = strings.NewReplacer("n'", "'", "[", "", "]", "").Replace(text)

			// 接口登记：⛔ path + method + api_group 三元组，少一样都能被蹭绿：
			//   - 只写 path：会被**同路径的 casbin 授权行**蹭绿（`'role_1', '<path>', 'get'`）；
			//   - sys_api 行的形状是 `(<id>, '<title>', '<path>', '<METHOD>', '<api_group>', …)`，
			//     casbin 行同一位置是 `'*'` ⇒ 三元组能唯一定位**登记**行。
			require.Containsf(t, text,
				"'"+snapshotArchiveAPIPathLiteral+"', 'get', '图像库'",
				"%s 缺少批量下载接口的 sys_api 登记（path+method+api_group）", snapshot)

			// 授权：⛔ 必须带 `'role_1', ` 前缀，只写 path+method 会被上面那条 sys_api 登记蹭绿。
			require.Containsf(t, text,
				"'role_1', '"+snapshotArchiveAPIPathLiteral+"', 'get'",
				"%s 缺少管理员角色对批量下载接口的 casbin 授权", snapshot)

			// 挂载：与读图/列表同属「设备图像抓拍会话」按钮（140450）。
			// ⛔ 挂到别的菜单会让「有图像库权限、没有某页权限」的角色下载按钮恒 403。
			require.Containsf(t, text, "(140450, 686)",
				"%s 未把批量下载接口挂到 140450（设备图像抓拍会话）", snapshot)
		})
	}
}

// TestSnapshotArchivePathAcceptsQueryToken 批量下载路径必须在 `?token=` 白名单里。
//
// ⛔ 这是 2026-11-04 真机踩出来的 401：前端下载那一步是 `<a download>`，由浏览器原生
// 发起、加不了 Authorization 头，而 `common.GetAccessToken` 的 URL 令牌白名单只收
// 流式（SSE）与取图接口 ⇒ 点「批量下载」必然 401（日志里只有一句
// "token is required in Authorization header"，看不出是白名单没收录）。
//
// ⛔ 为什么不在 common 包里断言：`queryTokenAllowlist` 是包内变量，改路径常量时
// 那边只能用字面量，两边各写一个字符串就又回到"改一处忘一处"。这里读源文件做跨文件
// 契约，改歪了会红。
func TestSnapshotArchivePathAcceptsQueryToken(t *testing.T) {
	root := dualVersionDDLRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "app", "utils", "common", "auth.go"))
	require.NoError(t, err)
	// ⛔ 判据写成 `"<path>": {}` 整条 map 条目：只匹配路径字符串的话，
	// 注释里提到这个路径也会蹭绿（而注释救不了 401）。
	require.Contains(t, string(raw), `"`+snapshotArchiveAPIPathLiteral+`": {}`,
		"批量下载接口没进 ?token= 白名单：<a download> 带不了 Authorization 头，前端点了只会 401")
}

// TestSnapshotArchiveMigrationIsIdempotentAndComplete 补开发库的迁移必须可重复执行，
// 且内容与三方言快照一致 —— 否则开发库与新建库会分叉。
//
// 迁移目录虽已退役，但**手工补开发库**这条路还在用（照 2026-10-05-record-cache-ticket-apis
// 的先例）；它不进自动化，所以只能靠本用例保证「写漏一处会被发现」。
func TestSnapshotArchiveMigrationIsIdempotentAndComplete(t *testing.T) {
	root := dualVersionDDLRoot(t)
	path := filepath.Join(root, "resource", "database", "gb28181", "migrations", snapshotArchiveMigration)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	text := strings.ToLower(string(raw))

	require.Contains(t, text, snapshotArchiveAPIPathLiteral, "迁移缺少接口路径")
	require.Contains(t, text, "'role_1'", "迁移缺少管理员授权")
	require.Contains(t, text, "(140450, 686)", "迁移缺少菜单挂载")
	// 三条 DML 都必须是幂等写法：开发库可能已经手工跑过一遍。
	require.Equal(t, 3, strings.Count(text, "where not exists"),
		"迁移里的三条 DML 都必须是幂等写法（WHERE NOT EXISTS）")
	// ⛔ 迁移不能带 down 自动执行体：删登记/删授权是 fail-closed 操作。
	_, err = os.Stat(path + "-down.sql")
	require.True(t, os.IsNotExist(err), "不该为权限回收提供可自动执行的 down 脚本")
}
