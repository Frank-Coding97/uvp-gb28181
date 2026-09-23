package database_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 发布基线产物的方言脚本。它们必须结构一致 —— 这是历史事故的直接防线：
// PostgreSQL 一份文件曾长期缺少 20 张核心业务表而无人察觉，
// 因为当时的断言只抽查了 4 张表，且「表没找到」时会退化成全文匹配、静默通过。
//
// 交付集是 MySQL / PostgreSQL / SQL Server 三份（2026-09-22 恢复 SQL Server：
// 2026-09-15 归档分支摘除过它，生成器本轮补回了 SQLServerProfile）。
// ⚠️ SQL Server 仍**没有实机**：这里的三方言一致性断言是它唯一的质量保障，
// 别把「契约测试全绿」读成「SQL Server 导入验收过」。
var releaseFiles = []string{
	"uvp-gb28181.sql",
	"postgresql_converted.sql",
	"sqlserver_converted.sql",
}

// 建表段落契约对这三个方言都成立：MySQL 行首 CREATE TABLE、PostgreSQL 行首
// CREATE TABLE、SQL Server 缩进在 IF OBJECT_ID ... BEGIN 内的 CREATE TABLE，
// 正则都带 `^\s*`，三者的建表段落都能逐表截出来。
// （SQLite 产物的建表段落形态不同 —— 带 CHECK 子句 + 独立 CREATE INDEX ——
// develop 线不出 SQLite 产物，将来要加时得单独覆盖。）
var declarationFiles = releaseFiles

// 表名可能被反引号、双引号、方括号包裹，也可能是裸标识符。
var createTablePattern = regexp.MustCompile(
	"(?im)^\\s*CREATE TABLE\\s+(?:IF NOT EXISTS\\s+)?[`\"\\[]?([A-Za-z0-9_]+)")

// DROP 的形态三方言不同：MySQL/PostgreSQL 是行首 `DROP TABLE IF EXISTS \`x\`;`，
// SQL Server 把 DROP 写在 `IF OBJECT_ID(N'x', N'U') IS NOT NULL DROP TABLE [x];` 里
// —— 所以**不能**用 `^\s*` 锚行首，否则 SQL Server 那一份会被整段漏掉。
var dropTablePattern = regexp.MustCompile(
	"(?im)DROP TABLE\\s+(?:IF EXISTS\\s+)?[`\"\\[]?([A-Za-z0-9_]+)")

// PG 的每一个标识符都必须带双引号。本库里有 sys_jobs.group 这种保留字做列名，
// 一旦退化成裸标识符，CREATE TABLE 会 `syntax error at or near "group"`，
// 连带索引一起失败 —— 表现成「少一张表 + 少几个索引」。
var pgCreateTableQuoted = regexp.MustCompile(
	"(?im)^\\s*CREATE TABLE\\s+(?:IF NOT EXISTS\\s+)?\"")

var pgUnquotedIdentifier = regexp.MustCompile(
	"(?im)^\\s*CREATE TABLE\\s+(?:IF NOT EXISTS\\s+)?[^\"\\s]")

// 基线收录策略。口径真源，见 policy.json。
type baselinePolicy struct {
	NotBooleanColumns []string `json:"not_boolean_columns"`
	DropColumns       map[string]struct {
		Columns []string `json:"columns"`
	} `json:"drop_columns"`
}

func readPolicy(t *testing.T) baselinePolicy {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("baseline", "policy.json"))
	require.NoError(t, err, "基线策略文件缺失")
	var policy baselinePolicy
	require.NoError(t, json.Unmarshal(body, &policy))
	require.NotEmpty(t, policy.NotBooleanColumns)
	require.NotEmpty(t, policy.DropColumns)
	return policy
}

// ⛔ 本函数返回的是**小写化**后的全文，函数名把这件事显式写出来。
// 曾经它叫 readForTest 并在内部静默 ToLower，于是「产物的实际大小写」在断言里
// 看不见：tableSection 会因此把建表段落截成 1 个字符，NotContains/NotRegexp 类
// 断言对空段落恒真 —— 静默假绿。断言里比对 SQL 关键字时，务必按小写形态写。
func readForTestLowered(t *testing.T, file string) string {
	t.Helper()
	body, err := os.ReadFile(file)
	require.NoError(t, err)
	return strings.ToLower(string(body))
}

// TestBaselineOmitsColumnsDroppedByPolicy 锁定「开发库沉淀的遗留列不进新装基线」。
// 开发库是存量库，会保留模型已不再声明的列；照抄进基线等于把历史包袱发给新客户。
// 收录口径写在 policy.json 的 drop_columns，每条都必须有代码契约依据。
func TestBaselineOmitsColumnsDroppedByPolicy(t *testing.T) {
	policy := readPolicy(t)
	for table, spec := range policy.DropColumns {
		require.NotEmptyf(t, spec.Columns, "drop_columns 的 %s 没写 columns", table)
		for _, file := range releaseFiles {
			text := readForTestLowered(t, file)
			section, found := tableSection(text, table)
			require.Truef(t, found, "%s 未找到 %s 的建表段落", file, table)
			for _, column := range spec.Columns {
				require.NotContainsf(t, section, column,
					"%s 的 %s 仍声明了遗留列 %s；"+
						"基线必须由 capture_schema.py 重新捕获后再生成", file, table, column)
			}
		}
	}
}

