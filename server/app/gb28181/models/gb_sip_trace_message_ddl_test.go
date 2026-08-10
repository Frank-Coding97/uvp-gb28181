package models_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSIPTraceMessageMigrationsShareSchemaAndGuards(t *testing.T) {
	root := migrationRoot(t)
	files := []struct {
		name  string
		path  string
		guard []string
	}{
		{"mysql", "2026-08-10-sip-trace-message.sql", []string{"create table if not exists", "mediumblob", "idx_gb_sip_trace_occurred_event"}},
		{"postgresql", "2026-08-10-sip-trace-message-postgresql.sql", []string{"create table if not exists", "bytea", "create index if not exists"}},
		{"sqlserver", "2026-08-10-sip-trace-message-sqlserver.sql", []string{"object_id", "varbinary(max)", "sys.indexes"}},
	}
	columns := []string{"event_id", "occurred_at", "direction", "device_id", "method", "status_code", "call_id", "cseq", "payload_nonce", "payload_ciphertext", "payload_algorithm", "payload_key_version", "payload_digest_sha256"}
	for _, file := range files {
		t.Run(file.name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, file.path))
			require.NoError(t, err)
			text := strings.ToLower(string(body))
			for _, column := range columns {
				require.Contains(t, text, column)
			}
			for _, guard := range file.guard {
				require.Contains(t, text, guard)
			}
		})
	}
	for _, path := range []string{"2026-08-10-sip-trace-message-down.sql", "2026-08-10-sip-trace-message-postgresql-down.sql", "2026-08-10-sip-trace-message-sqlserver-down.sql"} {
		body, err := os.ReadFile(filepath.Join(root, path))
		require.NoError(t, err)
		require.Contains(t, strings.ToLower(string(body)), "gb_sip_trace_message")
	}
	for _, path := range []string{"2026-07-19-sip-trace-capture.sql", "2026-08-10-sip-trace-capture-postgresql.sql", "2026-08-10-sip-trace-capture-sqlserver.sql"} {
		body, err := os.ReadFile(filepath.Join(root, path))
		require.NoError(t, err)
		text := strings.ToLower(string(body))
		require.Contains(t, text, "gb_sip_trace_capture")
		require.Contains(t, text, "active_key")
		require.Contains(t, text, "idx_sip_trace_capture_device_started")
	}
	databaseRoot := filepath.Join(root, "..", "..")
	for _, path := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		body, err := os.ReadFile(filepath.Join(databaseRoot, path))
		require.NoError(t, err)
		text := strings.ToLower(string(body))
		require.Contains(t, text, "gb_sip_trace_message")
		require.Contains(t, text, "gb_sip_trace_capture")
	}
}

func migrationRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "..", "resource", "database", "gb28181", "migrations")
}
