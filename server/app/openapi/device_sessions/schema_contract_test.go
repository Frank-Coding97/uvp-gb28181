package devicesessions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCaptureSessionSchemaIsPresentInMigrationsAndFreshBaselines(t *testing.T) {
	serverRoot, err := filepath.Abs("../../..")
	require.NoError(t, err)
	columns := []string{
		"client_id", "idempotency_digest", "request_digest", "session_id", "registry_session_id",
		"operation_id", "device_id", "channel_id", "internal_device_id", "internal_channel_id",
		"department_id", "snap_num", "interval_seconds", "state", "created_at", "updated_at",
	}
	indexes := []string{
		"uk_openapi_capture_idem", "uk_openapi_capture_session", "idx_openapi_capture_registry_session",
		"idx_openapi_capture_session_client", "idx_openapi_capture_state_updated",
	}
	files := []string{
		"resource/database/gb28181/migrations/2026-10-11-openapi-device-capture-session.sql",
		"resource/database/gb28181/migrations/2026-10-11-openapi-device-capture-session-postgresql.sql",
		"resource/database/gb28181/migrations/2026-10-11-openapi-device-capture-session-sqlserver.sql",
		"resource/database/gb28181/migrations/2026-10-11-openapi-device-capture-session-sqlite.sql",
		"resource/database/uvp-gb28181.sql",
		"resource/database/postgresql_converted.sql",
		"resource/database/sqlserver_converted.sql",
		"resource/database/sqlitebaseline/baseline.sql",
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(serverRoot, file))
			require.NoError(t, err)
			text := strings.ToLower(string(body))
			require.Contains(t, text, "sys_openapi_device_capture_session")
			for _, column := range columns {
				require.Containsf(t, text, column, "%s is missing %s", file, column)
			}
			for _, index := range indexes {
				require.Containsf(t, text, index, "%s is missing %s", file, index)
			}
		})
	}

	for _, file := range []string{
		"resource/database/gb28181/migrations/2026-10-11-openapi-device-capture-session-down.sql",
		"resource/database/gb28181/migrations/2026-10-11-openapi-device-capture-session-postgresql-down.sql",
		"resource/database/gb28181/migrations/2026-10-11-openapi-device-capture-session-sqlserver-down.sql",
		"resource/database/gb28181/migrations/2026-10-11-openapi-device-capture-session-sqlite-down.sql",
	} {
		body, err := os.ReadFile(filepath.Join(serverRoot, file))
		require.NoError(t, err)
		require.Contains(t, strings.ToLower(string(body)), "sys_openapi_device_capture_session")
	}

	manifest, err := os.ReadFile(filepath.Join(serverRoot, "resource/database/sqlitebaseline/manifest.json"))
	require.NoError(t, err)
	require.Contains(t, string(manifest), `"version": "sqlite-baseline-20261013-r2"`)
	require.Contains(t, string(manifest), `"tables": 112`)
}