// TestReleaseFilesDeclareSameColumnsPerTable 把跨方言一致性从「表级」下沉到「列级」。
//
// 历史事故的本体就是列级缺失：PostgreSQL 文件少了 gb_channel 等 20 张表的列，
// 而当时的断言只抽查了 4 张表的表名是否出现，于是长期无人察觉。
// SQL Server 目前没有可用的实例做实机导入，这条静态守卫就是它的最低保障。
func TestReleaseFilesDeclareSameColumnsPerTable(t *testing.T) {
	ir := readIR(t)
	want := ir.columnSet()
	require.NotEmpty(t, want)

	for _, file := range releaseFiles {
		t.Run(file, func(t *testing.T) {
			got := columnsIn(t, file)
			missing, extra := diffKeys(want, got)
			require.Emptyf(t, missing, "%s 缺少这些表: %v", file, missing)
			require.Emptyf(t, extra, "%s 多出这些表: %v", file, extra)

			for _, table := range sortedKeys(want) {
				require.Equalf(t, want[table], got[table],
					"%s 的 %s 列与中间表示不一致", file, table)
			}
		})
	}
}

func sortedKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func diffKeys(want, got map[string][]string) (missing, extra []string) {
	for name := range want {
		if _, ok := got[name]; !ok {
			missing = append(missing, name)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}

// 标识符可能是反引号、双引号、方括号包裹，也可能是裸标识符。
// 注意 [x] 的收尾字符与开头不同，不能靠回引用匹配。
func identAt(s string) (string, int) {
	t := strings.TrimLeft(s, " \t")
	switch {
	case strings.HasPrefix(t, "`"):
		if end := strings.Index(t[1:], "`"); end >= 0 {
			return t[1 : 1+end], len(s) - len(t) + end + 2
		}
	case strings.HasPrefix(t, `"`):
		if end := strings.Index(t[1:], `"`); end >= 0 {
			return t[1 : 1+end], len(s) - len(t) + end + 2
		}
	case strings.HasPrefix(t, "["):
		if end := strings.Index(t[1:], "]"); end >= 0 {
			return t[1 : 1+end], len(s) - len(t) + end + 2
		}
	default:
		end := 0
		for end < len(t) && (isWordByte(t[end])) {
			end++
		}
		if end > 0 {
			return t[:end], len(s) - len(t) + end
		}
	}
	return "", 0
}

func isWordByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// 建表语句体里以这些关键字开头的行不是列定义。
var tableBodyKeywords = map[string]bool{
	"primary": true, "unique": true, "key": true, "constraint": true,
	"check": true, "index": true, "foreign": true, "if": true,
	"engine": true, "with": true,
}

func columnsIn(t *testing.T, file string) map[string][]string {
	t.Helper()
	text := string(mustRead(t, file))
	out := map[string][]string{}

	for _, loc := range createTablePattern.FindAllStringSubmatchIndex(text, -1) {
		name := strings.ToLower(text[loc[2]:loc[3]])
		open := strings.IndexByte(text[loc[1]:], '(')
		if open < 0 {
			continue
		}
		start := loc[1] + open
		end, ok := matchingParen(text, start)
		require.Truef(t, ok, "%s 的 %s 建表语句括号不闭合", file, name)

		var columns []string
		for _, line := range strings.Split(text[start+1:end], "\n") {
			line = strings.TrimRight(strings.TrimSpace(line), ",")
			if line == "" {
				continue
			}
			ident, _ := identAt(line)
			if ident == "" || tableBodyKeywords[strings.ToLower(ident)] {
				continue
			}
			columns = append(columns, ident)
		}
		out[name] = columns
	}
	require.NotEmptyf(t, out, "%s 里没有解析出任何列定义", file)
	return out
}

func mustRead(t *testing.T, file string) []byte {
	t.Helper()
	body, err := os.ReadFile(file)
	require.NoError(t, err)
	return body
}

func matchingParen(text string, open int) (int, bool) {
	depth := 0
	for i := open; i < len(text); i++ {
		switch text[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

// TestEnumColumnsAreNotDeclaredAsBoolean 是本次修掉的真实缺陷的守卫：
// sys_users.status / sys_dict.status / sys_menu.hide 这类列在开发库里恰好只有 0/1，
// 被取值域判成布尔；但 Go 模型是 int8，语义是小枚举（1=正常 / 2=停用）。
// 建成 BOOLEAN 之后，应用一写 status=2 就报类型错（SQL Server 的 BIT 只能存 0/1，
// 连 2 都存不进去）。口径见 policy.json 的 not_boolean_columns。
func TestEnumColumnsAreNotDeclaredAsBoolean(t *testing.T) {
	policy := readPolicy(t)
	pg := readForTestLowered(t, "postgresql_converted.sql")
	mysql := readForTestLowered(t, "uvp-gb28181.sql")
	sqlserver := readForTestLowered(t, "sqlserver_converted.sql")

	for _, qualified := range policy.NotBooleanColumns {
		table, column, ok := strings.Cut(qualified, ".")
		require.Truef(t, ok, "not_boolean_columns 的 %q 应为 表.列 形式", qualified)

		pgSection, found := tableSection(pg, table)
		require.Truef(t, found, "postgresql_converted.sql 未找到 %s", table)
		require.Regexpf(t, regexp.MustCompile(`"?`+regexp.QuoteMeta(column)+`"?\s+smallint`),
			pgSection, "%s.%s 在 PostgreSQL 里必须是 SMALLINT", table, column)
		require.NotRegexpf(t, regexp.MustCompile(`"?`+regexp.QuoteMeta(column)+`"?\s+boolean`),
			pgSection, "%s.%s 不能是 BOOLEAN，它承载 1/2 这类小枚举", table, column)

		mysqlSection, found := tableSection(mysql, table)
		require.Truef(t, found, "uvp-gb28181.sql 未找到 %s", table)
		require.Containsf(t, mysqlSection, "`"+column+"` tinyint(1)",
			"%s.%s 在 MySQL 里应保持 tinyint(1)", table, column)

		// SQL Server 侧同理：这些列在 MySQL 是 tinyint(1)，但语义是小枚举，
		// 一旦映射成 BIT 会连 2 都写不进去。
		sqlserverSection, found := tableSection(sqlserver, table)
		require.Truef(t, found, "sqlserver_converted.sql 未找到 %s", table)
		require.Regexpf(t, regexp.MustCompile(`\[`+regexp.QuoteMeta(column)+`\]\s+(tinyint|smallint)`),
			sqlserverSection, "%s.%s 在 SQL Server 里必须是 TINYINT/SMALLINT", table, column)
		require.NotRegexpf(t, regexp.MustCompile(`\[`+regexp.QuoteMeta(column)+`\]\s+bit`),
			sqlserverSection, "%s.%s 不能是 BIT，它承载 1/2 这类小枚举", table, column)
	}
}

// JSON 列的**原生校验**必须在三方言都保住：MySQL 建成 json、PostgreSQL 建成 JSONB，
// 两者都在写入时拒绝非法 JSON。SQL Server 没有 JSON 类型（只能 NVARCHAR(MAX)）、
// SQLite 只有 TEXT —— 这两边都必须靠 CHECK 把同等约束补回来，否则就是跨方言语义退化：
// 同一份非法数据在 MySQL/PG 报错、在 SQL Server/SQLite 静默入库。
//
// 这条守卫钉的是一类真实缺口：生成器最初把 json 显式排除在「长度 CHECK」之外
// （json 没有长度语义，排除是对的），却没补 json 分支。实机导入全绿也照样发现不了
// —— 导入的是合法种子，永远撞不上这条 CHECK。
func TestJSONColumnsKeepNativeValidationAcrossDialects(t *testing.T) {
	ir := readIR(t)

	type ref struct{ table, column string }
	var jsonColumns []ref
	for _, table := range ir.Tables {
		for _, column := range table.Columns {
			if baseType(column.Type) == "json" {
				jsonColumns = append(jsonColumns, ref{table.Name, column.Name})
			}
		}
	}
	require.NotEmptyf(t, jsonColumns,
		"schema.ir.json 里没有 json 列 —— 断言失去对象，请先确认 IR 是最新的")

	mysql := readForTestLowered(t, "uvp-gb28181.sql")
	pg := readForTestLowered(t, "postgresql_converted.sql")
	sqlserver := readForTestLowered(t, "sqlserver_converted.sql")

	for _, jc := range jsonColumns {
		// 生成器输出小写类型名，SQL 大小写不敏感 —— 统一降格比较，别把断言的
		// 生死绑在 "JSONB" / "jsonb" 这种书写形态上。
		column := strings.ToLower(jc.column)

		mysqlSection, found := tableSection(mysql, jc.table)
		require.Truef(t, found, "uvp-gb28181.sql 未找到 %s", jc.table)
		require.Containsf(t, strings.ToLower(mysqlSection), "`"+column+"` json",
			"%s.%s 在 MySQL 里必须是 json 类型", jc.table, jc.column)

		pgSection, found := tableSection(pg, jc.table)
		require.Truef(t, found, "postgresql_converted.sql 未找到 %s", jc.table)
		require.Containsf(t, strings.ToLower(pgSection), `"`+column+`" jsonb`,
			"%s.%s 在 PostgreSQL 里必须是 JSONB", jc.table, jc.column)

		sqlserverSection, found := tableSection(sqlserver, jc.table)
		require.Truef(t, found, "sqlserver_converted.sql 未找到 %s", jc.table)
		require.Containsf(t, strings.ToLower(sqlserverSection), "isjson(["+column+"])",
			"SQL Server 的 %s.%s 是 JSON 列，必须有 ISJSON CHECK，否则非法 JSON 会静默入库",
			jc.table, jc.column)
	}
}

// IR 里的列类型是 MySQL 的类型文本，形如 "varchar(20)"、"bigint unsigned"、"json"。
// 取基础名要去掉括号参数与 unsigned 后缀。
func baseType(raw string) string {
	base := strings.ToLower(strings.TrimSpace(raw))
	if i := strings.IndexByte(base, '('); i >= 0 {
		base = base[:i]
	}
	base = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(base), "unsigned"))
	return base
}

// TestPostgreSQLIdentifiersAreQuoted 保证 PG 产物不退化成裸标识符。
// 一旦退化，sys_jobs.group 会让整条 CREATE TABLE 语法报错，并连带它后面的索引，
// 而报错信息只指向第一个撞上的保留字，很难反推到「生成器不再加引号」。
func TestPostgreSQLIdentifiersAreQuoted(t *testing.T) {
	pg := readForTestLowered(t, "postgresql_converted.sql")
	total := len(regexp.MustCompile(`(?im)^\s*CREATE TABLE\s+(?:IF NOT EXISTS\s+)?`).FindAllString(pg, -1))
	quoted := len(pgCreateTableQuoted.FindAllString(pg, -1))
	require.Equal(t, total, quoted, "PostgreSQL 的 CREATE TABLE 必须一律给表名加双引号")
	require.NotEmpty(t, total)
	require.NotRegexpf(t, pgUnquotedIdentifier, pg,
		"PostgreSQL 产物里出现了裸标识符的建表语句")

	// sys_jobs.group 是本次踩到的保留字，单独钉一条。
	jobs, found := tableSection(pg, "sys_jobs")
	require.True(t, found, "postgresql_converted.sql 未找到 sys_jobs")
	require.Containsf(t, jobs, `"group"`, "sys_jobs.group 是 PostgreSQL 保留字，必须加引号")
}

// PG 水位语句的真实形态。生成器走 pg_get_serial_sequence 而不是照
// `<表>_<列>_seq` 约定名硬写 —— 标识符超 63 字节会被 PG 截断，约定名就不成立了。
var pgSetvalPattern = regexp.MustCompile(
	`(?im)SELECT\s+setval\(\s*pg_get_serial_sequence\('([A-Za-z0-9_]+)',\s*'([A-Za-z0-9_]+)'\)`)

// PostgreSQL 的 SERIAL / BIGSERIAL 序列不会因为「带显式 id 的 INSERT」而前进。
// 基线种子一律写显式 id，因此每张「有种子行 + 单列自增主键」的表都必须在脚本里
// 补一条 setval，把水位抬到 max(id)。漏了的话，全新装环境运行起来第一次
// 无 id 插入就会 `duplicate key value violates unique constraint`。
//
// 这条守卫钉的是一个真实缺口：旧的手工基线靠人在文件尾部追加 setval（27 处，
// 含重复），那是纯人工知识、任何地方都校验不到；换成生成器后一度掉成 0 处，
// 三方言导入全绿也照样发现不了（导入用显式 id，永远撞不上）。
func TestPostgreSQLSeedTablesGetSequenceWatermarks(t *testing.T) {
	ir := readIR(t)
	pg := readForTestLowered(t, "postgresql_converted.sql")

	want := map[string][]string{}
	for _, table := range ir.Tables {
		if !table.Seeded || len(table.PrimaryKey) != 1 {
			continue
		}
		column := table.PrimaryKey[0].Name
		for _, c := range table.Columns {
			if c.Name == column && strings.Contains(strings.ToLower(c.Extra), "auto_increment") {
				want[strings.ToLower(table.Name)] = []string{column}
			}
		}
	}
	require.NotEmpty(t, want,
		"中间表示里没有「有种子行 + 自增主键」的表，这条断言会退化成空转")

	got := map[string][]string{}
	for _, match := range pgSetvalPattern.FindAllStringSubmatch(pg, -1) {
		table := strings.ToLower(match[1])
		require.NotContainsf(t, got, table, "%s 出现了重复的 setval", table)
		got[table] = []string{match[2]}
	}

	missing, extra := diffKeys(want, got)
	require.Emptyf(t, missing, "这些表有种子行却没有 setval 水位: %v", missing)
	require.Emptyf(t, extra, "这些表没有种子行却写了 setval: %v", extra)
	for _, table := range sortedKeys(want) {
		require.Equalf(t, want[table], got[table], "%s 的 setval 列名不对", table)
	}
}

// 生成物的唯一真源：capture_schema.py 从开发库抽取的中间表示。
type schemaIR struct {
	Meta struct {
		IncludedTableCount int    `json:"included_table_count"`
		SchemaFingerprint  string `json:"schema_fingerprint"`
	} `json:"meta"`
	ExcludedTables map[string]string `json:"excluded_tables"`
	Tables         []struct {
		Name    string `json:"name"`
		Seeded  bool   `json:"seeded"`
		Columns []struct {
			Name      string `json:"name"`
			Collation string `json:"collation"`
			// IR 里的 type 是 MySQL 的类型文本（"varchar(20)" / "bigint unsigned" / "json"）。
			Type  string `json:"type"`
			Extra string `json:"extra"`
		} `json:"columns"`
		PrimaryKey []struct {
			Name string `json:"name"`
		} `json:"primary_key"`
	} `json:"tables"`
}

func (ir schemaIR) columnSet() map[string][]string {
	out := map[string][]string{}
	for _, table := range ir.Tables {
		names := make([]string, 0, len(table.Columns))
		for _, column := range table.Columns {
			names = append(names, column.Name)
		}
		out[strings.ToLower(table.Name)] = names
	}
	return out
}

func readIR(t *testing.T) schemaIR {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("baseline", "schema.ir.json"))
	require.NoError(t, err, "基线中间表示缺失，无法校验生成物")
	var ir schemaIR
	require.NoError(t, json.Unmarshal(body, &ir))
	require.NotEmpty(t, ir.Tables)
	require.NotEmpty(t, ir.Meta.SchemaFingerprint)
	return ir
}

func tablesIn(t *testing.T, file string) map[string]bool {
	t.Helper()
	body, err := os.ReadFile(file)
	require.NoError(t, err)
	out := map[string]bool{}
	for _, match := range createTablePattern.FindAllStringSubmatch(string(body), -1) {
		out[strings.ToLower(match[1])] = true
	}
	require.NotEmptyf(t, out, "%s 里没有解析出任何 CREATE TABLE", file)
	return out
}

func irTableSet(ir schemaIR) map[string]bool {
	out := map[string]bool{}
	for _, table := range ir.Tables {
		out[strings.ToLower(table.Name)] = true
	}
	return out
}

func diff(want, got map[string]bool) (missing, extra []string) {
	for name := range want {
		if !got[name] {
			missing = append(missing, name)
		}
	}
	for name := range got {
		if !want[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}

// TestReleaseFilesMatchIntermediateRepresentation 保证四份方言脚本的表清单
// 与中间表示完全一致。任何手工改动、生成器回归、漏跑生成器都会在这里变红。
func TestReleaseFilesMatchIntermediateRepresentation(t *testing.T) {
	ir := readIR(t)
	want := irTableSet(ir)

	for _, file := range releaseFiles {
		t.Run(file, func(t *testing.T) {
			got := tablesIn(t, file)
			missing, extra := diff(want, got)
			require.Emptyf(t, missing, "%s 缺少这些表: %v", file, missing)
			require.Emptyf(t, extra, "%s 多出这些表: %v", file, extra)
		})
	}
}

// TestReleaseFilesAgreeAcrossDialects 直接比较四份文件，不依赖中间表示 ——
// 即使中间表示本身被改坏，这条也能发现方言之间的不一致。
func TestReleaseFilesAgreeAcrossDialects(t *testing.T) {
	base := tablesIn(t, releaseFiles[0])
	for _, file := range releaseFiles[1:] {
		got := tablesIn(t, file)
		missing, extra := diff(base, got)
		require.Emptyf(t, missing, "%s 相对 %s 缺少: %v", file, releaseFiles[0], missing)
		require.Emptyf(t, extra, "%s 相对 %s 多出: %v", file, releaseFiles[0], extra)
	}
}

// TestReleaseFilesDropOnlyTablesTheyRecreate 是一条数据安全断言：
// 基线是**建库脚本**，但它会先 DROP 再 CREATE。如果哪个方言文件里残留了一条
// 指向「本文件并不重建」的表的 DROP，那份脚本在一台已有数据的库上跑就会直接
// 删掉业务表。基线脚本必须自洽：DROP 集合 ⊆ CREATE 集合。
func TestReleaseFilesDropOnlyTablesTheyRecreate(t *testing.T) {
	for _, file := range releaseFiles {
		t.Run(file, func(t *testing.T) {
			text := string(mustRead(t, file))
			created := tablesIn(t, file)
			var stray []string
			for _, match := range dropTablePattern.FindAllStringSubmatch(text, -1) {
				name := strings.ToLower(match[1])
				if created[name] {
					continue
				}
				stray = append(stray, name)
			}
			sort.Strings(stray)
			require.Emptyf(t, stray,
				"%s 里出现了「不重建却要删」的表: %v —— 跑在存量库上会直接删掉业务表", file, stray)
		})
	}
}

// TestLiveMigrationDirectoryRetainsUpgradeHistory 锁定迁移目录的升级契约：
// 历史增量迁移继续保留供存量库升级/回滚，合并点标记也必须存在且为空操作。
//
// 为什么这条必须存在：
//  1. `//go:embed migrations/*.sql` 是**非递归**的，并且 glob 匹配不到任何文件时
//     `go build` 直接失败 —— 合并点标记必须始终存在。
//  2. 合并点标记是**运行时依赖不是文档**：执行器的
//     `if len(applied) == 0 && len(names) > 0` 是「快照库（基线化）」与
//     「老基线库（必须拒绝）」的区分点。names 为空 → 整段跳过 → 静默假成功。
//
// 「迁移建过的表是否都进了基线」这条保证现在由
// `capture_schema.py --check`（对活库比对 IR）与
// TestReleaseFilesMatchIntermediateRepresentation 共同承担 —— 前者比扫描迁移文件更强，
// 因为迁移文件的并集本来就只是开发库的一个真子集。
func TestLiveMigrationDirectoryRetainsUpgradeHistory(t *testing.T) {
	dir := filepath.Join("gb28181", "migrations")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var sqlFiles, markerFiles []string
	for _, entry := range entries {
		require.Falsef(t, entry.IsDir(), "%s 下不应有子目录", dir)
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		sqlFiles = append(sqlFiles, name)
		if strings.Contains(strings.ToLower(name), "merge-point") {
			markerFiles = append(markerFiles, name)
		}
	}
	require.NotEmpty(t, sqlFiles, "%s 下至少要有迁移或合并点标记", dir)
	require.Len(t, markerFiles, 1, "合并点标记必须唯一")
	require.Contains(t, sqlFiles, "2026-09-21-openapi-capability-catalog.sql")
	require.Contains(t, sqlFiles, "2026-09-21-openapi-capability-catalog-postgresql.sql")
	require.Contains(t, sqlFiles, "2026-09-21-openapi-capability-catalog-sqlserver.sql")

	// 标记必须是「拆语句后 0 条」的安全空操作：它只承担基线语义，不执行任何 DDL。
	body := string(mustRead(t, filepath.Join(dir, markerFiles[0])))
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		require.Failf(t, "合并点标记必须是纯注释",
			"%s 含可执行语句: %q —— 它在每次全新装库时都会被执行，不能带 DDL", markerFiles[0], trimmed)
	}
}

// TestBaselineExcludesPolicyTables 反向钉住 policy.json 的 excluded_tables：
// 被排除的表一张都不能出现在交付脚本里。没有这条的话，excluded_tables 只是
// 一段没人读的声明 —— 排除项写错、或生成器漏读，都不会有人发现。
func TestBaselineExcludesPolicyTables(t *testing.T) {
	ir := readIR(t)
	require.NotEmptyf(t, ir.ExcludedTables,
		"excluded_tables 为空 —— 断言失去对象；开发库里确实存在演示表与运维备份表")

	for _, file := range releaseFiles {
		got := tablesIn(t, file)
		for table := range ir.ExcludedTables {
			require.Falsef(t, got[strings.ToLower(table)],
				"%s 里出现了被 policy.json 排除的表 %s", file, table)
		}
	}
}

func TestMySQLReleaseInitializationContract(t *testing.T) {
	body, err := os.ReadFile("uvp-gb28181.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(body))

	require.Contains(t, sql, "mysql 8.0")
	require.Contains(t, sql, "create table `sys_civil_code`")
	require.Contains(t, sql, "create table `gb_cascade_platform`")
	require.Contains(t, sql, "create table `gb_recording_file`")
	require.Contains(t, sql, "create table `gb_sip_trace_message`")

	// 基线不得携带演示内容与现场环境数据。
	// 注意：这里不能用裸的 'demo' —— 插件示例菜单（path=/demo，name=Demo）是
	// develop 的真实功能，小写化之后会与之相撞。断言必须对准真正的演示数据。
	for _, forbidden := range []string{
		"`demo_students`",
		"`demo_teacher`",
		"create table `example`",
		"'演示账号'",
		"/public/uploads/",
		"18800000006",
		"13800000001",
		"headquarters@company.com",
		"'测试001'",
		"'测试002'",
	} {
		require.NotContains(t, sql, forbidden)
	}

	for _, seeded := range []string{
		"sys_api",
		"sys_casbin_rule",
		"sys_civil_code",
		"sys_dict",
		"sys_dict_item",
		"sys_menu",
		"sys_menu_api",
		"sys_role",
		"sys_role_menu",
		"sys_user_role",
		"sys_users",
	} {
		require.Regexp(t, regexp.MustCompile(`(?m)^insert into `+"`"+seeded+"`"), sql, seeded)
	}

	for _, environmentData := range []string{
		"gb_device",
		"gb_channel",
		"gb_sip_config",
		"gb_cascade_platform",
		"meta_node",
		"sys_operation_logs",
	} {
		require.NotRegexp(t, regexp.MustCompile(`(?m)^insert into `+"`"+environmentData+"`"), sql, environmentData)
	}
}

// TestSeedDataExcludesEnvironmentRows 锁定 policy.json 的清洗规则确实生效：
// 基线里不该出现演示账号，也不该有指向不存在角色的悬空权限规则。
func TestSeedDataExcludesEnvironmentRows(t *testing.T) {
	body, err := os.ReadFile("uvp-gb28181.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(body))

	// 注意同样不能用裸的 'demo'：插件示例菜单的 name 就是 Demo。
	for _, forbidden := range []string{
		"'演示账号'", "'role_10'", "'user_4'", "'role_2'", "'role_4'",
	} {
		require.NotContainsf(t, sql, forbidden,
			"基线种子数据里出现了环境数据 %s；请补 policy.json 的 seed_filters", forbidden)
	}

	// 反向守卫：OpenAPI 客户端（菜单 /gb28181/openapi-client + 12 条接口）**已经并入
	// develop**，属于产品功能，必须留在基线里。
	// 归档分支（2026-09-15）曾按「未并入 develop 的在途功能」把它整块排除，
	// 那条口径现在已失效 —— 这条断言防止有人照抄旧 policy 又把它排掉。
	require.Contains(t, sql, "'/gb28181/openapi-client'",
		"OpenAPI 客户端菜单已并入 develop，必须留在基线里（归档版 policy 曾排除它，口径已失效）")
	require.Contains(t, sql, "'/api/gb28181/openapi-clients'",
		"OpenAPI 客户端接口已并入 develop，必须留在基线里")
}

// TestSQLServerIdentitySeedBlocksAreBalanced 守住 SQL Server 侧最容易静默出错的一步：
// IDENTITY 列拒绝显式值，所以每张「有种子行 + 单列自增主键」的表都必须把
// INSERT 包在 SET IDENTITY_INSERT [t] ON; ... SET IDENTITY_INSERT [t] OFF; 之间。
//
// 少一个 ON → 整批插入报错；少一个 OFF → 会话里 IDENTITY_INSERT 一直开着，
// 后面别的表的显式插入会被"意外放行"，属于典型的静默损坏。
// 这条口径在 MySQL/PostgreSQL 上根本不存在（那边显式 id 是常态），
// 所以必须单独钉 —— 三方言导入验收也帮不上忙（SQL Server 没有实机）。
func TestSQLServerIdentitySeedBlocksAreBalanced(t *testing.T) {
	ir := readIR(t)
	text := string(mustRead(t, "sqlserver_converted.sql"))

	turnOn := regexp.MustCompile(`(?im)^SET IDENTITY_INSERT \[([A-Za-z0-9_]+)\] ON;`)
	turnOff := regexp.MustCompile(`(?im)^SET IDENTITY_INSERT \[([A-Za-z0-9_]+)\] OFF;`)

	onCount := map[string]int{}
	for _, match := range turnOn.FindAllStringSubmatch(text, -1) {
		onCount[strings.ToLower(match[1])]++
	}
	offCount := map[string]int{}
	for _, match := range turnOff.FindAllStringSubmatch(text, -1) {
		offCount[strings.ToLower(match[1])]++
	}

	// 期望集合：有种子行 + 单列自增主键 = IDENTITY 列需要显式插入的表。
	want := map[string]bool{}
	for _, table := range ir.Tables {
		if !table.Seeded || len(table.PrimaryKey) != 1 {
			continue
		}
		column := table.PrimaryKey[0].Name
		for _, c := range table.Columns {
			if c.Name == column && strings.Contains(strings.ToLower(c.Extra), "auto_increment") {
				want[strings.ToLower(table.Name)] = true
			}
		}
	}
	require.NotEmptyf(t, want, "IR 里没有「有种子行 + 自增主键」的表，这条断言会退化成空转")

	for table := range want {
		require.Equalf(t, 1, onCount[table], "%s 有种子行却没包 SET IDENTITY_INSERT ON", table)
		require.Equalf(t, 1, offCount[table], "%s 的 SET IDENTITY_INSERT 没有配对的 OFF", table)
	}
	for table, count := range onCount {
		require.Truef(t, want[table], "%s 没有种子行/没有自增主键，却写了 SET IDENTITY_INSERT", table)
		require.Equalf(t, count, offCount[table], "%s 的 SET IDENTITY_INSERT ON/OFF 不配对", table)
	}
}

// TestSQLServerBinaryCollationsMatchIR 保住 MySQL *_bin/ascii_bin 到 SQL Server
// 大小写敏感排序的映射。SQL Server 默认排序规则通常大小写不敏感；如果基线丢掉
// 这条列级 COLLATE，增量迁移与全新安装的唯一键语义就会分叉。
func TestSQLServerBinaryCollationsMatchIR(t *testing.T) {
	ir := readIR(t)
	sqlserver := readForTestLowered(t, "sqlserver_converted.sql")

	for _, table := range ir.Tables {
		section, found := tableSection(sqlserver, table.Name)
		require.Truef(t, found, "sqlserver_converted.sql 未找到 %s", table.Name)
		for _, column := range table.Columns {
			if !strings.HasSuffix(strings.ToLower(column.Collation), "_bin") {
				continue
			}
			pattern := regexp.MustCompile(
				`\[` + regexp.QuoteMeta(strings.ToLower(column.Name)) +
					`\]\s+(?:nchar|nvarchar)\([^)]*\)\s+collate\s+latin1_general_100_bin2\b`,
			)
			require.Regexpf(t, pattern, section,
				"%s.%s 的 %s 未映射为 SQL Server Latin1_General_100_BIN2",
				table.Name, column.Name, column.Collation)
		}
	}
}

func TestInitializationDiagnosisSchemaContract(t *testing.T) {
	columns := []string{
		"gb_sip_trace_session_diagnosis", "session_day", "observed_at", "correlation_key",
		"state", "category", "code", "stage", "source", "device_id", "channel_id",
		"call_id", "cseq", "method", "status_code", "stream_id", "resolved_at",
		"uk_sip_trace_diagnosis_session", "idx_sip_trace_diagnosis_category_state_observed",
		"idx_sip_trace_diagnosis_device_observed", "idx_sip_trace_diagnosis_call_cseq",
	}
	for _, file := range declarationFiles {
		t.Run(file, func(t *testing.T) {
			body, err := os.ReadFile(file)
			require.NoError(t, err)
			text := strings.ToLower(string(body))
			// 之前这里找不到表时会退化成返回全文，导致 SQL Server 文件整张表缺失
			// 也照样通过。现在找不到就明确失败。
			require.Containsf(t, text, "gb_sip_trace_session_diagnosis",
				"%s 缺少 SIP 诊断表", file)
			// 找不到建表段落时必须显式失败。历史缺陷正是「找不到就退化成返回全文」：
			// SQL Server 文件整张表缺失也照样通过；PostgreSQL 给标识符加上双引号之后
			// 又会重演同一个退化。宁可变红，不要静默通过。
			section, found := diagnosisSchemaSection(text)
			require.Truef(t, found, "%s 未找到 SIP 诊断表的建表段落", file)
			for _, column := range columns {
				require.Containsf(t, section, column,
					"fresh schema missing diagnosis field or index %s", column)
			}
			for _, forbidden := range []string{"json", "enum", "partial index"} {
				require.NotContainsf(t, section, forbidden,
					"fresh schema contains unsupported feature %s", forbidden)
			}
		})
	}
}

func TestPostgreSQLInitializationMenuTypeSupportsDirectoryPageAndButton(t *testing.T) {
	body, err := os.ReadFile("postgresql_converted.sql")
	require.NoError(t, err)
	menuSchema := strings.ToLower(string(body))
	// sys_menu.type 承载目录/页面/按钮的 1/2/3，所以必须是 SMALLINT 而不是 BOOLEAN。
	// 允许 NOT NULL 出现在类型与默认值之间；列名可能带双引号（PostgreSQL profile 一律加引号）。
	require.Regexpf(t, regexp.MustCompile(`"?type"?\s+smallint(\s+not\s+null)?\s+default\s+2`),
		menuSchema, "sys_menu.type 必须承载目录/页面/按钮的 1/2/3")
	require.NotRegexp(t, regexp.MustCompile(`"?type"?\s+boolean`),
		menuSchema, "BOOLEAN 无法保存页面和按钮类型")
}

func diagnosisSchemaSection(text string) (string, bool) {
	return tableSection(text, "gb_sip_trace_session_diagnosis")
}

// tableSection 截出一张表的建表段落（从 CREATE TABLE 到下一个表/注释为止）。
// 用正则在「建表语句本身」上定位，而不是枚举三种引号形态的字面量 ——
// PostgreSQL 一度裸写表名、现在一律加双引号，枚举法会随生成器演进而静默失效。
// 找不到时返回 false，调用方必须显式失败，绝不能退化成「返回全文」。
func tableSection(text, table string) (string, bool) {
	// 表名后必须紧跟 `(` —— SQLite 产物按名字排序，`gb_channel` 会先撞上
	// `gb_channel_favorite_group` 的前缀。RE2 没有否定先行断言，用「后面必须是
	// 左括号」把边界表达出来。
	pattern := regexp.MustCompile(
		"(?im)^[ \\t]*CREATE TABLE\\s+(?:IF NOT EXISTS\\s+)?" +
			"[`\"\\[]?" + regexp.QuoteMeta(table) + "[`\"\\]]?[ \\t\\r\\n]*\\(")
	loc := pattern.FindStringIndex(text)
	if loc == nil {
		return "", false
	}

	// ⛔ 截断点必须从「建表头部之后」开始找。若从 section[1:] 找，而输入又被
	// 调用方小写化过（readForTestLowered 会做这件事），那么 `create table ` 在位置 0
	// 就撞上 section 自身，整个段落被截成 1 个字符。那时 NotContains/NotRegexp
	// 类断言会恒真 —— 静默假绿，比报错危险得多。
	end := len(text)
	endPattern := regexp.MustCompile(
		`(?im)^(?:-- table structure for|create table |if object_id\(n')`)
	if m := endPattern.FindStringIndex(text[loc[1]:]); m != nil {
		end = loc[1] + m[0]
	}
	return text[loc[0]:end], true
}
