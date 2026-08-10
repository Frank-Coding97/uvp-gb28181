package models

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCascadePermissionsSeedsCoverAllDatabaseVariantsAndFreshInstalls(t *testing.T) {
	root := filepath.Join("..", "..", "..", "resource", "database")
	files := []string{
		filepath.Join(root, "gb28181", "migrations", "2026-08-10-gb-cascade-permissions.sql"),
		filepath.Join(root, "gb28181", "migrations", "2026-08-10-gb-cascade-permissions-postgresql.sql"),
		filepath.Join(root, "gb28181", "migrations", "2026-08-10-gb-cascade-permissions-sqlserver.sql"),
		filepath.Join(root, "uvp-gb28181.sql"),
		filepath.Join(root, "postgresql_converted.sql"),
		filepath.Join(root, "sqlserver_converted.sql"),
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		require.NoError(t, err, file)
		text := string(data)
		require.Contains(t, text, "gb28181:cascade:view", file)
		require.Contains(t, text, "gb28181:cascade:manage", file)
		require.Contains(t, text, "/api/gb28181/cascade/platforms", file)
		require.Contains(t, text, "sys_casbin_rule", file)
	}
}
