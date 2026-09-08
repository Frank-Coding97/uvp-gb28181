package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	migrationsfs "uvplatform.cn/uvp-gb28181/resource/database/gb28181"
)

func TestProcessAuthorityMigrationAndInitialization(t *testing.T) {
	for _, target := range []struct{ suffix, baseline string }{
		{"", "uvp-gb28181.sql"}, {"-postgresql", "postgresql_converted.sql"}, {"-sqlserver", "sqlserver_converted.sql"},
	} {
		t.Run(target.baseline, func(t *testing.T) {
			name := "migrations/2026-09-08-openapi-process-authority" + target.suffix
			body, err := migrationsfs.FS.ReadFile(name + ".sql")
			require.NoError(t, err)
			up := strings.ToLower(string(body))
			for _, required := range []string{"sys_openapi_process_authority", "sys_openapi_process_generation", "domain_id", "current_generation_id", "generation_id", "started_at", "row_version", "ck_openapi_authority_singleton", "foreign key", "references", "uk_openapi_generation_domain"} {
				require.Contains(t, up, required)
			}
			for _, forbidden := range []string{"drop table", "cascade", "insert into", "update gb_device", "delete from"} {
				require.NotContains(t, up, forbidden)
			}
			down, err := migrationsfs.FS.ReadFile(name + "-down.sql")
			require.NoError(t, err)
			require.Contains(t, strings.ToLower(string(down)), "select 1")
			for _, forbidden := range []string{"drop ", "delete ", "truncate ", "update ", "alter "} {
				require.NotContains(t, strings.ToLower(string(down)), forbidden)
			}
			baseline, err := os.ReadFile(filepath.Join("..", "..", "..", "resource", "database", target.baseline))
			require.NoError(t, err)
			start, end := "-- openapi-process-authority:begin", "-- openapi-process-authority:end"
			text := string(baseline)
			a, b := strings.Index(text, start), strings.Index(text, end)
			require.GreaterOrEqual(t, a, 0)
			require.Greater(t, b, a)
			require.Equal(t, strings.TrimSpace(string(body)), strings.TrimSpace(text[a+len(start):b]))
		})
	}
}
