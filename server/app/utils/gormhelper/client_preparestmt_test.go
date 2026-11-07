package gormhelper

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestUsePrepareStmtDisablesPolarDB 锁住「PolarDB 默认不走客户端预编译」这条口径。
//
// ⛔ 为什么必须存在：PolarDB-X（开源版 v2.4.2 / 5.4.19）的 COM_STMT_PREPARE 有语句
// 限制 —— 启动时的数据库身份探测
// `SELECT DATABASE() AS database_name, VERSION() AS database_version`
// 走文本协议正常，一旦被 GORM 缓存成预编译语句就报
// `Prepare does not support sql: SELECT DATABASE() ...`（实测 192.168.10.220）。
// 服务端的 `PREPARE ... FROM '...'` 反而是通的，所以这不是 SQL 写法的问题，
// 关预编译是唯一干净的解法。
//
// 症状很误导：进程起不来，日志只有「读取数据库身份失败」，看着像连不上库。
//
// 其余方言保持开启 —— 它们是真正的 MySQL / PostgreSQL 内核，没有这个限制。
func TestUsePrepareStmtDisablesPolarDB(t *testing.T) {
	require.False(t, usePrepareStmt("polardb"),
		"PolarDB 必须关闭 PrepareStmt，否则身份探测语句无法预编译，进程直接起不来")

	// 反向确认：别把整条链路一起关掉（关掉对 MySQL/PG 只是白丢一层语句缓存）。
	require.True(t, usePrepareStmt("mysql"))
	require.True(t, usePrepareStmt("postgresql"))
	require.True(t, usePrepareStmt("sqlserver"))
	require.True(t, usePrepareStmt("kingbase"))
	require.True(t, usePrepareStmt("dm"))
}
