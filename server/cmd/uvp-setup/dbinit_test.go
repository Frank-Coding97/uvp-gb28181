package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// dbCounts 独立于被测代码地数一遍库里的东西（表数/索引数/种子行数/admin）。
func dbCounts(t *testing.T, dbPath string) (tables, indexes, seeded int, adminRows int, adminChanged bool) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("打开建好的库失败: %v", err)
	}
	defer func() { _ = db.Close() }()

	one := func(query string) int {
		var n int
		if err := db.QueryRow(query).Scan(&n); err != nil {
			t.Fatalf("查询 %q 失败: %v", query, err)
		}
		return n
	}
	tables = one("SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
	indexes = one("SELECT count(*) FROM sqlite_master WHERE type='index' AND name NOT LIKE 'sqlite_%'")
	adminRows = one("SELECT count(*) FROM sys_users WHERE username='admin'")
	adminChanged = one("SELECT count(*) FROM sys_users WHERE username='admin' AND must_change_password=1") == 1

	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		t.Fatalf("列出表失败: %v", err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			t.Fatalf("读表名失败: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("关闭结果集失败: %v", err)
	}
	for _, name := range names {
		seeded += one(fmt.Sprintf("SELECT count(*) FROM %q", name))
	}
	return
}

var summaryRe = regexp.MustCompile(
	`✓ (\d+) 张表 · (\d+) 个索引 · (\d+) 行种子数据 · 管理员账号已就位（共执行 (\d+) 条语句）`)

// TestDBInitBuildsUsableDatabaseFromRealBaseline 端到端跑一次**真实基线**建库。
//
// 这是绿色包安装时唯一一步"会把库写坏且很难发现"的操作，而此前只有
// planStatements 层面的单测 —— 没人跑过完整的 initDatabase：
//
//	① 结尾对账行必须与**独立回读**的数字一致（自己报自己不算数）；
//	② admin 必须恰好 1 个，且 must_change_password=1（否则首次登录不会强制改密）；
//	③ 再跑一次必须走"已就绪、跳过"分支（安装脚本重启时会走到，不能重建库）。
//
// ⛔ 数字刻意不写死（112/313 那种）：那是 TestBaselineGoldenCounts 的职责，
// 这里锁的是"**打印的 = 库里的**"，基线换版时不该跟着改两份。
func TestDBInitBuildsUsableDatabaseFromRealBaseline(t *testing.T) {
	baseline := baselinePath(t)
	dbPath := filepath.Join(t.TempDir(), "data", "uvp.db")

	out := captureStdout(t, func() {
		if err := runDBInit([]string{"--baseline", baseline, "--db", dbPath}); err != nil {
			t.Fatalf("db-init 失败: %v", err)
		}
	})
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("db-init 报成功但库文件不存在: %v", err)
	}

	// 阶段行（安装时用户看到的就是这两类）
	for _, want := range []string{"建表 ", "灌种子数据 "} {
		if !strings.Contains(out, want) {
			t.Errorf("输出里缺少阶段行 %q\n--- stdout ---\n%s", want, out)
		}
	}
	if strings.Contains(out, "\r") {
		t.Errorf("非交互场景不该画进度条（输出里出现了 \\r）\n--- stdout ---\n%s", out)
	}

	m := summaryRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("结尾对账行不见了（安装时最后一句就是它）\n--- stdout ---\n%s", out)
	}
	gotTables, _ := strconv.Atoi(m[1])
	gotIndexes, _ := strconv.Atoi(m[2])
	gotSeeded, _ := strconv.Atoi(m[3])

	tables, indexes, seeded, adminRows, adminChanged := dbCounts(t, dbPath)
	if gotTables != tables {
		t.Errorf("对账行说 %d 张表，实际库里 %d 张", gotTables, tables)
	}
	if gotIndexes != indexes {
		t.Errorf("对账行说 %d 个索引，实际库里 %d 个", gotIndexes, indexes)
	}
	if gotSeeded != seeded {
		t.Errorf("对账行说 %d 行种子，实际库里 %d 行", gotSeeded, seeded)
	}
	if tables == 0 || indexes == 0 || seeded == 0 {
		t.Errorf("建出来的库是空的：表 %d / 索引 %d / 种子 %d", tables, indexes, seeded)
	}
	if adminRows != 1 {
		t.Errorf("admin 账号数 = %d，期望恰好 1 个", adminRows)
	}
	if !adminChanged {
		t.Error("admin 的 must_change_password 不是 1 —— 首次登录就不会强制改密了")
	}

	// 幂等：安装脚本重启时会再跑一次，必须走"已就绪、跳过"而不是重建。
	out2 := captureStdout(t, func() {
		if err := runDBInit([]string{"--baseline", baseline, "--db", dbPath}); err != nil {
			t.Fatalf("第二次 db-init 失败（重跑必须幂等）: %v", err)
		}
	})
	if !strings.Contains(out2, "数据库已就绪") {
		t.Errorf("第二次应走「已就绪、跳过」分支\n--- stdout ---\n%s", out2)
	}
	if strings.Contains(out2, "建表 ") {
		t.Errorf("第二次不该重新建表\n--- stdout ---\n%s", out2)
	}
}
