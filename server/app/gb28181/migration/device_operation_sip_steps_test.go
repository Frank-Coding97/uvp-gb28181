package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	migrationsfs "uvplatform.cn/uvp-gb28181/resource/database/gb28181"
)

func TestDeviceOperationSIPStepsMigrationPreservesUnknownHistory(t *testing.T) {
	for _, target := range []struct{ suffix, baseline string }{
		{"", "uvp-gb28181.sql"}, {"-postgresql", "postgresql_converted.sql"}, {"-sqlserver", "sqlserver_converted.sql"},
	} {
		t.Run(target.baseline, func(t *testing.T) {
			stem := "migrations/2026-09-07-device-operation-sip-steps" + target.suffix
			body, err := migrationsfs.FS.ReadFile(stem + ".sql")
			require.NoError(t, err)
			up := strings.ToLower(string(body))
			if target.suffix == "-postgresql" {
				statements := splitStatements(string(body))
				require.Len(t, statements, 2, "the existing line-delimited runner must not split a DO block")
				require.True(t, strings.HasPrefix(statements[1], "DO $$"))
				require.True(t, strings.HasSuffix(statements[1], "END $$;"))
			}
			for _, word := range []string{"gb_device_operation_intent", "sip_steps_json", "null", "ck_device_intent_sip_size", "32768"} {
				require.Contains(t, up, word)
			}
			for _, forbidden := range []string{"drop ", "delete ", "truncate ", "update ", "insert into", "default "} {
				require.NotContains(t, up, forbidden)
			}
			down, err := migrationsfs.FS.ReadFile(stem + "-down.sql")
			require.NoError(t, err)
			require.Contains(t, string(down), "SELECT 1")
			for _, forbidden := range []string{"drop ", "delete ", "truncate ", "update ", "alter "} {
				require.NotContains(t, strings.ToLower(string(down)), forbidden)
			}
			baseline, err := os.ReadFile(filepath.Join("..", "..", "..", "resource", "database", target.baseline))
			require.NoError(t, err)
			text := string(baseline)
			begin, end := "-- device-operation-sip-steps:begin", "-- device-operation-sip-steps:end"
			a, b := strings.Index(text, begin), strings.Index(text, end)
			require.GreaterOrEqual(t, a, 0)
			require.Greater(t, b, a)
			require.Equal(t, strings.TrimSpace(string(body)), strings.TrimSpace(text[a+len(begin):b]))
			require.Greater(t, a, strings.Index(text, "-- device-operation-intent:end"))
		})
	}
}
