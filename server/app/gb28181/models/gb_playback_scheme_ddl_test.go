package models_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlaybackSchemeMigrationContracts(t *testing.T) {
	root := dualVersionDDLRoot(t)
	files := []string{
		"2026-08-07-playback-schemes.sql",
		"2026-08-07-playback-schemes-postgresql.sql",
		"2026-08-07-playback-schemes-sqlserver.sql",
	}
	for _, name := range files {
		body, err := os.ReadFile(filepath.Join(root, "resource/database/gb28181/migrations", name))
		require.NoError(t, err, name)
		text := strings.ToLower(string(body))
		for _, token := range []string{
			"gb_playback_scheme", "gb_playback_scheme_slot", "owner_user_id", "owner_dept_id",
			"layout_size", "slot_count", "slot_index", "device_code", "channel_code",
			"device_name_snapshot", "channel_name_snapshot", "uk_playback_scheme_owner_name",
			"uk_playback_scheme_slot", "idx_playback_scheme_owner_updated",
		} {
			require.Contains(t, text, token, name)
		}
		require.NotContains(t, text, "foreign key", name)
	}
}

func TestPlaybackSchemeDownMigrationsProtectData(t *testing.T) {
	root := dualVersionDDLRoot(t)
	for _, name := range []string{
		"2026-08-07-playback-schemes-down.sql",
		"2026-08-07-playback-schemes-postgresql-down.sql",
		"2026-08-07-playback-schemes-sqlserver-down.sql",
	} {
		body, err := os.ReadFile(filepath.Join(root, "resource/database/gb28181/migrations", name))
		require.NoError(t, err, name)
		text := strings.ToLower(string(body))
		require.Contains(t, text, "gb_playback_scheme_slot", name)
		require.Contains(t, text, "gb_playback_scheme", name)
		require.True(t, strings.Contains(text, "signal") || strings.Contains(text, "raise exception") || strings.Contains(text, "throw"), name)
		require.Less(t, strings.Index(text, "gb_playback_scheme_slot"), strings.LastIndex(text, "gb_playback_scheme"), name)
	}
}

func TestPlaybackSchemeFreshSchemasMatchMigration(t *testing.T) {
	root := dualVersionDDLRoot(t)
	for _, path := range []string{
		"resource/database/uvp-gb28181.sql",
		"resource/database/postgresql_converted.sql",
		"resource/database/sqlserver_converted.sql",
	} {
		body, err := os.ReadFile(filepath.Join(root, path))
		require.NoError(t, err, path)
		text := strings.ToLower(string(body))
		for _, token := range []string{
			"gb_playback_scheme", "gb_playback_scheme_slot", "owner_user_id", "layout_size", "slot_index",
		} {
			require.Contains(t, text, token, path)
		}
	}
}
