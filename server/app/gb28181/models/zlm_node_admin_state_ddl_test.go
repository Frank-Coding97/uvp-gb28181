package models_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestZLMNodeAdminStateMigrationForEveryDatabase(t *testing.T) {
	root := dualVersionDDLRoot(t)
	migrations := filepath.Join(root, "resource", "database", "gb28181", "migrations")
	for _, pair := range []struct {
		up, down, guard string
	}{
		{"2026-09-18-zlm-node-enabled.sql", "2026-09-18-zlm-node-enabled-down.sql", "information_schema"},
		{"2026-09-18-zlm-node-enabled-postgresql.sql", "2026-09-18-zlm-node-enabled-postgresql-down.sql", "if not exists"},
		{"2026-09-18-zlm-node-enabled-sqlserver.sql", "2026-09-18-zlm-node-enabled-sqlserver-down.sql", "col_length"},
	} {
		upBody, err := os.ReadFile(filepath.Join(migrations, pair.up))
		require.NoError(t, err)
		up := strings.NewReplacer("`", "", "[", "", "]", "").Replace(strings.ToLower(string(upBody)))
		for _, token := range []string{
			"meta_node", "enabled", "state='maintenance'", "state='offline'",
			"/api/gb28181/zlm/nodes/:id/enable", "/api/gb28181/zlm/nodes/:id/disable",
			"gb28181:zlm:node:manage", "sys_menu_api", "sys_casbin_rule", pair.guard,
		} {
			require.Containsf(t, up, token, "%s missing %s", pair.up, token)
		}

		downBody, err := os.ReadFile(filepath.Join(migrations, pair.down))
		require.NoError(t, err)
		down := strings.NewReplacer("`", "", "[", "", "]", "").Replace(strings.ToLower(string(downBody)))
		for _, token := range []string{"enabled", "maintenance", "deleted_at", "sys_menu_api", "sys_casbin_rule"} {
			require.Containsf(t, down, token, "%s missing %s", pair.down, token)
		}
	}
}

func TestZLMNodeAdminStatePresentInSchemas(t *testing.T) {
	root := dualVersionDDLRoot(t)
	for _, file := range []string{
		"gb28181/meta_node.sql",
		"uvp-gb28181.sql",
		"postgresql_converted.sql",
		"sqlserver_converted.sql",
	} {
		body, err := os.ReadFile(filepath.Join(root, "resource", "database", file))
		require.NoError(t, err)
		text := strings.ToLower(string(body))
		require.Containsf(t, text, "enabled", "%s missing enabled", file)
		if file != "gb28181/meta_node.sql" {
			require.Contains(t, text, "/api/gb28181/zlm/nodes/:id/enable")
			require.Contains(t, text, "/api/gb28181/zlm/nodes/:id/disable")
			require.Contains(t, text, "gb28181:zlm:node:manage")
		}
	}
}
