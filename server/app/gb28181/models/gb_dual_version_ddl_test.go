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
		"profile_version", "profile_charset", "target_scope", "target_code", "scope_key", "last_operation_id",
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
			require.Contains(t, text, "source_operation_seq")
			require.Contains(t, text, "update")
			for _, guard := range migration.guards {
				require.Contains(t, text, guard)
			}
			require.Contains(t, text, "gb_ptz_state", "migration must upgrade existing PTZ state rows")
			require.Contains(t, text, "idx_ptz_operation_target")
		})
	}
	sqlServerMigration := mustReadAlarmDDL(t, root, "resource/database/gb28181/migrations/2026-07-25-gb-dual-version-sqlserver.sql")
	require.Contains(t, sqlServerMigration, "uk_full_control_state_target", "SQL Server migration must remove the legacy global target key")
	require.Contains(t, sqlServerMigration, "drop constraint uk_full_control_state_target", "legacy SQL Server key must be dropped before device-scoped uniqueness")
	require.Contains(t, sqlServerMigration, "drop index uk_control_state_target on gb_device_control_state", "legacy ORM unique index must be replaced by the device-scoped key")
	for _, legacyIndex := range []string{
		"drop index idx_full_control_state_device_target on gb_device_control_state",
		"drop index idx_full_control_state_channel on gb_device_control_state",
		"drop index idx_full_ptz_operation_target on gb_ptz_operation",
	} {
		require.Contains(t, sqlServerMigration, legacyIndex, "SQL Server migration must remove the historical duplicate index")
	}

	stateDeviceCodeGuards := map[string]string{
		"mysql":      "table_name = 'gb_ptz_state' and column_name = 'device_code'",
		"postgresql": "alter table gb_ptz_state add column if not exists device_code",
		"sqlserver":  "col_length(n'gb_ptz_state', n'device_code')",
	}
	for name, expected := range stateDeviceCodeGuards {
		var path string
		for _, migration := range migrations {
			if migration.name == name {
				path = migration.path
				break
			}
		}
		sql, err := os.ReadFile(filepath.Join(root, path))
		require.NoError(t, err)
		require.Contains(t, normalizeAlarmDDL(string(sql)), expected)
	}

	cruiseOperationGuards := map[string][]string{
		"mysql": {
			"table_name = 'gb_ptz_cruise_track'",
			"column_name = 'last_operation_id'",
		},
		"postgresql": {
			"alter table if exists gb_ptz_cruise_track add column if not exists last_operation_id",
		},
		"sqlserver": {
			"object_id(n'gb_ptz_cruise_track', n'u') is not null",
			"col_length(n'gb_ptz_cruise_track', n'last_operation_id') is null",
		},
	}
	for name, expectedGuards := range cruiseOperationGuards {
		var path string
		for _, migration := range migrations {
			if migration.name == name {
				path = migration.path
				break
			}
		}
		text := mustReadAlarmDDL(t, root, path)
		for _, expected := range expectedGuards {
			require.Contains(t, text, expected)
		}
	}

	uniqueKeys := map[string]string{
		"mysql":      "`uk_control_state_target` (`device_id`, `target_scope`, `target_code`)",
		"postgresql": "unique (device_id, target_scope, target_code)",
		"sqlserver":  "unique ([device_id], [target_scope], [target_code])",
	}
	for name, expected := range uniqueKeys {
		var path string
		for _, migration := range migrations {
			if migration.name == name {
				path = migration.path
				break
			}
		}
		sql, err := os.ReadFile(filepath.Join(root, path))
		require.NoError(t, err)
		require.Contains(t, strings.ToLower(string(sql)), expected)
	}
}

