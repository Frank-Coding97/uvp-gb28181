package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/migration"
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

func TestMigrationDifference(t *testing.T) {
	require.Equal(t, []string{"b.sql", "c.sql"}, migrationDifference([]string{"a.sql"}, []string{"a.sql", "b.sql", "c.sql"}))
}
