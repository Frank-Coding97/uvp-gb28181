package models_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeviceTrafficMigrationsContainTablesIndexesAndPermissions(t *testing.T) {
	root := dualVersionDDLRoot(t)
	for _, name := range []string{
		"2026-08-15-device-traffic.sql",
		"2026-08-15-device-traffic-postgresql.sql",
		"2026-08-15-device-traffic-sqlserver.sql",
	} {
		body, err := os.ReadFile(filepath.Join(root, "resource/database/gb28181/migrations", name))
		require.NoError(t, err, name)
		text := strings.ToLower(string(body))
		for _, token := range []string{
			"gb_device_traffic_session", "gb_device_traffic_daily", "gb_device_traffic_gap",
			"business_key", "last_total_bytes", "upstream_bytes", "downstream_bytes",
			"uk_traffic_session_business", "uk_traffic_daily_scope",
			"idx_traffic_session_channel_started", "idx_traffic_gap_node_state",
			"gb28181:traffic:view", "device-mgmt", "sys_api", "sys_menu_api",
			"sys_role_menu", "sys_casbin_rule", "role_1", "not exists",
			"/api/gb28181/device-traffic/summary", "/api/gb28181/device-traffic/trend",
			"/api/gb28181/device-traffic/realtime", "/api/gb28181/device-traffic/sessions",
			"/api/gb28181/device-traffic/coverage", "/api/gb28181/device-traffic/viewers",
			"/api/gb28181/device-traffic/viewers/kick",
		} {
			require.Contains(t, text, token, "%s missing %s", name, token)
		}
	}
}
