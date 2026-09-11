package models_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeviceZLMNodeDDLIsAvailableForEverySupportedDatabase(t *testing.T) {
	root := dualVersionDDLRoot(t)
	for _, migration := range []struct {
		name, up, down, guard string
	}{
		{"mysql", "2026-08-21-device-zlm-node.sql", "2026-08-21-device-zlm-node-down.sql", "information_schema.columns"},
		{"postgresql", "2026-08-21-device-zlm-node-postgresql.sql", "2026-08-21-device-zlm-node-postgresql-down.sql", "if not exists"},
		{"sqlserver", "2026-08-21-device-zlm-node-sqlserver.sql", "2026-08-21-device-zlm-node-sqlserver-down.sql", "col_length"},
	} {
		t.Run(migration.name, func(t *testing.T) {
			for _, file := range []string{migration.up, migration.down} {
				body, err := os.ReadFile(filepath.Join(root, "resource/database/gb28181/migrations", file))
				require.NoError(t, err)
				require.Contains(t, strings.ToLower(string(body)), "zlm_node_id")
			}
			body, err := os.ReadFile(filepath.Join(root, "resource/database/gb28181/migrations", migration.up))
			require.NoError(t, err)
			require.Contains(t, strings.ToLower(string(body)), migration.guard)
		})
	}

	for _, file := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		body, err := os.ReadFile(filepath.Join(root, "resource/database", file))
		require.NoError(t, err)
		text := strings.ToLower(string(body))
		require.Contains(t, text, "zlm_node_id", file)
		require.Contains(t, text, "idx_gb_device_zlm_node", file)
	}
}
