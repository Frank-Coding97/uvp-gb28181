package models

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoMySQLOnlySQLInModels 拦住 **MySQL 专有 SQL 语法**混进模型层。
//
// ⭐ 这条护栏来自一次真实踩坑：ListStaleOnline 用了
//
//	keepalive_time < DATE_SUB(NOW(), INTERVAL (keepalive_interval * ? + ?) SECOND)
//
// 而 DATE_SUB / NOW() / INTERVAL 三个都是 MySQL 专有，SQLite 里
// `no such function: DATE_SUB` ⇒ 查询直接失败。
//
// ⚠️ 症状极具误导性，值得记下来：日志只有一行
//   「离线扫描:查询超时设备失败」（后台定时任务，每 30 秒一次），
//   而**用户看到的是设备列表页报「DB 未就绪」**
//   ⇒ 一个后台扫描的 SQL 错误，在前端表现为"整个数据库连不上"。
//   当时差点去查连接配置，方向完全错了。
//
// 为什么用「扫源码」而不是「跑 SQLite」：
//   真跑要连库、要造数据，而且**只覆盖写了测试的那几个函数**；
//   扫源码能覆盖整个包，新增 SQL 时立刻就会被拦下。
//   两者互补：这里管"不许引入"，单测管"我写的对不对"。
func TestNoMySQLOnlySQLInModels(t *testing.T) {
	// MySQL 专有、SQLite 没有的函数/语法。
	// ⛔ 刻意**不收** IFNULL / CONCAT / CONCAT_WS / NOW：
	//   SQLite 内置了等价实现（ifnull/concat），收了会误报。
	mysqlOnly := []string{
		"DATE_SUB", "DATE_ADD", "DATE_DIFF", "CURDATE", "CURTIME",
		"GROUP_CONCAT", "UNIX_TIMESTAMP", "FROM_UNIXTIME", "LAST_INSERT_ID",
		"INTERVAL",     // 只在 INTERVAL n UNIT 形式下是方言；GORM 里少见，此处从严
	}

	// 只看这些文件：模型层的查询构造处。控制器/服务层若有同样问题，
	// 那是另一批（见 TestNoMySQLOnlySQLRepoWide 的思路），不混在一起。
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("没找到模型层文件（cwd=%s）", mustGetwd())
	}

	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		content, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", f, err)
		}
		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			// 只看像 SQL 的行：含引号且有 SQL 关键字，或显式构造 SQL。
			upper := strings.ToUpper(trimmed)
			if !strings.Contains(trimmed, `"`) && !strings.Contains(trimmed, "`") {
				continue
			}
			isSQL := strings.Contains(upper, "SELECT ") || strings.Contains(upper, "WHERE ") ||
				strings.Contains(upper, "UPDATE ") || strings.Contains(upper, "INSERT ") ||
				strings.Contains(upper, "DELETE ") || strings.Contains(upper, "ORDER BY") ||
				strings.Contains(upper, "GROUP BY")
			if !isSQL {
				continue
			}
			for _, fn := range mysqlOnly {
				if strings.Contains(upper, fn) {
					// 显式声明方言分支的地方是**允许**的：那里正是为了兼容而写的。
					if hasDialectGuardNearby(lines, i) {
						continue
					}
					t.Errorf(
						"%s:%d 用了 MySQL 专有语法 %q，而本包要同时支持 SQLite。\n"+
							"    %s\n"+
							"    ⇒ 改成方言分支，或用 SQLite 等价表达式。\n"+
							"    例：DATE_SUB(NOW(), INTERVAL n SECOND) →\n"+
							"        SQLite: datetime('now', '-' || (k*n) || ' seconds')\n"+
							"        ⭐ 注意 julianday() 对 DATETIME 列返回 NULL（静默错误，别用）。\n"+
							"    若已显式按方言分支，请确认该行上方的注释说明了原因。",
						f, i+1, fn, trimmed)
				}
			}
		}
	}
}

// hasDialectGuardNearby 判断该行附近是否有方言分支的显式声明。
//
// ⛔ 判据要**窄**：只认 "sqlite"/"Dialect" 字样或注释里的说明，
//   否则整条护栏会因为"文件里恰好出现过 sqlite"而整体失效。
func hasDialectGuardNearby(lines []string, idx int) bool {
	from := idx - 6
	if from < 0 {
		from = 0
	}
	to := idx + 1
	if to > len(lines) {
		to = len(lines)
	}
	for _, l := range lines[from:to] {
		if strings.Contains(strings.ToLower(l), "sqlite") || strings.Contains(l, "Dialect") {
			return true
		}
	}
	return false
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "unknown"
	}
	return wd
}

// TestListStaleOnlineSQLHasNoMySQLOnlySyntax 精确守住这一处。
// 护栏在上面（整包扫描），这条负责说明**为什么**它需要方言分支，
// 并把 SQLite 的等价写法钉住，避免以后有人"顺手简化"回 MySQL 版。
func TestListStaleOnlineSQLHasNoMySQLOnlySyntax(t *testing.T) {
	content, err := os.ReadFile("gb_device.go")
	if err != nil {
		t.Fatalf("读取 gb_device.go 失败: %v", err)
	}
	src := string(content)

	// 函数体里必须出现 SQLite 分支
	if !regexp.MustCompile(`(?s)func ListStaleOnline.*?sqlite`).MatchString(src) {
		t.Errorf("ListStaleOnline 缺少 SQLite 方言分支。\n"+
			"    ⛔ DATE_SUB/NOW()/INTERVAL 在 SQLite 下是 no such function，\n"+
			"       表现为设备列表页报「DB 未就绪」，而日志只有后台扫描的一行警告。")
	}

	// SQLite 分支必须用 datetime(...)—— 不能是 julianday（对 DATETIME 列返回 NULL）
	if strings.Contains(src, "julianday(keepalive_time)") {
		t.Errorf("ListStaleOnline 用了 julianday(keepalive_time)：\n"+
			"    keepalive_time 列声明为 DATETIME、实际存 'YYYY-MM-DD HH:MM:SS' 文本，\n"+
			"    julianday() 对这种值返回 NULL ⇒ 条件恒不成立 ⇒ **静默地扫不出任何设备**，\n"+
			"    不报错但功能失效。比报错更难发现。")
	}
}
