package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot 从 server/cmd/uvp-setup 回到仓库根。
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("解析仓库根失败: %v", err)
	}
	return root
}

func baselinePath(t *testing.T) string {
	t.Helper()
	p := filepath.Join(repoRoot(t), "server", "resource", "database", "sqlitebaseline", "baseline.sql")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("baseline.sql 不存在（%s），跳过", p)
	}
	return p
}

func loadStatements(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(baselinePath(t))
	if err != nil {
		t.Fatalf("读取 baseline.sql 失败: %v", err)
	}
	stmts := splitSQL(string(raw))
	if len(stmts) == 0 {
		t.Fatal("切分结果为空")
	}
	return stmts
}

// TestBaselineGoldenCounts 钉住「基线能切出多少个对象」。
//
// ⛔ 这三个数字是 Linux 绿色包安装时打印的那三个（`111 张表 · 307 个索引 ·
//
//	38 条种子语句`）。数字变了就说明 baseline.sql 换了版本 —— 那时应当**显式**
//	更新本用例，而不是让它悄悄漂移。
func TestBaselineGoldenCounts(t *testing.T) {
	stmts := loadStatements(t)
	phases, counted, dupes, total := planStatements(stmts)

	counts := map[string]int{}
	for i, ok := range counted {
		if ok {
			counts[phases[i]]++
		}
	}
	if got := counts[phaseTable]; got != 111 {
		t.Errorf("建表语句数 = %d，期望 111", got)
	}
	if got := counts[phaseIndex]; got != 307 {
		t.Errorf("能真正建出的索引数 = %d，期望 307", got)
	}
	if got := counts[phaseSeed]; got != 38 {
		t.Errorf("种子数据语句数 = %d，期望 38", got)
	}
	if got := counts[phasePragma]; got != 2 {
		t.Errorf("PRAGMA 语句数 = %d，期望 2（必须能在事务外执行）", got)
	}
	if total != len(stmts)-len(dupes) {
		t.Errorf("分母 %d 与「语句数-重复数」%d 不一致", total, len(stmts)-len(dupes))
	}
	// 已知基线缺陷（详见 topics/migrations-and-db.md）：MySQL 迁移过来的索引名
	// 只在表内唯一、SQLite 是库级唯一 ⇒ 同名索引被静默跳过，实测 9 条。
	// ⛔ 这里刻意断言「确实存在被跳过的」，好在未来修复后立刻发现并更新结论。
	if len(dupes) == 0 {
		t.Error("预期存在被 SQLite 静默跳过的重复索引名，实际为 0 —— 基线或算法变了？")
	}
}

// TestSplitDigestMatchesPython 与基线生成器 generate.py 的 split_sql() 对拍。
//
// ⛔⛔ 这是**三份实现**（generate.py / ctl.sh 内嵌 Python / 本 Go 版）之间唯一的
//
//	硬锁：指纹格式与 ctl.sh 的 UVP_DB_SPLIT_DIGEST=1 完全相同，
//	任何一处切分规则漂移都会立刻红。
func TestSplitDigestMatchesPython(t *testing.T) {
	stmts := loadStatements(t)
	goDigest := "stmts=" + itoa(len(stmts)) + " sha256=" + splitDigest(stmts)

	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("未找到 python3，跳过与 generate.py 的对拍（CI 环境请装 python3）")
	}
	gen := filepath.Join(repoRoot(t), "server", "resource", "database", "sqlitebaseline", "generate.py")
	script := `
import hashlib, importlib.util, sys
spec = importlib.util.spec_from_file_location("uvp_baseline_gen", r"""` + gen + `""")
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)
stmts = mod.split_sql(open(r"""` + baselinePath(t) + `""", encoding="utf-8").read())
print("stmts=%d sha256=%s" % (len(stmts), hashlib.sha256("\n\x1e\n".join(stmts).encode("utf-8")).hexdigest()))
`
	out, err := exec.Command(py, "-c", script).CombinedOutput()
	if err != nil {
		t.Skipf("调用 python3 失败（%v）：%s", err, strings.TrimSpace(string(out)))
	}
	pyDigest := strings.TrimSpace(string(out))
	if pyDigest != goDigest {
		t.Fatalf("切分指纹与 generate.py 不一致\n  python: %s\n  go    : %s", pyDigest, goDigest)
	}
}

func TestSplitSQLCases(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "字符串里的分号不切",
			in:   "INSERT INTO t VALUES ('a;b');INSERT INTO t VALUES ('c');",
			want: []string{"INSERT INTO t VALUES ('a;b')", "INSERT INTO t VALUES ('c')"},
		},
		{
			name: "行注释里的分号不切",
			in:   "-- 回调地址 http://x/?a=1;b=2\nCREATE TABLE t (a INT);",
			want: []string{"CREATE TABLE t (a INT)"},
		},
		{
			name: "块注释里的分号不切",
			in:   "/* a;b */\nCREATE TABLE t (a INT);",
			want: []string{"CREATE TABLE t (a INT)"},
		},
		{
			name: "单引号用反斜杠转义",
			in:   `INSERT INTO t VALUES ('it\'s;ok');`,
			want: []string{`INSERT INTO t VALUES ('it\'s;ok')`},
		},
		{
			name: "单引号用双写转义",
			in:   "INSERT INTO t VALUES ('it''s;ok');",
			want: []string{"INSERT INTO t VALUES ('it''s;ok')"},
		},
		{
			name: "双引号里的分号不切",
			in:   `SELECT "a;b" FROM t;`,
			want: []string{`SELECT "a;b" FROM t`},
		},
		{
			name: "结尾无分号的尾语句也算一条",
			in:   "CREATE TABLE t (a INT);\nSELECT 1",
			want: []string{"CREATE TABLE t (a INT)", "SELECT 1"},
		},
		{
			name: "空白语句被丢弃",
			in:   "CREATE TABLE t (a INT);;\n;\n",
			want: []string{"CREATE TABLE t (a INT)"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitSQL(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("语句数 = %d，期望 %d：%#v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("第 %d 条 = %q，期望 %q", i+1, got[i], tc.want[i])
				}
			}
		})
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
