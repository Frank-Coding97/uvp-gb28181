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
			for _, name := range []string{"sys_openapi_client", "sys_openapi_client_scope", "sys_openapi_nonce", "sys_openapi_audit"} {
				require.Contains(t, sql, name)
			}
			for _, column := range []string{"secret_ciphertext", "secret_iv", "secret_key_id", "secret_version", "auth_epoch", "scope_epoch", "row_version", "expires_at", "uk_openapi_nonce", "idx_openapi_audit_client_time"} {
				require.Contains(t, sql, column)
			}
			require.NotContains(t, sql, "secret_plaintext")
			require.NotContains(t, sql, "insert into sys_openapi_client")
			require.NotContains(t, sql, "update sys_department")
			down, err := os.ReadFile(filepath.Join("../../../resource/database/gb28181/migrations", stem+suffix+"-down.sql"))
			require.NoError(t, err)
			require.NotContains(t, strings.ToLower(string(down)), "drop database")
			require.NotContains(t, strings.ToLower(string(down)), "drop table gb_device")
		})
	}
}

func TestOpenAPICoreInitializationParity(t *testing.T) {
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database", name))
			require.NoError(t, err)
			for _, table := range []string{"sys_openapi_client", "sys_openapi_client_scope", "sys_openapi_nonce", "sys_openapi_audit"} {
				require.Contains(t, strings.ToLower(string(body)), table)
			}
		})
	}
}
