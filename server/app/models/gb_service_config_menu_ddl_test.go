package models

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGBServiceConfigMenuMigrations(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	serverRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))

	for _, filename := range []string{
		"2026-08-07-gb-service-config-menu.sql",
		"2026-08-07-gb-service-config-menu-postgresql.sql",
		"2026-08-07-gb-service-config-menu-sqlserver.sql",
	} {
		t.Run(filename, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(serverRoot, "resource", "database", "gb28181", "migrations", filename))
			require.NoError(t, err)
			sql := strings.ToLower(string(body))
			for _, token := range []string{
				"/gb28181/sip/config",
				"gb28181/sip/serviceconfig",
				"国标服务配置",
				"sys_role_menu",
				"gb28181:sip:config:view",
				"gb28181:sip:config:update",
			} {
				require.Contains(t, sql, token)
			}
			require.Contains(t, sql, "parent_id")
			require.Contains(t, sql, "= 0")
			require.NotContains(t, sql, "gb_parent_id", "一级菜单 migration must not resolve or depend on a GB parent menu")
			require.NotContains(t, sql, "140355", "migration must not depend on a fixed GB menu id")
		})
	}
}
