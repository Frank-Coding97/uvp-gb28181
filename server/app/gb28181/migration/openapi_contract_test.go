package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAPICoreSchemaMigrations(t *testing.T) {
	const stem = "2026-09-05-openapi-aksk-schema"
	for _, suffix := range []string{"", "-postgresql", "-sqlserver"} {
		t.Run(suffix, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations", stem+suffix+".sql"))
			require.NoError(t, err)
			sql := strings.ToLower(string(body))
			require.Contains(t, sql, "-- openapi-aksk-media:begin")
			require.Contains(t, sql, "-- openapi-aksk-media:end")
			mediaStart := strings.Index(sql, "-- openapi-aksk-media:begin")
			mediaEnd := strings.Index(sql, "-- openapi-aksk-media:end")
			require.Greater(t, mediaEnd, mediaStart)
			require.NotContains(t, sql[mediaStart:mediaEnd], "tags_json")
			for _, name := range []string{"sys_openapi_client", "sys_openapi_client_scope", "sys_openapi_nonce", "sys_openapi_audit", "gb_openapi_play_grant", "gb_openapi_viewer"} {
				require.Contains(t, sql, name)
			}
			for _, column := range []string{"secret_ciphertext", "secret_iv", "secret_key_id", "secret_version", "auth_epoch", "scope_epoch", "row_version", "expires_at", "uk_openapi_nonce", "idx_openapi_audit_client_time", "grant_id", "client_epoch", "scope_epoch", "device_epoch", "node_uuid", "boot_nonce", "media_generation", "runtime_epoch", "runtime_protocol_version", "runtime_confirmed_revision", "runtime_confirmed_at", "runtime_identity_status", "access_epoch", "legacy_revoked_before"} {
				require.Contains(t, sql, column)
			}
			require.Contains(t, sql, "fk_openapi_viewer_grant")
			require.Contains(t, sql, "on delete")
			require.Contains(t, sql, "media_generation > 0")
			require.Contains(t, sql, "device_id <>")
			require.Contains(t, sql, "stream <>")
			require.Contains(t, sql, "gb_device")
			require.Contains(t, sql, "meta_node")
			require.NotContains(t, sql, "secret_plaintext")
			require.NotContains(t, sql, "insert into sys_openapi_client")
			require.NotContains(t, sql, "update sys_department")
			down, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations", stem+suffix+"-down.sql"))
			require.NoError(t, err)
			downSQL := strings.ToLower(string(down))
			require.NotContains(t, downSQL, "drop database")
			require.NotContains(t, downSQL, "drop table gb_device")
			require.NotContains(t, downSQL, "drop table meta_node")
			require.Contains(t, downSQL, "isolated empty test databases only")
			require.Less(t, strings.Index(downSQL, "gb_openapi_viewer"), strings.Index(downSQL, "gb_openapi_play_grant"), "viewer must be dropped before its grant table")
		})
	}
}

func TestOpenAPICoreInitializationParity(t *testing.T) {
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database", name))
			require.NoError(t, err)
			lower := strings.ToLower(string(body))
			require.Contains(t, lower, "-- openapi-aksk-media:begin")
			require.Contains(t, lower, "-- openapi-aksk-media:end")
			for _, table := range []string{"sys_openapi_client", "sys_openapi_client_scope", "sys_openapi_nonce", "sys_openapi_audit", "gb_openapi_play_grant", "gb_openapi_viewer", "fk_openapi_viewer_grant"} {
				require.Contains(t, lower, table)
			}
		})
	}
}

func TestOpenAPIDownStatementsRespectRunnerBoundaries(t *testing.T) {
	const dir = "../../../resource/database/gb28181/migrations"

	mysqlDown, err := os.ReadFile(filepath.Join(dir, "2026-09-05-openapi-aksk-schema-down.sql"))
	require.NoError(t, err)
	prepareCount, executeCount, deallocateCount := 0, 0, 0
	for _, statement := range splitStatements(string(mysqlDown)) {
		normalized := strings.ToLower(strings.TrimSpace(statement))
		switch {
		case strings.HasPrefix(normalized, "prepare openapi_stmt from"):
			prepareCount++
			require.NotContains(t, normalized, "execute openapi_stmt")
		case strings.HasPrefix(normalized, "execute openapi_stmt"):
			executeCount++
			require.NotContains(t, normalized, "deallocate prepare")
		case strings.HasPrefix(normalized, "deallocate prepare openapi_stmt"):
			deallocateCount++
		}
	}
	require.Positive(t, prepareCount)
	require.Equal(t, prepareCount, executeCount)
	require.Equal(t, prepareCount, deallocateCount)

	sqlServerDown, err := os.ReadFile(filepath.Join(dir, "2026-09-05-openapi-aksk-schema-sqlserver-down.sql"))
	require.NoError(t, err)
	statements := splitStatements(string(sqlServerDown))
	require.NotEmpty(t, statements)
	for _, statement := range statements {
		normalized := strings.ToLower(strings.TrimSpace(statement))
		require.NotContains(t, normalized, "begin")
		require.NotContains(t, normalized, "end")
		if strings.HasPrefix(normalized, "if ") && (strings.Contains(normalized, "drop column") || strings.Contains(normalized, "drop constraint")) {
			require.Contains(t, normalized, "alter table")
		}
	}
}
