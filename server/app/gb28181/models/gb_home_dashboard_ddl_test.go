package models_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHomeDashboardMigrationContracts(t *testing.T) {
	root := dualVersionDDLRoot(t)
	for _, name := range []string{
		"2026-09-02-home-dashboard.sql",
		"2026-09-02-home-dashboard-postgresql.sql",
		"2026-09-02-home-dashboard-sqlserver.sql",
	} {
		body, err := os.ReadFile(filepath.Join(root, "resource/database/gb28181/migrations", name))
		require.NoError(t, err, name)
		text := strings.ToLower(string(body))
		for _, token := range []string{
			"gb_dashboard_layout", "gb_sip_metric_minute", "gb_sip_metric_flush", "gb_sip_metric_gap", "gb_play_attempt",
			"dashboard_key", "schema_version", "revision", "layout_json", "bucket_start", "direction", "request_count",
			"flush_id", "correlation_id", "outcome", "started_at", "uk_dashboard_layout_user_key", "uk_sip_metric_minute_bucket",
		} {
			require.Contains(t, text, token, name)
		}
		require.NotContains(t, text, " json ", name)
	}
}

func TestHomeDashboardDownMigrationsProtectFacts(t *testing.T) {
	root := dualVersionDDLRoot(t)
	for _, name := range []string{
		"2026-09-02-home-dashboard-down.sql",
		"2026-09-02-home-dashboard-postgresql-down.sql",
		"2026-09-02-home-dashboard-sqlserver-down.sql",
	} {
		body, err := os.ReadFile(filepath.Join(root, "resource/database/gb28181/migrations", name))
		require.NoError(t, err, name)
		text := strings.ToLower(string(body))
		require.Contains(t, text, "gb_dashboard_layout", name)
		require.Contains(t, text, "gb_play_attempt", name)
		require.True(t, strings.Contains(text, "signal") || strings.Contains(text, "raise exception") || strings.Contains(text, "throw"), name)
	}
}
