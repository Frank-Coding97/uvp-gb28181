package gormhelper

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

// TestMySQLIsolationLevelTakesEffect 证明 DSN 上的隔离级别**真的被连接执行了**。
//
// 为什么必须有这条：DSN 里的 `transaction_isolation=%27...%27` 走的是驱动
// 「未知参数当系统变量」的约定。参数名拼错、引号编码不对，驱动都不会报错 ——
// 它只是把 `SET xxx = yyy` 发出去，要么建连成功但设了个无关变量，要么拼出
// 非法 SQL 被重连逻辑吞掉。所以「DSN 字符串里有 READ-COMMITTED」和
// 「连接真的跑在 RC 上」是两回事。
//
// 这条尤其重要：2026-11-09 万级压测里，REPEATABLE-READ 的间隙锁导致
// gb_channel_mount 死锁 6311 条、通道覆盖率只有 8.4%。如果这个参数
// 落地失败，故障会原样复发，而日志上只会看到"死锁"这个结果、看不到原因。
//
// 需要真实 MySQL，用环境变量提供连接串（可以不带库名）：
//
//	UVP_TEST_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/' go test ./app/utils/gormhelper/ -run MySQLIsolation
//
// 未设置时跳过 —— 本地开发不该被外部依赖卡住。
func TestMySQLIsolationLevelTakesEffect(t *testing.T) {
	rawDSN := os.Getenv("UVP_TEST_MYSQL_DSN")
	if rawDSN == "" {
		t.Skip("未设置 UVP_TEST_MYSQL_DSN，跳过（该用例需要真实 MySQL）")
	}

	// 复用生产代码算出的隔离级别，避免测试自己另写一份而测不到真东西。
	useDSNConfig(t, map[string]interface{}{})
	want := mysqlTransactionIsolation("mysql")

	sep := "?"
	if strings.Contains(rawDSN, "?") {
		sep = "&"
	}
	dsn := rawDSN + sep + "transaction_isolation=%27" + want + "%27"

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("按 DSN 打开 MySQL 失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var level string
	if err := db.QueryRow("SELECT @@session.transaction_isolation").Scan(&level); err != nil {
		t.Fatalf("读取 transaction_isolation 失败: %v", err)
	}

	if level != want {
		t.Fatalf("会话隔离级别应为 %s，实得 %q —— DSN 里的该参数未被驱动执行\nDSN: %s",
			want, level, dsn)
	}
}
