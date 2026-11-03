package migration

import (
	"os"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelStreamMigrations(t *testing.T) {
	for _, dialect := range []Dialect{DialectMySQL, DialectPostgres, DialectSQLServer, DialectSQLite} {
		suffix := map[Dialect]string{DialectMySQL: "", DialectPostgres: "-postgresql", DialectSQLServer: "-sqlserver", DialectSQLite: "-sqlite"}[dialect]
		for name, column := range map[string]string{"identification": "stream_identification", "session": "current_stream_identification"} {
			file := "2026-11-02-channel-stream-" + name + suffix + ".sql"
			source := &embedSource{dialect: dialect}
			up, err := source.ReadSQL(file)
			require.NoError(t, err)
			require.Contains(t, up, column)
			down, err := source.ReadSQL(DownFileName(file))
			require.NoError(t, err)
			require.Contains(t, down, column)
			require.Equal(t, baselineColumnRequirement{table: "gb_channel", column: column}, baselineColumnMigrations[file])
		}
	}
}

func TestChannelStreamSQLiteUpgrade(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE gb_channel (id INTEGER PRIMARY KEY)`).Error)
	store, lock, source, _ := newFakes()
	for _, name := range []string{"identification", "session"} {
		file := "2026-11-02-channel-stream-" + name + "-sqlite.sql"
		source.files[file], err = (&embedSource{dialect: DialectSQLite}).ReadSQL(file)
		require.NoError(t, err)
	}
	probe := schemaProbe{hasTable: func(string) (bool, error) { return true, nil }, hasColumn: func(table, column string) (bool, error) { return db.Migrator().HasColumn(table, column), nil }}
	for range 2 {
		store.applied = make(map[string]bool)
		require.NoError(t, run(store, lock, source, &dbExecutor{db: db}, probe))
	}
	require.NoError(t, db.Exec(`INSERT INTO gb_channel (id) VALUES (1)`).Error)
	var row struct {
		StreamIdentification        string
		CurrentStreamIdentification *string
	}
	require.NoError(t, db.Table("gb_channel").Take(&row).Error)
	require.Equal(t, "", row.StreamIdentification)
	require.Nil(t, row.CurrentStreamIdentification)
	for _, name := range []string{"session", "identification"} {
		body, err := (&embedSource{dialect: DialectSQLite}).ReadSQL("2026-11-02-channel-stream-" + name + "-sqlite-down.sql")
		require.NoError(t, err)
		require.NoError(t, db.Exec(body).Error)
	}
	require.False(t, db.Migrator().HasColumn("gb_channel", "stream_identification"))
	require.False(t, db.Migrator().HasColumn("gb_channel", "current_stream_identification"))
}

func TestChannelStreamFreshBaselines(t *testing.T) {
	for _, file := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql", "sqlitebaseline/baseline.sql"} {
		body, err := os.ReadFile("../../../resource/database/" + file)
		require.NoError(t, err)
		require.Contains(t, string(body), "stream_identification")
		require.Contains(t, string(body), "current_stream_identification")
		if file != "sqlitebaseline/baseline.sql" {
			continue
		}
		start := strings.Index(string(body), `CREATE TABLE IF NOT EXISTS "gb_channel" (`)
		require.NotEqual(t, -1, start)
		section := string(body)[start:]
		end := strings.Index(section, "\n);")
		require.NotEqual(t, -1, end)
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, db.Exec(section[:end+3]).Error)
		require.True(t, db.Migrator().HasColumn("gb_channel", "stream_identification"))
		require.True(t, db.Migrator().HasColumn("gb_channel", "current_stream_identification"))
	}
}
