package migration

import (
	"os"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 2026-10-27：对讲的三列（openapi_client_id / openapi_idempotency_key /
// openapi_request_hash）当年只加进了 baseline，没有迁移 ⇒ 存量库升级后表里
// 40 列，而 models.GbTalkSession 按 43 列写入，创建对讲会话直接失败。
// 62 上级（1.1.13 存量库）实测现象：前端点「开始广播」只弹「语音对讲操作失败」。
// 下面这组用例锁三件事：文件齐全、老库能补上、新装基线库不重复执行。
var talkOpenAPIColumnMigrations = []struct {
	dialect Dialect
	column  string
	up      string
	down    string
}{
	{DialectMySQL, "openapi_client_id", "2026-10-27-talk-openapi-client-id.sql", "2026-10-27-talk-openapi-client-id-down.sql"},
	{DialectMySQL, "openapi_idempotency_key", "2026-10-27-talk-openapi-idempotency-key.sql", "2026-10-27-talk-openapi-idempotency-key-down.sql"},
	{DialectMySQL, "openapi_request_hash", "2026-10-27-talk-openapi-request-hash.sql", "2026-10-27-talk-openapi-request-hash-down.sql"},
	{DialectPostgres, "openapi_client_id", "2026-10-27-talk-openapi-client-id-postgresql.sql", "2026-10-27-talk-openapi-client-id-postgresql-down.sql"},
	{DialectPostgres, "openapi_idempotency_key", "2026-10-27-talk-openapi-idempotency-key-postgresql.sql", "2026-10-27-talk-openapi-idempotency-key-postgresql-down.sql"},
	{DialectPostgres, "openapi_request_hash", "2026-10-27-talk-openapi-request-hash-postgresql.sql", "2026-10-27-talk-openapi-request-hash-postgresql-down.sql"},
	{DialectSQLServer, "openapi_client_id", "2026-10-27-talk-openapi-client-id-sqlserver.sql", "2026-10-27-talk-openapi-client-id-sqlserver-down.sql"},
	{DialectSQLServer, "openapi_idempotency_key", "2026-10-27-talk-openapi-idempotency-key-sqlserver.sql", "2026-10-27-talk-openapi-idempotency-key-sqlserver-down.sql"},
	{DialectSQLServer, "openapi_request_hash", "2026-10-27-talk-openapi-request-hash-sqlserver.sql", "2026-10-27-talk-openapi-request-hash-sqlserver-down.sql"},
	{DialectSQLite, "openapi_client_id", "2026-10-27-talk-openapi-client-id-sqlite.sql", "2026-10-27-talk-openapi-client-id-sqlite-down.sql"},
	{DialectSQLite, "openapi_idempotency_key", "2026-10-27-talk-openapi-idempotency-key-sqlite.sql", "2026-10-27-talk-openapi-idempotency-key-sqlite-down.sql"},
	{DialectSQLite, "openapi_request_hash", "2026-10-27-talk-openapi-request-hash-sqlite.sql", "2026-10-27-talk-openapi-request-hash-sqlite-down.sql"},
}

func TestTalkOpenAPIColumnMigrationsCoverEveryDialect(t *testing.T) {
	for _, item := range talkOpenAPIColumnMigrations {
		t.Run(string(item.dialect)+"/"+item.column, func(t *testing.T) {
			up, err := (&embedSource{dialect: item.dialect}).ReadSQL(item.up)
			require.NoError(t, err)
			require.Contains(t, up, item.column)
			require.Contains(t, up, "gb_talk_session")
			// 老库才需要补列：登记表里必须有它，否则空版本表的基线库会当成「老库需要执行」。
			require.Equal(t, "gb_talk_session", baselineColumnMigrations[item.up].table)
			require.Equal(t, item.column, baselineColumnMigrations[item.up].column)
			_, err = (&embedSource{dialect: item.dialect}).ReadSQL(item.down)
			require.NoError(t, err)
		})
	}
}

func TestTalkOpenAPIIndexMigrationsCoverEveryDialect(t *testing.T) {
	files := map[Dialect]string{
		DialectMySQL:     "2026-10-27-talk-openapi-idempotency-index.sql",
		DialectPostgres:  "2026-10-27-talk-openapi-idempotency-index-postgresql.sql",
		DialectSQLServer: "2026-10-27-talk-openapi-idempotency-index-sqlserver.sql",
		DialectSQLite:    "2026-10-27-talk-openapi-idempotency-index-sqlite.sql",
	}
	for dialect, name := range files {
		up, err := (&embedSource{dialect: dialect}).ReadSQL(name)
		require.NoError(t, err)
		require.Contains(t, up, "idx_talk_session_openapi_idempotency")
		require.Equal(t, "idx_talk_session_openapi_idempotency", baselineIndexMigrations[name].index)
	}
}

// 存量库（缺三列）跑一遍迁移后，三列与幂等索引都在。
func TestTalkOpenAPIColumnsAreAddedToLegacySQLiteDatabase(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })

	// 62 上实测的老结构：40 列，缺三列。
	require.NoError(t, db.Exec(`CREATE TABLE "gb_talk_session" (
		"id" INTEGER PRIMARY KEY AUTOINCREMENT,
		"session_id" TEXT NOT NULL,
		"channel_id" INTEGER NOT NULL,
		"device_id" TEXT NOT NULL,
		"mode" TEXT NOT NULL DEFAULT 'talk',
		"state" TEXT NOT NULL,
		"expires_at" DATETIME NOT NULL
	)`).Error)

	executor := &dbExecutor{db: db}
	for _, item := range talkOpenAPIColumnMigrations {
		if item.dialect != DialectSQLite {
			continue
		}
		up, err := (&embedSource{dialect: DialectSQLite}).ReadSQL(item.up)
		require.NoError(t, err)
		require.NoError(t, executor.ExecSQL(up), item.up)
		require.True(t, db.Migrator().HasColumn("gb_talk_session", item.column), item.column)
	}

	indexUp, err := (&embedSource{dialect: DialectSQLite}).ReadSQL("2026-10-27-talk-openapi-idempotency-index-sqlite.sql")
	require.NoError(t, err)
	require.NoError(t, executor.ExecSQL(indexUp))
	require.True(t, db.Migrator().HasIndex("gb_talk_session", "idx_talk_session_openapi_idempotency"))

	// 老结构 + 三列后，按模型写入不再报错（这是当初的故障点）。
	require.NoError(t, db.Exec(`INSERT INTO "gb_talk_session"
		("session_id","channel_id","device_id","mode","state","expires_at","openapi_client_id","openapi_request_hash")
		VALUES ('s1',1,'34020000002000000001','talk','reserved','2026-10-27 00:00:00',0,'')`).Error)
}