func TestDualVersionFullSchemasContainNewTablesAndColumns(t *testing.T) {
	root := dualVersionDDLRoot(t)
	paths := map[string][]string{
		"resource/database/gb28181/gb_device.sql":    {"reported_version", "protocol_override", "effective_version", "effective_version_source"},
		"resource/database/gb28181/gb_ptz.sql":       {"profile_version", "profile_charset", "target_scope", "target_code", "scope_key", "last_operation_id", "gb_device_control_state"},
		"resource/database/uvp-gb28181.sql":          {"reported_version", "protocol_override", "effective_version", "profile_version", "target_scope", "target_code", "scope_key", "last_operation_id", "gb_device_control_state"},
		"resource/database/postgresql_converted.sql": {"reported_version", "protocol_override", "effective_version", "profile_version", "target_scope", "target_code", "scope_key", "last_operation_id", "gb_device_control_state"},
		"resource/database/sqlserver_converted.sql":  {"reported_version", "protocol_override", "effective_version", "profile_version", "target_scope", "target_code", "scope_key", "last_operation_id", "gb_device_control_state"},
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

func TestDualVersionFullSchemasCreateDeviceBaseline(t *testing.T) {
	root := dualVersionDDLRoot(t)
	paths := []string{
		"resource/database/uvp-gb28181.sql",
		"resource/database/postgresql_converted.sql",
		"resource/database/sqlserver_converted.sql",
	}
	for _, path := range paths {
		path := path
		t.Run(path, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, path))
			require.NoError(t, err)
			text := normalizeAlarmDDL(string(body))
			deviceTable := ddlTableSection(text, "gb_device")
			require.Contains(t, text, "create table gb_device (")
			require.NotContains(t, text, "alter table gb_device", "全量脚本不能依赖不存在的设备表")
			for _, column := range []string{
				"device_id", "reported_version", "reported_version_at", "protocol_override",
				"effective_version", "effective_version_source", "effective_version_at",
			} {
				require.Contains(t, deviceTable, column, "设备基线表必须包含 "+column)
			}
		})
	}
}

func TestDualVersionCruiseTrackOperationLinkIsInBaselines(t *testing.T) {
	root := dualVersionDDLRoot(t)
	paths := []string{
		"resource/database/gb28181/gb_ptz.sql",
		"resource/database/uvp-gb28181.sql",
		"resource/database/postgresql_converted.sql",
		"resource/database/sqlserver_converted.sql",
	}
	for _, path := range paths {
		path := path
		t.Run(path, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, path))
			require.NoError(t, err)
			cruiseTable := ddlTableSection(normalizeAlarmDDL(string(body)), "gb_ptz_cruise_track")
			require.Contains(t, cruiseTable, "last_operation_id")
		})
	}
}

func TestDualVersionFullSchemasIsolateControlStatePerDevice(t *testing.T) {
	root := dualVersionDDLRoot(t)
	paths := map[string]string{
		"resource/database/gb28181/gb_ptz.sql":       "`uk_control_state_target` (`device_id`, `target_scope`, `target_code`)",
		"resource/database/uvp-gb28181.sql":          "`uk_control_state_target` (`device_id`, `target_scope`, `target_code`)",
		"resource/database/postgresql_converted.sql": "unique (device_id, target_scope, target_code)",
		"resource/database/sqlserver_converted.sql":  "unique ([device_id], [target_scope], [target_code])",
	}
	for path, uniqueKey := range paths {
		t.Run(path, func(t *testing.T) {
			sql, err := os.ReadFile(filepath.Join(root, path))
			require.NoError(t, err)
			text := strings.ToLower(string(sql))
			require.Contains(t, text, "source_operation_seq")
			require.Contains(t, text, uniqueKey)
		})
	}
}

func TestDualVersionFullSchemasKeepPTZStateDeviceIdentityAndTargetIndex(t *testing.T) {
	root := dualVersionDDLRoot(t)
	paths := map[string]string{
		"resource/database/gb28181/gb_ptz.sql":       "device_id bigint unsigned not null, device_code varchar(20) not null, channel_id",
		"resource/database/uvp-gb28181.sql":          "device_id bigint unsigned not null, device_code varchar(20) not null, channel_id",
		"resource/database/postgresql_converted.sql": "device_id bigint not null, device_code varchar(20) not null, channel_id",
		"resource/database/sqlserver_converted.sql":  "device_id bigint not null, device_code nvarchar(20) not null, channel_id",
	}
	for path, stateColumns := range paths {
		t.Run(path, func(t *testing.T) {
			sql, err := os.ReadFile(filepath.Join(root, path))
			require.NoError(t, err)
			text := normalizeAlarmDDL(string(sql))
			require.Contains(t, ddlTableSection(text, "gb_ptz_state"), stateColumns)
			require.Contains(t, text, "idx_ptz_operation_target")
		})
	}
}

func ddlTableSection(text, table string) string {
	start := strings.Index(text, table+" (")
	if start < 0 {
		return ""
	}
	section := text[start:]
	if end := strings.Index(section, " create table "); end >= 0 {
		return section[:end]
	}
	return section
}

func dualVersionDDLRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}
