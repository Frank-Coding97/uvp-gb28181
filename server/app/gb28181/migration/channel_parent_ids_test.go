package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const channelParentIDsMigration = "2026-10-13-channel-parent-ids"

var channelParentIDsMigrationFiles = []struct {
	dialect Dialect
	up      string
	down    string
}{
	{DialectMySQL, channelParentIDsMigration + ".sql", channelParentIDsMigration + "-down.sql"},
	{DialectPostgres, channelParentIDsMigration + "-postgresql.sql", channelParentIDsMigration + "-postgresql-down.sql"},
	{DialectSQLServer, channelParentIDsMigration + "-sqlserver.sql", channelParentIDsMigration + "-sqlserver-down.sql"},
	{DialectSQLite, channelParentIDsMigration + "-sqlite.sql", channelParentIDsMigration + "-sqlite-down.sql"},
}

func readChannelParentIDsMigration(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations", name))
	require.NoError(t, err, "迁移文件缺失: %s", name)
	return string(body)
}

func TestChannelParentIDsMigrationFilesCoverEveryDialect(t *testing.T) {
	for _, file := range channelParentIDsMigrationFiles {
		t.Run(string(file.dialect), func(t *testing.T) {
			up := strings.ToLower(readChannelParentIDsMigration(t, file.up))
			down := strings.ToLower(readChannelParentIDsMigration(t, file.down))
			require.Contains(t, up, "512", "up must preserve the full 2022 multi-parent value")
			require.Contains(t, down, "20", "down must restore the prior column limit")
		})
	}
}

func TestChannelParentIDsMigrationIsRegisteredForEveryDialect(t *testing.T) {
	for _, file := range channelParentIDsMigrationFiles {
		found := false
		names, err := (&embedSource{dialect: file.dialect}).UpFiles()
		require.NoError(t, err)
		for _, name := range names {
			if name == file.up {
				found = true
			}
		}
		require.Truef(t, found, "%s up migration must be selected by its dialect", file.up)
		_, err = (&embedSource{dialect: file.dialect}).ReadSQL(file.down)
		require.NoErrorf(t, err, "%s down migration must be embedded", file.down)
	}
}

func TestChannelParentIDsAre512CharactersInEveryFreshBaseline(t *testing.T) {
	for _, file := range []struct{ name, declaration string }{
		{"uvp-gb28181.sql", "`parent_id` varchar(512)"},
		{"postgresql_converted.sql", `"parent_id" varchar(512)`},
		{"sqlserver_converted.sql", "[parent_id] nvarchar(512)"},
		{"sqlitebaseline/baseline.sql", `length("parent_id") <= 512`},
	} {
		t.Run(file.name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database", file.name))
			require.NoError(t, err)
			require.Contains(t, strings.ToLower(string(body)), file.declaration,
				"fresh baseline must accept complete GB/T 28181-2022 multi-parent values")
		})
	}
}

