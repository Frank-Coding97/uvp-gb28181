package migration

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var cascadeOptionColumns = []string{"auto_push_catalog", "recording_enabled", "rtcp_keepalive"}

func cascadeOptionMigration(column string, dialect Dialect) string {
	suffix := map[Dialect]string{DialectMySQL: "", DialectPostgres: "-postgresql", DialectSQLServer: "-sqlserver", DialectSQLite: "-sqlite"}[dialect]
	return "2026-10-13-cascade-" + strings.ReplaceAll(column, "_", "-") + suffix + ".sql"
}

func TestCascadeOptionMigrationFilesCoverEveryDialectAndAreRegistered(t *testing.T) {
	for _, dialect := range []Dialect{DialectMySQL, DialectPostgres, DialectSQLServer, DialectSQLite} {
		for _, column := range cascadeOptionColumns {
			t.Run(string(dialect)+"/"+column, func(t *testing.T) {
				name := cascadeOptionMigration(column, dialect)
				body, err := (&embedSource{dialect: dialect}).ReadSQL(name)
				require.NoError(t, err)
				require.Contains(t, body, column)
				files, err := (&embedSource{dialect: dialect}).UpFiles()
				require.NoError(t, err)
				require.Contains(t, files, name)
				require.Equal(t, baselineColumnRequirement{table: "gb_cascade_platform", column: column}, baselineColumnMigrations[name])
				_, err = (&embedSource{dialect: dialect}).ReadSQL(strings.TrimSuffix(name, ".sql") + "-down.sql")
				require.NoError(t, err)
			})
		}
	}
}

func TestSQLiteCascadeOptionsUpgradePreservesLegacyPlatformAndRepeatsSafely(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.Exec("CREATE TABLE gb_cascade_platform (id INTEGER PRIMARY KEY, name TEXT, catalog_batch_size INTEGER NOT NULL DEFAULT 100, max_streams INTEGER NOT NULL DEFAULT 1)").Error)
	require.NoError(t, db.Exec("INSERT INTO gb_cascade_platform (id, name, catalog_batch_size, max_streams) VALUES (1, 'legacy', 100, 4)").Error)
	probe := schemaProbe{
		hasTable: func(table string) (bool, error) {
			if table == "gb_cascade_platform" {
				return db.Migrator().HasTable(table), nil
			}
			return true, nil
		},
		hasColumn: func(table, column string) (bool, error) { return db.Migrator().HasColumn(table, column), nil },
	}
	store, lock, source, _ := newFakes()
	for _, column := range cascadeOptionColumns {
		name := cascadeOptionMigration(column, DialectSQLite)
		source.files[name], err = (&embedSource{dialect: DialectSQLite}).ReadSQL(name)
		require.NoError(t, err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		// An empty ledger after SQL success must also skip already-existing columns.
		if attempt == 1 {
			store.applied = make(map[string]bool)
		}
		require.NoError(t, run(store, lock, source, &dbExecutor{db: db}, probe))
	}
	var row struct {
		Name             string
		CatalogBatchSize int
		MaxStreams       int
		AutoPushCatalog  bool
		RecordingEnabled bool
		RTCPKeepalive    bool
	}
	require.NoError(t, db.Table("gb_cascade_platform").First(&row).Error)
	require.Equal(t, "legacy", row.Name)
	require.Equal(t, 100, row.CatalogBatchSize)
	require.Equal(t, 4, row.MaxStreams)
	require.False(t, row.AutoPushCatalog)
	require.False(t, row.RecordingEnabled)
	require.False(t, row.RTCPKeepalive)
	for _, column := range cascadeOptionColumns {
		require.NoError(t, db.Exec(fmt.Sprintf("UPDATE gb_cascade_platform SET %s = 1 WHERE id = 1", column)).Error)
	}
	require.NoError(t, db.Table("gb_cascade_platform").First(&row).Error)
	require.True(t, row.AutoPushCatalog)
	require.True(t, row.RecordingEnabled)
	require.True(t, row.RTCPKeepalive)
}

func TestSQLiteCascadeFreshBaselineUsesSafeDefaults(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	body, err := os.ReadFile("../../../resource/database/sqlitebaseline/baseline.sql")
	require.NoError(t, err)
	const marker = `CREATE TABLE IF NOT EXISTS "gb_cascade_platform" (`
	start := strings.Index(string(body), marker)
	require.NotEqual(t, -1, start)
	section := string(body)[start:]
	end := strings.Index(section, "\n);")
	require.NotEqual(t, -1, end)
	require.NoError(t, db.Exec(section[:end+3]).Error)
	require.NoError(t, db.Exec(`INSERT INTO gb_cascade_platform
  (name, upstream_server_id, upstream_domain, host, port, local_device_id, local_domain, local_sip_ip, local_sip_port, created_at, updated_at)
  VALUES ('new-platform', '34020000002000000001', '3402000000', '192.0.2.1', 5060, '34020000002000000009', '3402000000', '192.0.2.2', 5060, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`).Error)
	var row struct {
		CatalogBatchSize int
		MaxStreams       int
		AutoPushCatalog  bool
		RecordingEnabled bool
		RTCPKeepalive    bool
	}
	require.NoError(t, db.Table("gb_cascade_platform").First(&row).Error)
	require.Equal(t, 1, row.CatalogBatchSize)
	require.Zero(t, row.MaxStreams)
	require.False(t, row.AutoPushCatalog)
	require.False(t, row.RecordingEnabled)
	require.False(t, row.RTCPKeepalive)
}
