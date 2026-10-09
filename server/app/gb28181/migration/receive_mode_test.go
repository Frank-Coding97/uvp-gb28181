package migration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestReceiveModeSQLiteMigrationPreservesExistingNodes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE TABLE meta_node (id INTEGER PRIMARY KEY, name TEXT)").Error)
	require.NoError(t, db.Exec("INSERT INTO meta_node VALUES (1, 'legacy')").Error)
	{
		file := "2026-10-09-meta-node-receive-mode-sqlite.sql"
		body, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations", file))
		require.NoError(t, err)
		require.NoError(t, db.Exec(string(body)).Error)
		needed, err := needsBaselineColumnUpgrade(file, schemaProbe{
			hasTable:  func(string) (bool, error) { return true, nil },
			hasColumn: func(table, column string) (bool, error) { return db.Migrator().HasColumn(table, column), nil },
		})
		require.NoError(t, err)
		require.False(t, needed)
	}
	var row struct {
		RTPReceiveMode string
		RTPProxyPort   int
	}
	require.NoError(t, db.Table("meta_node").First(&row).Error)
	require.Equal(t, "multi", row.RTPReceiveMode)
	require.Equal(t, 10000, row.RTPProxyPort)
}
