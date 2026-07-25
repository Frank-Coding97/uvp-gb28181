package models_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDualVersionMigrationContractsAreIdempotent(t *testing.T) {
	root := dualVersionDDLRoot(t)
	columns := []string{
		"reported_version", "reported_version_at", "protocol_override",
		"effective_version", "effective_version_source", "effective_version_at",
		"profile_version", "profile_charset", "target_scope", "target_code",
	}
	migrations := []struct {
		name   string
		path   string
		guards []string
	}{
		{name: "mysql", path: "resource/database/gb28181/migrations/2026-07-25-gb-dual-version.sql", guards: []string{"information_schema.columns", "information_schema.statistics"}},
		{name: "postgresql", path: "resource/database/gb28181/migrations/2026-07-25-gb-dual-version-postgresql.sql", guards: []string{"add column if not exists", "create table if not exists"}},
		{name: "sqlserver", path: "resource/database/gb28181/migrations/2026-07-25-gb-dual-version-sqlserver.sql", guards: []string{"col_length", "object_id", "sys.indexes"}},
	}
	for _, migration := range migrations {
		migration := migration
		t.Run(migration.name, func(t *testing.T) {
			sql, err := os.ReadFile(filepath.Join(root, migration.path))
			require.NoError(t, err)
			text := strings.ToLower(string(sql))
			for _, column := range columns {
				require.Contains(t, text, column, "migration must mention "+column)
			}
			require.Contains(t, text, "gb_device_control_state")
			require.Contains(t, text, "update")
			for _, guard := range migration.guards {
				require.Contains(t, text, guard)
			}
		})
	}
}

func TestDualVersionFullSchemasContainNewTablesAndColumns(t *testing.T) {
	root := dualVersionDDLRoot(t)
	paths := map[string][]string{
		"resource/database/gb28181/gb_device.sql":    {"reported_version", "protocol_override", "effective_version", "effective_version_source"},
		"resource/database/gb28181/gb_ptz.sql":       {"profile_version", "profile_charset", "target_scope", "target_code", "gb_device_control_state"},
		"resource/database/uvp-gb28181.sql":          {"reported_version", "protocol_override", "effective_version", "profile_version", "target_scope", "target_code", "gb_device_control_state"},
		"resource/database/postgresql_converted.sql": {"reported_version", "protocol_override", "effective_version", "profile_version", "target_scope", "target_code", "gb_device_control_state"},
		"resource/database/sqlserver_converted.sql":  {"reported_version", "protocol_override", "effective_version", "profile_version", "target_scope", "target_code", "gb_device_control_state"},
	}
	for path, tokens := range paths {
		tokens := tokens
		t.Run(path, func(t *testing.T) {
			sql, err := os.ReadFile(filepath.Join(root, path))
			require.NoError(t, err)
			text := strings.ToLower(string(sql))
			for _, token := range tokens {
				require.Contains(t, text, token)
			}
		})
	}
}

func dualVersionDDLRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}
