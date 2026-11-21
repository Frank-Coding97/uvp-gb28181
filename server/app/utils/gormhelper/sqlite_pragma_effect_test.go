package gormhelper

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/glebarez/sqlite"
)

// TestSQLiteDSNPragmasTakeEffect 证明 DSN 上的 PRAGMA **真的被执行了**。
//
// 为什么必须有这条：DSN 里的 `_pragma=xxx` 是驱动层的约定，拼错名字、写错大小写、
// 用了该驱动不认识的参数形式，都不会报错 —— 驱动直接忽略，连接照常建立。
// 于是「DSN 字符串里有 synchronous(NORMAL)」和「数据库真的跑在 NORMAL 上」是两回事。
// 前者靠字符串断言就够，后者只能打开连接读 PRAGMA 实测。
//
// 这条尤其重要：2026-11-09 千路压测发现 SQLite 默认 synchronous=FULL 把写吞吐钉死在
// 48 次/秒（改 NORMAL 后 2745 次/秒），如果这个 PRAGMA 落地失败，故障会原样复发。
func TestSQLiteDSNPragmasTakeEffect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pragma.db")
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatalf("按 DSN 打开 SQLite 失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// SetMaxOpenConns(1)：与 GetOneSqliteClient 对齐，保证读到的就是业务连接那份设置。
	db.SetMaxOpenConns(1)

	var sync int
	if err := db.QueryRow("PRAGMA synchronous").Scan(&sync); err != nil {
		t.Fatalf("读取 synchronous 失败: %v", err)
	}
	// 1 = NORMAL。0=NORMAL 以外的值（2=FULL 是出厂默认）都说明这条 PRAGMA 没落地。
	if sync != 1 {
		t.Fatalf("synchronous 应为 1(NORMAL)，实得 %d —— DSN 里的该 PRAGMA 未被驱动执行\nDSN: %s", sync, sqliteDSN(path))
	}

	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("读取 journal_mode 失败: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode 应为 wal，实得 %q", journalMode)
	}

	var fk int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("读取 foreign_keys 失败: %v", err)
	}
	if fk != 1 {
		t.Fatalf("foreign_keys 应为 1，实得 %d", fk)
	}

	// auto_vacuum 只有 2(INCREMENTAL) 才符合预期。
	// ⛔ 0(NONE) 说明这条 PRAGMA 没能在建库那一刻生效 —— 它必须排在
	//   journal_mode 之前，否则文件头已经被写成 NONE 了。
	var autoVacuum int
	if err := db.QueryRow("PRAGMA auto_vacuum").Scan(&autoVacuum); err != nil {
		t.Fatalf("读取 auto_vacuum 失败: %v", err)
	}
	if autoVacuum != 2 {
		t.Fatalf("auto_vacuum 应为 2(INCREMENTAL)，实得 %d —— 该 PRAGMA 必须在建库前生效\nDSN: %s", autoVacuum, sqliteDSN(path))
	}
}

// TestSQLiteAutoVacuumSurvivesReopen 证明这个模式是**写进文件头**的，不是连接态。
//
// 关心点：重开连接后如果退回 0，说明只是当前连接记得，文件其实还是 NONE，
// 那么第二天重启服务，增量回收就全失效了（而且不会报错）。
func TestSQLiteAutoVacuumSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reopen.db")

	first, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatalf("首次打开失败: %v", err)
	}
	first.SetMaxOpenConns(1)
	if _, err := first.Exec("CREATE TABLE probe (id INTEGER PRIMARY KEY, body TEXT)"); err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	if _, err := first.Exec("INSERT INTO probe (body) VALUES ('x')"); err != nil {
		t.Fatalf("写数据失败: %v", err)
	}
	_ = first.Close()

	second, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatalf("再次打开失败: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })
	second.SetMaxOpenConns(1)

	var autoVacuum int
	if err := second.QueryRow("PRAGMA auto_vacuum").Scan(&autoVacuum); err != nil {
		t.Fatalf("重开后读取 auto_vacuum 失败: %v", err)
	}
	if autoVacuum != 2 {
		t.Fatalf("重开后 auto_vacuum 应为 2，实得 %d —— 模式没写进文件头", autoVacuum)
	}
}
