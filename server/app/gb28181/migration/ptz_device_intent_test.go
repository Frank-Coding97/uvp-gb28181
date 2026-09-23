package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	migrationsfs "uvplatform.cn/uvp-gb28181/resource/database/gb28181"
)

func TestPTZDeviceIntentMigrationPreservesUnknownHistory(t *testing.T) {
	for _, target := range []struct{ suffix, baseline string }{
		{"", "uvp-gb28181.sql"}, {"-postgresql", "postgresql_converted.sql"}, {"-sqlserver", "sqlserver_converted.sql"},
	} {
		t.Run(target.baseline, func(t *testing.T) {
			name := "migrations/2026-09-08-ptz-device-intent" + target.suffix
			body, err := migrationsfs.FS.ReadFile(name + ".sql")
			require.NoError(t, err)
			up := strings.ToLower(string(body))
			for _, field := range []string{"gb_ptz_operation", "device_epoch", "device_intent_id", "owner_process_id", "owner_run_id", "local_quiesced_at", "unique", "uk_ptz_device_intent"} {
				require.Contains(t, up, field)
			}
			if target.suffix == "-sqlserver" {
				require.Contains(t, up, "where device_intent_id is not null", "historical NULL bindings must coexist")
			}
			for _, forbidden := range []string{"update gb_ptz", "update `gb_ptz", "delete from", "drop table", "default 1", "default '"} {
				require.NotContains(t, up, forbidden, "old authority must not be fabricated")
			}
			down, err := migrationsfs.FS.ReadFile(name + "-down.sql")
			require.NoError(t, err)
			require.Contains(t, strings.ToLower(string(down)), "select 1")
			for _, forbidden := range []string{"drop ", "delete ", "truncate ", "update ", "alter "} {
				require.NotContains(t, strings.ToLower(string(down)), forbidden)
			}
			baseline, err := os.ReadFile(filepath.Join("..", "..", "..", "resource", "database", target.baseline))
			require.NoError(t, err)
			text := string(baseline)
			start, end := "-- ptz-device-intent:begin", "-- ptz-device-intent:end"
			a, b := strings.Index(text, start), strings.Index(text, end)
			require.GreaterOrEqual(t, a, 0)
			require.Greater(t, b, a)
			require.Equal(t, strings.TrimSpace(string(body)), strings.TrimSpace(text[a+len(start):b]))
		})
	}
}
