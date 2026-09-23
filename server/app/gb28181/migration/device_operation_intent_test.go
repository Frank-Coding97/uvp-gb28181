package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	migrationsfs "uvplatform.cn/uvp-gb28181/resource/database/gb28181"
)

func TestDeviceOperationIntentMigrationAndInitialization(t *testing.T) {
	for _, target := range []struct{ suffix, baseline string }{
		{"", "uvp-gb28181.sql"}, {"-postgresql", "postgresql_converted.sql"}, {"-sqlserver", "sqlserver_converted.sql"},
	} {
		t.Run(target.baseline, func(t *testing.T) {
			name := "migrations/2026-09-07-device-operation-intent" + target.suffix
			body, err := migrationsfs.FS.ReadFile(name + ".sql")
			require.NoError(t, err)
			up := strings.ToLower(string(body))
			for _, required := range []string{"gb_device_operation_intent", "contract_version", "target_scope", "target_pk", "target_code", "device_pk", "device_code", "device_epoch", "dispatch_started_at", "cancelled_at", "row_version", "reserved", "dispatched", "cancelled", "ck_device_intent_phase", "ix_device_intent_recovery"} {
				require.Contains(t, up, required)
			}
			for _, forbidden := range []string{"drop table", "cascade", "insert into", "update gb_device", "default 'dispatched'", "coverage"} {
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
			start, end := "-- device-operation-intent:begin", "-- device-operation-intent:end"
			text := string(baseline)
			a, b := strings.Index(text, start), strings.Index(text, end)
			require.GreaterOrEqual(t, a, 0)
			require.Greater(t, b, a)
			require.Equal(t, strings.TrimSpace(string(body)), strings.TrimSpace(text[a+len(start):b]), "fresh initialization must embed the same non-destructive migration")
		})
	}
}

func TestDeviceOperationIntentSQLServerMigrationStatements(t *testing.T) {
	body, err := migrationsfs.FS.ReadFile("migrations/2026-09-07-device-operation-intent-sqlserver.sql")
	require.NoError(t, err)
	statements := splitStatements(string(body))
	require.Len(t, statements, 2, "table and index must each be a complete independently idempotent statement")
	require.True(t, strings.HasPrefix(statements[0], "IF OBJECT_ID("))
	require.NotContains(t, statements[0], "\nBEGIN\n")
	require.Contains(t, statements[0], "CREATE TABLE dbo.gb_device_operation_intent")
	require.True(t, strings.HasPrefix(statements[1], "IF NOT EXISTS (SELECT 1 FROM sys.indexes"))
	require.Contains(t, statements[1], "object_id = OBJECT_ID(N'dbo.gb_device_operation_intent')")
	require.Contains(t, statements[1], "CREATE INDEX ix_device_intent_recovery")
}