func TestSQLiteChannelParentIDsMigrationPreservesDataAndAllowsSlashDelimitedParentIDs(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })

	baseline, err := os.ReadFile(filepath.Join("../../../resource/database/sqlitebaseline", "baseline.sql"))
	require.NoError(t, err)
	createTable := sqliteChannelTableDDL(t, string(baseline))
	createTable = strings.ReplaceAll(createTable, `length("parent_id") <= 512`, `length("parent_id") <= 20`)
	require.Contains(t, createTable, `length("parent_id") <= 20`, "fixture must represent the legacy SQLite baseline")
	require.NoError(t, db.Exec(createTable).Error)
	for _, statement := range []string{
		`CREATE UNIQUE INDEX "uk_device_channel" ON "gb_channel" ("device_id", "channel_id")`,
		`CREATE INDEX "idx_deleted_at" ON "gb_channel" ("deleted_at")`,
		`CREATE INDEX "idx_device_id" ON "gb_channel" ("device_id")`,
		`CREATE INDEX "idx_owner_dept_deleted" ON "gb_channel" ("owner_dept_id", "deleted_at")`,
		`CREATE INDEX "idx_stream_id" ON "gb_channel" ("stream_id")`,
		`CREATE INDEX "idx_stream_transport" ON "gb_channel" ("stream_transport")`,
	} {
		require.NoError(t, db.Exec(statement).Error)
	}

	const oldParentID = "34020000001320000001"
	const longParentID = "34020000001320000001/34020000001320000002"
	require.Len(t, oldParentID, 20)
	require.Len(t, longParentID, 41)
	require.NoError(t, db.Exec(`INSERT INTO gb_channel (id, channel_id, device_id, parent_id) VALUES (1, 'channel', 'device', ?)`, oldParentID).Error)

	probe := schemaProbe{
		hasTable: func(table string) (bool, error) { return db.Migrator().HasTable(table), nil },
		hasColumn: func(table, column string) (bool, error) {
			return db.Migrator().HasColumn(table, column), nil
		},
		columnLength: func(table, _ string) (int64, error) { return sqliteColumnLimit(db, table) },
	}
	needed, err := needsBaselineUpgrade(channelParentIDsMigrationFiles[3].up, probe)
	require.NoError(t, err)
	require.True(t, needed, "empty-ledger legacy SQLite baseline must run the width upgrade")

	require.NoError(t, (&dbExecutor{db: db}).ExecSQL(readChannelParentIDsMigration(t, channelParentIDsMigration+"-sqlite.sql")))
	needed, err = needsBaselineUpgrade(channelParentIDsMigrationFiles[3].up, probe)
	require.NoError(t, err)
	require.False(t, needed, "a 512-character SQLite baseline must not rebuild again")
	var got string
	require.NoError(t, db.Raw(`SELECT parent_id FROM gb_channel WHERE id = 1`).Scan(&got).Error)
	require.Equal(t, oldParentID, got, "migration must preserve existing values")
	require.NoError(t, db.Exec(`UPDATE gb_channel SET parent_id = ? WHERE id = 1`, longParentID).Error)
	require.NoError(t, db.Raw(`SELECT parent_id FROM gb_channel WHERE id = 1`).Scan(&got).Error)
	require.Equal(t, longParentID, got, "the complete slash-delimited standard value must be stored")
	require.Error(t, checkChannelParentIDsDown(db, DialectSQLite, channelParentIDsMigration+"-sqlite.sql"),
		"runner downgrade guard must reject values that would be truncated")

	err = (&dbExecutor{db: db}).ExecSQL(readChannelParentIDsMigration(t, channelParentIDsMigration+"-sqlite-down.sql"))
	require.Error(t, err, "down must refuse to narrow while a value would be lost")
	require.NoError(t, db.Raw(`SELECT parent_id FROM gb_channel WHERE id = 1`).Scan(&got).Error)
	require.Equal(t, longParentID, got, "failed down must leave original data and table intact")

	require.NoError(t, db.Exec(`UPDATE gb_channel SET parent_id = ? WHERE id = 1`, oldParentID).Error)
	require.NoError(t, checkChannelParentIDsDown(db, DialectSQLite, channelParentIDsMigration+"-sqlite.sql"))
	require.NoError(t, (&dbExecutor{db: db}).ExecSQL(readChannelParentIDsMigration(t, channelParentIDsMigration+"-sqlite-down.sql")))
	err = db.Exec(`UPDATE gb_channel SET parent_id = ? WHERE id = 1`, longParentID).Error
	require.Error(t, err, "successful down must restore the 20-character constraint")
}

func sqliteChannelTableDDL(t *testing.T, baseline string) string {
	t.Helper()
	const startMarker = `CREATE TABLE IF NOT EXISTS "gb_channel" (`
	start := strings.Index(baseline, startMarker)
	require.NotEqual(t, -1, start, "SQLite baseline gb_channel table must exist")
	rest := baseline[start:]
	end := strings.Index(rest, "\n);")
	require.NotEqual(t, -1, end, "SQLite baseline gb_channel definition must close")
	return rest[:end+3]
}
