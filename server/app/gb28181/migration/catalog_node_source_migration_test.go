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

func TestCatalogNodeSourceBackfillIsScopedToLiveOwnedCatalogChannels(t *testing.T) {
	files := []struct {
		dialect string
		name    string
	}{
		{"mysql", "2026-10-12-catalog-node-source-device.sql"},
		{"postgresql", "2026-10-12-catalog-node-source-device-postgresql.sql"},
		{"sqlserver", "2026-10-12-catalog-node-source-device-sqlserver.sql"},
		{"sqlite", "2026-10-12-catalog-node-source-device-sqlite.sql"},
	}

	for _, file := range files {
		t.Run(file.dialect, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations", file.name))
			require.NoError(t, err)
			sql := strings.ToLower(string(body))
			sql = strings.NewReplacer("n'", "'", "`", "", "[", "", "]", "", `"`, "").Replace(sql)
			sql = strings.Join(strings.Fields(sql), " ")

			require.Contains(t, sql, "source_device_id = ''", "only unassigned nodes can be backfilled")
			require.Contains(t, sql, "device_id <> ''", "the channel must identify an upstream device")
			require.Contains(t, sql, "node_type = 'channel'", "only physical channel nodes can infer a source")
			require.Contains(t, sql, "source = 'catalog'", "manual and inferred nodes must not be assigned a catalog source")
			require.GreaterOrEqual(t, strings.Count(sql, "deleted_at is null"), 2,
				"both the catalog node and linked channel must be active")

			ownerMatchesCatalogChannel := strings.Contains(sql, "owner_dept_id = c.owner_dept_id") ||
				strings.Contains(sql, "c.owner_dept_id = gb_catalog_node.owner_dept_id")
			require.True(t, ownerMatchesCatalogChannel, "backfill must not cross owner departments")
		})
	}
}

func TestCatalogNodeSourceColumnAndIndexExistInFreshBaselines(t *testing.T) {
	for _, file := range []struct{ name, column string }{
		{"uvp-gb28181.sql", "`source_device_id` varchar(20) not null default ''"},
		{"postgresql_converted.sql", `"source_device_id" varchar(20) not null default ''`},
		{"sqlserver_converted.sql", `[source_device_id] nvarchar(20) not null constraint [df_gb_catalog_node_source_device_id] default n''`},
	} {
		t.Run(file.name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database", file.name))
			require.NoError(t, err)
			sql := strings.ToLower(string(body))
			sql = strings.Join(strings.Fields(sql), " ")
			require.Contains(t, sql, file.column, "fresh install must carry catalog source identity")
			require.Contains(t, sql, "idx_owner_source_type_code", "fresh install must carry the source-scoped lookup index")
		})
	}
}

func TestSQLiteCatalogNodeSourceBackfillOnlyUpdatesLiveOwnedCatalogChannels(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })

	for _, statement := range []string{
		"CREATE TABLE gb_catalog_node (id INTEGER PRIMARY KEY, owner_dept_id INTEGER NOT NULL, node_type TEXT NOT NULL, source TEXT NOT NULL, channel_id INTEGER, deleted_at TEXT)",
		"CREATE TABLE gb_channel (id INTEGER PRIMARY KEY, owner_dept_id INTEGER NOT NULL, device_id TEXT NOT NULL, deleted_at TEXT)",
		"INSERT INTO gb_channel(id, owner_dept_id, device_id, deleted_at) VALUES (1, 10, 'source-1', NULL), (2, 10, 'source-2', NULL), (3, 10, 'source-3', NULL), (4, 10, 'source-4', NULL), (5, 20, 'source-5', NULL), (6, 10, 'source-6', 'deleted')",
		"INSERT INTO gb_catalog_node(id, owner_dept_id, node_type, source, channel_id, deleted_at) VALUES (1, 10, 'channel', 'catalog', 1, NULL), (2, 10, 'channel', 'manual', 2, NULL), (3, 10, 'biz_group', 'catalog', 3, NULL), (4, 10, 'channel', 'catalog', 4, 'deleted'), (5, 10, 'channel', 'catalog', 5, NULL), (6, 10, 'channel', 'catalog', 6, NULL)",
	} {
		require.NoError(t, db.Exec(statement).Error)
	}

	body, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations", "2026-10-12-catalog-node-source-device-sqlite.sql"))
	require.NoError(t, err)
	for _, statement := range splitStatements(string(body)) {
		require.NoError(t, db.Exec(statement).Error, statement)
	}

	var rows []struct {
		ID             uint
		SourceDeviceID string
	}
	require.NoError(t, db.Table("gb_catalog_node").Select("id, source_device_id").Order("id").Scan(&rows).Error)
	require.Len(t, rows, 6)
	for _, row := range rows {
		want := ""
		if row.ID == 1 {
			want = "source-1"
		}
		require.Equal(t, want, row.SourceDeviceID, "node id=%d must keep or receive the expected source", row.ID)
	}
}
