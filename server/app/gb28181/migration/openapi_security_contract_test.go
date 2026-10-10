package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAPIMustAuthNewMigrationAndInitialization(t *testing.T) {
	for _, tc := range []struct{ suffix, init string }{{"", "uvp-gb28181.sql"}, {"-postgresql", "postgresql_converted.sql"}, {"-sqlserver", "sqlserver_converted.sql"}} {
		t.Run(tc.init, func(t *testing.T) {
			path := filepath.Join("../../../resource/database/gb28181/migrations", "2026-09-06-openapi-must-auth-lock"+tc.suffix+".sql")
			body, err := os.ReadFile(path)
			require.NoError(t, err)
			sql := strings.ToLower(string(body))
			require.Len(t, splitStatements(sql), 2, "create and insert are standalone runner statements")
			for _, part := range []string{"sys_openapi_security_state", "must_auth_locked", "locked_at", "lock_version", "check (id = 1)", "where not exists", "-- openapi-must-auth:begin", "-- openapi-must-auth:end"} {
				require.Contains(t, sql, part)
			}
			require.NotContains(t, sql, "update sys_openapi_security_state", "up/up must never unlock")
			init, err := os.ReadFile(filepath.Join("../../../resource/database", tc.init))
			require.NoError(t, err)
			start := strings.Index(strings.ToLower(string(init)), "-- openapi-must-auth:begin")
			require.GreaterOrEqual(t, start, 0)
			end := strings.Index(strings.ToLower(string(init)), "-- openapi-must-auth:end")
			require.Greater(t, end, start)
			require.Equal(t, strings.TrimSpace(sql[strings.Index(sql, "-- openapi-must-auth:begin"):strings.Index(sql, "-- openapi-must-auth:end")]), strings.TrimSpace(strings.ToLower(string(init))[start:end]))
		})
	}
}
