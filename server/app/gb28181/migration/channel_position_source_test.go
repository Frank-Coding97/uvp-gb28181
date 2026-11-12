package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// channelPositionSourceMigration 是本批迁移的文件名（不含方言后缀）。
const channelPositionSourceMigration = "2026-09-21-channel-position-source"

// 三方言的 up/down 文件后缀。跟 targetTrackDialectFiles 同形，但本批不涉及列引号差异，
// 所以只留文件名。
var channelPositionSourceDialects = []struct{ name, up, down string }{
	{"mysql", "", "-down"},
	{"postgresql", "-postgresql", "-postgresql-down"},
	{"sqlserver", "-sqlserver", "-sqlserver-down"},
}

// flatPositionSQL 把三方言的引号/标识符引用风格归一（SQL Server 的 `N'…'` 与 `[标识符]`、
// MySQL 的反引号），好让"同一段语句在不在"用三份文件共用的一条断言表达。
// ⛔ 只归一化这一层：断言的是"这条语句在不在"，不是"方言写得对不对"。
func flatPositionSQL(text string) string {
	return strings.NewReplacer("n'", "'", "[", "", "]", "", "`", "").Replace(strings.ToLower(text))
}

func readPositionSourceFile(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations", name))
	require.NoError(t, err, "迁移文件缺失: %s", name)
	return flatPositionSQL(string(body))
}

