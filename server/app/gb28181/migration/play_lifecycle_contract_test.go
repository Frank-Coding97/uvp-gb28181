package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	migrationsfs "uvplatform.cn/uvp-gb28181/resource/database/gb28181"
)

const playLifecycleMigration = "2026-09-21-play-lifecycle-log"

func TestPlayLifecycleMigrationThreeDialectContract(t *testing.T) {
	files := []struct{ up, down string }{
		{playLifecycleMigration + ".sql", playLifecycleMigration + "-down.sql"},
		{playLifecycleMigration + "-postgresql.sql", playLifecycleMigration + "-postgresql-down.sql"},
		{playLifecycleMigration + "-sqlserver.sql", playLifecycleMigration + "-sqlserver-down.sql"},
	}
	for _, file := range files {
		t.Run(file.up, func(t *testing.T) {
			up, err := migrationsfs.FS.ReadFile("migrations/" + file.up)
			require.NoError(t, err)
			normalized := normalizeLifecycleDDL(string(up))
			for _, token := range []string{
				"gb_play_attempt", "gb_play_lifecycle_event", "event_id", "lifecycle_id", "sequence",
				"event_at", "elapsed_ms", "stage", "event_name", "fact_state", "source", "metadata_json",
				"stream_id", "ssrc", "call_id", "cseq", "current_stage", "media_state", "client_state",
				"lifecycle_state", "reason_code", "reason_message", "last_event_at",
				"uk_play_lifecycle_event_id", "uk_play_lifecycle_sequence",
			} {
				require.Contains(t, normalized, token)
			}
			for _, state := range []string{"unknown", "in_progress"} {
				require.Contains(t, normalized, state)
			}

			down, err := migrationsfs.FS.ReadFile("migrations/" + file.down)
			require.NoError(t, err)
			normalizedDown := normalizeLifecycleDDL(string(down))
			require.Contains(t, normalizedDown, "drop")
			require.Contains(t, normalizedDown, "gb_play_lifecycle_event")
			require.NotContains(t, normalizedDown, "drop table gb_play_attempt")
		})
	}
}

func TestPlayLifecycleMigrationIsDiscoverable(t *testing.T) {
	entries, err := migrationsfs.FS.ReadDir("migrations")
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	for dialect, expected := range map[Dialect]string{
		DialectMySQL: playLifecycleMigration + ".sql", DialectPostgres: playLifecycleMigration + "-postgresql.sql", DialectSQLServer: playLifecycleMigration + "-sqlserver.sql",
	} {
		require.Contains(t, FilterUpFiles(names, dialect), expected)
	}
}

func TestPlayLifecyclePermissionMigrationThreeDialectContract(t *testing.T) {
	base := "2026-09-21-play-lifecycle-permissions"
	for _, suffix := range []string{".sql", "-postgresql.sql", "-sqlserver.sql"} {
		body, err := migrationsfs.FS.ReadFile("migrations/" + base + suffix)
		require.NoError(t, err)
		normalized := normalizeLifecycleDDL(string(body))
		for _, token := range []string{
			"/gb28181/playback-log", "gb28181/playback-log/index", "gb28181:play-log:view",
			"/api/gb28181/play/lifecycles", "/api/gb28181/play/lifecycles/:lifecycleid",
			"/api/gb28181/play/lifecycles/:lifecycleid/client-events", "sys_menu_api", "sys_casbin_rule", "not exists",
			} {
				require.Contains(t, normalized, token, suffix)
			}
			if suffix == ".sql" {
				for _, token := range []string{
					"convert(p.v0 using utf8mb4) collate utf8mb4_unicode_ci",
					"convert(p.v1 using utf8mb4) collate utf8mb4_unicode_ci",
					"convert(p.v2 using utf8mb4) collate utf8mb4_unicode_ci",
					"convert(p.v3 using utf8mb4) collate utf8mb4_unicode_ci",
				} {
					require.Contains(t, normalized, token, "mysql permission migration must normalize casbin comparisons: %s", token)
				}
			}
		}
	for _, suffix := range []string{"-down.sql", "-postgresql-down.sql", "-sqlserver-down.sql"} {
		body, err := migrationsfs.FS.ReadFile("migrations/" + base + suffix)
		require.NoError(t, err)
		normalized := normalizeLifecycleDDL(string(body))
		require.Contains(t, normalized, "/gb28181/playback-log")
		require.Contains(t, normalized, "deleted_at")
	}
}

func TestPlayLifecycleFreshBaselinesContainTablesAndPermissionEntry(t *testing.T) {
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("..", "..", "..", "resource", "database", name))
			require.NoError(t, err)
			normalized := normalizeLifecycleDDL(string(body))
			for _, token := range []string{
				"gb_play_attempt", "gb_play_lifecycle_event", "metadata_json",
				"/api/gb28181/play/lifecycles", "/gb28181/playback-log", "gb28181:play-log:view",
			} {
				require.Contains(t, normalized, token)
			}
			if name == "uvp-gb28181.sql" {
				for _, token := range []string{
					"convert(p.v0 using utf8mb4) collate utf8mb4_unicode_ci",
					"convert(p.v1 using utf8mb4) collate utf8mb4_unicode_ci",
					"convert(p.v2 using utf8mb4) collate utf8mb4_unicode_ci",
					"convert(p.v3 using utf8mb4) collate utf8mb4_unicode_ci",
				} {
					require.Contains(t, normalized, token, "mysql fresh baseline must normalize casbin comparisons: %s", token)
				}
			}
		})
	}
}

func normalizeLifecycleDDL(value string) string {
	value = strings.NewReplacer("`", "", "[", "", "]", "").Replace(strings.ToLower(value))
	return strings.Join(strings.Fields(value), " ")
}