// 新装基线库已经有这三列 ⇒ runner 必须判定「不需要执行」，否则 ALTER 会直接报
// duplicate column 让升级卡死。
func TestTalkOpenAPIColumnsSkipWhenBaselineAlreadyHasThem(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })

	body, err := os.ReadFile("../../../resource/database/sqlitebaseline/baseline.sql")
	require.NoError(t, err)
	start := strings.Index(string(body), `CREATE TABLE IF NOT EXISTS "gb_talk_session" (`)
	require.NotEqual(t, -1, start, "baseline 里必须有 gb_talk_session")
	section := string(body)[start:]
	end := strings.Index(section, "\n);")
	require.NotEqual(t, -1, end)
	require.NoError(t, db.Exec(section[:end+3]).Error)

	probe := schemaProbe{
		hasTable:  func(table string) (bool, error) { return db.Migrator().HasTable(table), nil },
		hasColumn: func(table, column string) (bool, error) { return db.Migrator().HasColumn(table, column), nil },
	}
	for _, item := range talkOpenAPIColumnMigrations {
		if item.dialect != DialectSQLite {
			continue
		}
		needed, err := needsBaselineColumnUpgrade(item.up, probe)
		require.NoError(t, err)
		require.False(t, needed, "baseline 已含 %s，不应重复执行 %s", item.column, item.up)
	}

	// 反向：把列删掉，判定必须变 true（证明上面的 false 不是恒假）。
	require.NoError(t, db.Exec(`ALTER TABLE "gb_talk_session" DROP COLUMN "openapi_client_id"`).Error)
	needed, err := needsBaselineColumnUpgrade("2026-10-27-talk-openapi-client-id-sqlite.sql", probe)
	require.NoError(t, err)
	require.True(t, needed)
}
