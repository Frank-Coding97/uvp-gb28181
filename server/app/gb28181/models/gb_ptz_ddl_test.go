package models_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var ptzTableNames = []string{
	"gb_ptz_operation",
	"gb_ptz_state",
	"gb_ptz_preset",
	"gb_ptz_cruise_track",
	"gb_ptz_operation_attempt",
	"gb_ptz_home_position",
}

var operationResponseColumns = []string{
	"response_required",
	"max_attempts",
	"queue_deadline_at",
	"dispatch_started_at",
	"transport_deadline_at",
	"deadline_at",
	"next_attempt_at",
	"response_call_id",
	"response_cseq",
	"response_at",
	"response_has_data",
	"trigger_operation_id",
	"reconcile_operation_id",
}

var ptzIndexNames = []string{
	"uk_ptz_operation_id",
	"uk_ptz_operation_idempotency",
	"idx_ptz_operation_channel_time",
	"idx_ptz_operation_channel_cmd_id",
	"idx_ptz_operation_device_sn",
	"idx_ptz_operation_status_time",
	"idx_ptz_operation_status_next_attempt",
	"idx_ptz_operation_status_queue_deadline",
	"idx_ptz_operation_status_transport_deadline",
	"idx_ptz_operation_status_deadline",
	"idx_ptz_operation_call_id",
	"uk_ptz_state_channel",
	"idx_ptz_state_device",
	"idx_ptz_state_received",
	"uk_ptz_preset_channel_number",
	"idx_ptz_preset_device",
	"uk_ptz_cruise_channel_track",
	"idx_ptz_cruise_device",
	"uk_ptz_operation_attempt",
	"idx_ptz_attempt_status_lease",
	"uk_ptz_home_position_channel",
	"idx_ptz_home_position_device",
}

func TestHomePositionDDLContract(t *testing.T) {
	root := ptzDDLServerRoot(t)
	t.Run("legacy_2026_07_19_fixture", func(t *testing.T) {
		sql := readSQL(t, root, "resource/database/gb28181/migrations/2026-07-19-gb-ptz.sql")
		normalized := normalizeSQL(sql)
		for _, table := range ptzTableNames[:4] {
			require.Equal(t, 1, createTableCount(normalized, table), table)
		}
		for _, table := range ptzTableNames[4:] {
			require.Zero(t, createTableCount(normalized, table), table)
		}
		operation := tableDefinition(t, normalized, "gb_ptz_operation")
		for _, column := range operationResponseColumns {
			require.Empty(t, columnClauseOptional(operation, column), column)
		}
		state := tableDefinition(t, normalized, "gb_ptz_state")
		for _, column := range []string{"home_enabled", "home_pan", "home_tilt", "home_zoom"} {
			require.NotEmpty(t, columnClause(t, state, column))
		}
	})

	fullScripts := []struct {
		name string
		path string
	}{
		{name: "mysql_module", path: "resource/database/gb28181/gb_ptz.sql"},
		{name: "mysql_full", path: "resource/database/uvp-gb28181.sql"},
		{name: "postgresql_full", path: "resource/database/postgresql_converted.sql"},
		{name: "sqlserver_full", path: "resource/database/sqlserver_converted.sql"},
	}
	for _, script := range fullScripts {
		script := script
		t.Run(script.name, func(t *testing.T) {
			sql := readSQL(t, root, script.path)
			requireFullPTZSchema(t, sql)
		})
	}

	migrations := []struct {
		name              string
		path              string
		idempotencyTokens []string
	}{
		{
			name: "mysql_migration", path: "resource/database/gb28181/migrations/2026-07-24-gb-home-position.sql",
			idempotencyTokens: []string{"information_schema.columns", "information_schema.statistics", "create table if not exists"},
		},
		{
			name: "postgresql_migration", path: "resource/database/gb28181/migrations/2026-07-24-gb-home-position-postgresql.sql",
			idempotencyTokens: []string{"add column if not exists", "create index if not exists", "create table if not exists"},
		},
		{
			name: "sqlserver_migration", path: "resource/database/gb28181/migrations/2026-07-24-gb-home-position-sqlserver.sql",
			idempotencyTokens: []string{"col_length", "sys.indexes", "object_id"},
		},
	}
	for _, migration := range migrations {
		migration := migration
		t.Run(migration.name, func(t *testing.T) {
			sql := readSQL(t, root, migration.path)
			normalized := normalizeSQL(sql)
			requireFullPTZSchema(t, sql)
			for _, token := range migration.idempotencyTokens {
				require.Contains(t, normalized, token)
			}
			requireMigrationGuards(t, migration.name, normalized)
			requireLegacyPendingConvergence(t, normalized)
			require.NotRegexp(t, regexp.MustCompile(`(?i)drop\s+column\s+[^,;]*(home_enabled|home_pan|home_tilt|home_zoom)`), sql)
			require.NotRegexp(t, regexp.MustCompile(`(?is)insert\s+into\s+[^\s(]*gb_ptz_home_position[^;]*select[^;]*gb_ptz_state`), sql)
		})
	}
}

