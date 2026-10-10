package migration

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// ⭐ 这组用例锁的是「绿色安装包默认走 SQLite」这条链路上曾经漏掉的地方。
//
// 背景：接 SQLite 时漏了 DialectOf 不认 SQLite ⇒ 返回 DialectUnknown。
// 而它**不会在开发机上暴露** —— 开发库是 MySQL，只有真机（SQLite）才走到这个分支。
//
// ⚠️ 本文件刻意放在 migration 包内而不是根包：根包的测试二进制会 import
// bootstrap，而它的 init() 需要真实环境（日志目录/时区）才能过，
// 于是任何根包用例都会以 `phase: logging` 失败 —— 那是环境问题，不是用例问题。
//
// 判据要点：**断言行为，不断言常量存在**。只断言 DialectSQLite 有定义的话，
// 把 DialectOf 的 case 删掉测试照样绿 —— 而那正是最初的 bug。

func TestDialectSQLiteValue(t *testing.T) {
	// 方言字符串会拼进迁移文件名，改名等于让历史迁移文件失联。
	if got := string(DialectSQLite); got != "sqlite" {
		t.Fatalf("DialectSQLite 必须是 \"sqlite\"（迁移文件名依赖它），实际 %q", got)
	}
}

func TestDialectOfRecognizesSQLite(t *testing.T) {
	// ⛔ 最初漏掉的就是这处：SQLite 的 dialector 没有 case 时会被判成 Unknown。
	// 两种形态都要覆盖 —— DialectOf 是 type switch，生产上可能拿到
	// 值形式（sqlite.Open 的返回值）或指针形式（&sqlite.Dialector{}）。
	cases := map[string]gorm.Dialector{
	 "指针形态": &sqlite.Dialector{},
	 "值形态":   sqlite.Dialector{},
	}
	for name, dialector := range cases {
		if got := DialectOf(dialector); got != DialectSQLite {
			t.Errorf("%s：DialectOf 对 SQLite 应返回 DialectSQLite，实际 %q", name, got)
		}
	}
}

// TestDialectOfUnknownStillUnknown 护栏：别把未知方言也判成 sqlite。
func TestDialectOfUnknownStillUnknown(t *testing.T) {
	if got := DialectOf(nil); got != DialectUnknown {
		t.Fatalf("nil dialector 应为 DialectUnknown，实际 %q", got)
	}
}