// isIdentifierChar 判字符是否属于标识符（用于识别"列名是不是一个完整 token"）。
func isIdentifierChar(c byte) bool {
	return c == '_' || c == '-' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// columnClause 从一行里取出「在建这一列」的语句中、列名**之后**的那一段（列类型 + 约束）。
//
// ⛔ 这个函数存在的理由全是踩过的坑：
//   - 不能对整行 `Contains("not null")`：SQL Server 的守卫写成 `IF COL_LENGTH(...) IS NULL
//     ALTER ...`、PG 的守卫是 `to_regclass(...) IS NOT NULL`，两者都含 "not null"
//     ⇒ 整行搜会把**守卫**当成列约束，"这列可不可空"根本没被验到，断言恒真。
//   - 不能用 `LastIndex(line, 列名)`：SQL Server 给 position_source 建了同名默认值约束
//     `df_gb_channel_position_source`，列名作为子串会再出现一次，取最后一次就跑到约束名上。
//   - 不能只搜列名不看上下文：列名还出现在存在性检查、`information_schema` 查询与
//     `COMMENT ON COLUMN` 里，那些都不是定义。
//
// 所以这里的做法是：**先定位 `ADD` 子句，再从 ADD 之后截到列名结束**；
// 截不到时回退到 MySQL 建表体内联列的形态（列名后面紧跟类型名，且前面不是标识符字符）。
func columnClause(line, column string) (string, bool) {
	if at := strings.Index(line, " add "); at >= 0 {
		rest := line[at+len(" add "):]
		rest = strings.TrimPrefix(rest, "column ")
		rest = strings.TrimPrefix(rest, "if not exists ")
		if strings.HasPrefix(rest, column) {
			tail := rest[len(column):]
			if len(tail) > 0 && !isIdentifierChar(tail[0]) {
				return tail, true
			}
		}
	}
	// MySQL 全量基线把列烘焙在 `CREATE TABLE gb_channel` 体内：逐行写成"列名 类型 …"。
	if strings.HasPrefix(strings.TrimSpace(line), "--") {
		return "", false
	}
	at := strings.Index(line, column)
	if at < 0 {
		return "", false
	}
	if before := line[:at]; before != "" && isIdentifierChar(before[len(before)-1]) {
		return "", false
	}
	tail := line[at+len(column):]
	for _, typeName := range []string{" varchar", " nvarchar", " datetime", " timestamp"} {
		if strings.HasPrefix(tail, typeName) {
			return tail, true
		}
	}
	return "", false
}

// columnClauseIn 在整篇 SQL 里找到第一处该列的定义段。
func columnClauseIn(text, column string) (string, bool) {
	for _, line := range strings.Split(text, "\n") {
		if clause, ok := columnClause(line, column); ok {
			return clause, true
		}
	}
	return "", false
}

// 三方言 up 都必须加出两列，且**可空性必须各自符合语义**：
//
//   - `position_source` NOT NULL DEFAULT ” —— 「无坐标」只能有一种写法。
//     允许 NULL 会让"空串"和"NULL"并存，前端判 `""` 就漏掉 NULL 那一半。
//   - `position_updated_at` NULL 可空 —— 「还没被任何一路写过」是真实状态，
//     用 0 时间戳表示会让界面显示 0001-01-01（本仓前端 dateTime() 专门过滤过这个值）。
func TestChannelPositionSourceDDLIsAvailableForEverySupportedDatabase(t *testing.T) {
	for _, dialect := range channelPositionSourceDialects {
		t.Run(dialect.name, func(t *testing.T) {
			up := readPositionSourceFile(t, channelPositionSourceMigration+dialect.up+".sql")

			sourceClause, ok := columnClauseIn(up, "position_source")
			require.Truef(t, ok, "%s 的 up 里没有加 position_source 的语句", dialect.name)
			require.Contains(t, sourceClause, "not null", "%s: position_source 必须 NOT NULL", dialect.name)
			require.Contains(t, sourceClause, "default ''", "%s: position_source 必须有空串默认值", dialect.name)

			updatedClause, ok := columnClauseIn(up, "position_updated_at")
			require.Truef(t, ok, "%s 的 up 里没有加 position_updated_at 的语句", dialect.name)
			require.NotContains(t, updatedClause, "not null",
				"%s: position_updated_at 必须可空 —— 「还没被写过」是真实状态，不能拿 0 时间戳冒充", dialect.name)
		})
	}
}

// 三方言 up 都必须**回填**：库里已有的非零坐标只可能是目录应答写进去的
// （在本批之前，`catalog/upsert.go` 是坐标列唯一的写入方），所以给它们标 `catalog`。
//
// ⛔ 不回填的后果：存量点位在界面上"有坐标但来源未知"，而这正是本能力要消除的状态。
func TestChannelPositionSourceMigrationBackfillsExistingCatalogRows(t *testing.T) {
	for _, dialect := range channelPositionSourceDialects {
		t.Run(dialect.name, func(t *testing.T) {
			up := readPositionSourceFile(t, channelPositionSourceMigration+dialect.up+".sql")
			require.Contains(t, up, "update gb_channel", "%s 缺少回填语句", dialect.name)
			require.Contains(t, up, "position_source = 'catalog'",
				"%s 的回填必须把存量坐标标成 catalog", dialect.name)
			// 判据必须只看"有值"的行：0 是「无坐标」，回填它会凭空造出"来源是 catalog 的无坐标"。
			require.Contains(t, up, "longitude <> 0 or latitude <> 0",
				"%s 的回填条件必须排除坐标为 0 的行", dialect.name)
		})
	}
}

// down 必须真的把两列删掉（三方言）。SQL Server 与 PostgreSQL 还要求带存在性守卫，
// 否则重跑会报"列不存在"而让回滚中途失败。
func TestChannelPositionSourceDownRemovesBothColumns(t *testing.T) {
	for _, dialect := range channelPositionSourceDialects {
		t.Run(dialect.name, func(t *testing.T) {
			down := readPositionSourceFile(t, channelPositionSourceMigration+dialect.down+".sql")
			for _, column := range []string{"position_source", "position_updated_at"} {
				require.Containsf(t, down, column, "%s 的 down 没有删除 %s", dialect.name, column)
			}
			require.Contains(t, down, "drop column", "%s 的 down 里没有 drop column", dialect.name)
			// 不允许 down 只删一列：删一半会让模型与库结构对不上（unknown column），
			// 而且这种"半回滚"在 runner 的记账里已经算成功，不会重试。
			require.Equal(t, 2, strings.Count(down, "drop column"),
				"%s 的 down 必须删掉两列", dialect.name)
			if dialect.name != "mysql" {
				// MySQL 没有 DROP COLUMN IF EXISTS，本仓既有 down 也都是裸 DROP（见
				// 2026-07-20-channel-snapshot-down.sql），所以只对另外两方言要求守卫。
				require.Contains(t, down, "if", "%s 的 down 应带存在性守卫", dialect.name)
			}
		})
	}
}

// 三份**全量快照**都必须能让新库拿到这两列。
//
// 依据是 runner 的基线语义（见 migration/runner.go）：空版本表 + 基线探测表已存在
// ⇒ **整批迁移被标记成已应用而一条都不执行**。所以快照里没有的列在快照建出来的库上
// 永远不会出现，增量迁移补不回来 —— 表现是"后端起得来、用到坐标就报 unknown column"。
//
// ⛔ 三方言的载体不同（MySQL 烘焙进 `CREATE TABLE gb_channel` 体内，PG / SQL Server 是
// 文件头的 `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` 块），所以这里断言"这列在不在
// 快照里"与"可空性对不对"，**不**断言它写在哪个位置 —— 那是各快照的编排风格，不是契约。
func TestChannelPositionSourceBaselinesCarryBothColumns(t *testing.T) {
	for _, dialect := range channelPositionSourceDialects {
		snapshot := map[string]string{
			"mysql":      "uvp-gb28181.sql",
			"postgresql": "postgresql_converted.sql",
			"sqlserver":  "sqlserver_converted.sql",
		}[dialect.name]
		t.Run(snapshot, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database", snapshot))
			require.NoError(t, err)
			text := flatPositionSQL(string(body))

			sourceClause, ok := columnClauseIn(text, "position_source")
			require.Truef(t, ok, "%s 里找不到 position_source 的定义", snapshot)
			require.Contains(t, sourceClause, "not null",
				"%s: 快照里的 position_source 必须是 NOT NULL + 空串默认", snapshot)
			require.Contains(t, sourceClause, "default ''", snapshot)

			updatedClause, ok := columnClauseIn(text, "position_updated_at")
			require.Truef(t, ok, "%s 里找不到 position_updated_at 的定义", snapshot)
			require.NotContains(t, updatedClause, "not null",
				"%s: 快照里的 position_updated_at 必须可空", snapshot)
		})
	}
}