func requireMigrationGuards(t *testing.T, dialect, sql string) {
	t.Helper()
	for _, column := range operationResponseColumns {
		switch dialect {
		case "mysql_migration":
			require.Contains(t, sql, "column_name = '"+column+"'", column)
		case "postgresql_migration":
			require.Contains(t, sql, "add column if not exists "+column, column)
		case "sqlserver_migration":
			require.Contains(t, sql, "col_length(n'gb_ptz_operation', n'"+column+"')", column)
		}
	}

	switch dialect {
	case "mysql_migration":
		for _, index := range []string{
			"idx_ptz_operation_channel_cmd_id",
			"idx_ptz_operation_status_next_attempt",
			"idx_ptz_operation_status_queue_deadline",
			"idx_ptz_operation_status_transport_deadline",
			"idx_ptz_operation_status_deadline",
		} {
			require.Contains(t, sql, "index_name = '"+index+"'", index)
		}
	case "postgresql_migration":
		for _, index := range ptzIndexNames {
			require.Regexp(t, regexp.MustCompile(`create (unique )?index if not exists `+regexp.QuoteMeta(index)+`\b`), sql, index)
		}
	case "sqlserver_migration":
		for _, index := range ptzIndexNames {
			require.Contains(t, sql, "name = n'"+index+"'", index)
		}
	}
}

func ptzDDLServerRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func readSQL(t *testing.T, root, relativePath string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, relativePath))
	require.NoError(t, err, relativePath)
	return string(body)
}

func requireFullPTZSchema(t *testing.T, sql string) {
	t.Helper()
	normalized := normalizeSQL(sql)
	definitions := make(map[string]string, len(ptzTableNames))
	for _, table := range ptzTableNames {
		require.Equal(t, 1, createTableCount(normalized, table), "duplicate or missing CREATE TABLE for %s", table)
		definitions[table] = tableDefinition(t, normalized, table)
	}

	operation := definitions["gb_ptz_operation"]
	for _, column := range operationResponseColumns {
		require.NotEmpty(t, columnClause(t, operation, column), "missing operation column %s", column)
	}
	requireColumnDefault(t, operation, "response_required", "false", "0")
	requireColumnDefault(t, operation, "max_attempts", "1")
	for _, column := range operationResponseColumns[2:] {
		require.NotContains(t, columnClause(t, operation, column), "not null", "%s must remain nullable", column)
	}

	state := definitions["gb_ptz_state"]
	for _, legacyColumn := range []string{"home_enabled", "home_pan", "home_tilt", "home_zoom"} {
		require.Empty(t, columnClauseOptional(state, legacyColumn), "new full schema must not declare %s", legacyColumn)
	}

	home := definitions["gb_ptz_home_position"]
	for _, column := range []string{
		"device_id", "channel_id", "channel_code", "enabled", "reset_time", "preset_id",
		"enabled_encoding", "confirmed_at", "source", "verification", "source_sn",
		"source_operation_id", "source_operation_seq", "raw_summary", "created_at", "updated_at",
	} {
		require.NotEmpty(t, columnClause(t, home, column))
	}
	require.Contains(t, columnClause(t, home, "enabled"), "not null")
	require.NotContains(t, columnClause(t, home, "reset_time"), "not null")
	require.NotContains(t, columnClause(t, home, "preset_id"), "not null")

	attempt := definitions["gb_ptz_operation_attempt"]
	for _, column := range []string{
		"operation_id", "attempt_no", "sn", "status", "call_id", "cseq", "sip_status",
		"started_at", "lease_until", "sent_at", "completed_at", "error_code", "error_message", "created_at",
	} {
		require.NotEmpty(t, columnClause(t, attempt, column))
	}

	for _, index := range ptzIndexNames {
		require.Contains(t, normalized, index, "missing index %s", index)
	}
}

func requireLegacyPendingConvergence(t *testing.T, sql string) {
	t.Helper()
	for _, token := range []string{
		"update gb_ptz_operation",
		"status = 'unknown'",
		"error_code = 'transport_unknown'",
		"completed_at = coalesce(completed_at,",
		"action in ('home_position', 'refresh_home_position')",
		"status in ('queued', 'sent')",
		"completed_at is null",
	} {
		require.Contains(t, sql, token)
	}
	require.Regexp(t, regexp.MustCompile(`response_required\s*=\s*(false|0)`), sql)
}

func requireColumnDefault(t *testing.T, definition, column string, allowed ...string) {
	t.Helper()
	clause := columnClause(t, definition, column)
	require.Contains(t, clause, "not null")
	for _, value := range allowed {
		if strings.Contains(clause, "default "+value) {
			return
		}
	}
	require.Failf(t, "missing default", "%s does not contain an allowed default: %s", column, clause)
}

func tableDefinition(t *testing.T, sql, table string) string {
	t.Helper()
	for _, prefix := range []string{"create table if not exists " + table, "create table " + table} {
		start := strings.Index(sql, prefix)
		if start < 0 {
			continue
		}
		openOffset := strings.Index(sql[start:], "(")
		require.NotEqual(t, -1, openOffset, table)
		open := start + openOffset
		depth := 0
		for i := open; i < len(sql); i++ {
			switch sql[i] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					return sql[open+1 : i]
				}
			}
		}
	}
	require.Failf(t, "missing table", "missing CREATE TABLE for %s", table)
	return ""
}

func createTableCount(sql, table string) int {
	return strings.Count(sql, "create table "+table+" (") +
		strings.Count(sql, "create table if not exists "+table+" (")
}

func columnClause(t *testing.T, definition, column string) string {
	t.Helper()
	clause := columnClauseOptional(definition, column)
	require.NotEmpty(t, clause, "missing column %s", column)
	return clause
}

func columnClauseOptional(definition, column string) string {
	for _, clause := range splitTopLevel(definition) {
		fields := strings.Fields(strings.TrimSpace(clause))
		if len(fields) > 0 && fields[0] == column {
			return strings.TrimSpace(clause)
		}
	}
	return ""
}

func splitTopLevel(value string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, value[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, value[start:])
}

func normalizeSQL(sql string) string {
	sql = strings.NewReplacer("`", "", "[", "", "]", "").Replace(strings.ToLower(sql))
	return strings.Join(strings.Fields(sql), " ")
}
