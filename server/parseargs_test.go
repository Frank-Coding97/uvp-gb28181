package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/migration"
)

// 5.3:无 flag 正常启动
func TestParseArgsNoFlag(t *testing.T) {
	require.Equal(t, "", parseArgs([]string{"uvp-gb28181"}))
}

// 5.4:有 flag 返回文件名
func TestParseArgsWithFlag(t *testing.T) {
	require.Equal(t, "2026-07-20-a.sql", parseArgs([]string{"uvp-gb28181", "-migrate-down=2026-07-20-a.sql"}))
}

// 5.5:未知 flag 不 panic,正常启动路径
func TestParseArgsUnknownFlag(t *testing.T) {
	require.Equal(t, "", parseArgs([]string{"uvp-gb28181", "-unknown-flag=x"}))
}

func TestMigrateUpRequested(t *testing.T) {
	require.True(t, migrateUpRequested([]string{"uvp-gb28181", "-migrate-up"}))
	require.False(t, migrateUpRequested([]string{"uvp-gb28181", "-migrate-up=false"}))
	require.False(t, migrateUpRequested([]string{"uvp-gb28181"}))
}

func TestDatabaseIdentitySQLSupportsConfiguredDialects(t *testing.T) {
	require.Contains(t, databaseIdentitySQL(migration.DialectMySQL), "DATABASE()")
	require.Contains(t, databaseIdentitySQL(migration.DialectPostgres), "current_database()")
	require.Contains(t, databaseIdentitySQL(migration.DialectSQLServer), "DB_NAME()")
}

// TestDatabaseIdentitySQLWrapsMySQLProbeInCast 锁住 MySQL 方言身份查询里的 CAST。
//
// ⛔ 不能退回裸的 `SELECT DATABASE() AS database_name, VERSION() AS database_version`。
// PolarDB-X（开源版 v2.4.2 / 5.4.19，实测 192.168.10.220）会把这种以 DATABASE()
// 开头的 SELECT 当成特殊语句短路：只返回一列、列名固定为 `DATABASE()`，AS 别名被
// 整体丢弃，VERSION() 那列直接消失。GORM 按 `column:database_name` 匹配不到列，
// 报 `unsupported Scan, storing driver.Value type []uint8 into type *databaseIdentity`
// —— 报错指向"驱动类型不兼容"，真因却是列名没了，排查成本很高。
// 套 CAST 之后它被当成普通表达式，两列与别名都正常返回；对 MySQL 8 语义等价。
func TestDatabaseIdentitySQLWrapsMySQLProbeInCast(t *testing.T) {
	sql := databaseIdentitySQL(migration.DialectMySQL)
	require.Contains(t, sql, "CAST(DATABASE() AS CHAR) AS database_name")
	require.Contains(t, sql, "CAST(VERSION() AS CHAR) AS database_version")
}

func TestMigrationDifference(t *testing.T) {
	require.Equal(t, []string{"b.sql", "c.sql"}, migrationDifference([]string{"a.sql"}, []string{"a.sql", "b.sql", "c.sql"}))
}
