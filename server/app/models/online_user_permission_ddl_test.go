package models

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOnlineUserPermissionMigrations(t *testing.T) {
	root := filepath.Join("..", "..", "resource", "database", "gb28181", "migrations")
	for _, filename := range []string{
		"2026-08-17-online-user-permissions.sql",
		"2026-08-17-online-user-permissions-postgresql.sql",
		"2026-08-17-online-user-permissions-sqlserver.sql",
	} {
		t.Run(filename, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, filename))
			require.NoError(t, err)
			sql := strings.ToLower(string(body))
			for _, token := range []string{
				"/system/online-user", "system/online-user/index", "在线用户",
				"lucide:usersround", "system:online-user:list",
				"system:online-user:force-logout", "/api/sysonlineuser/list",
				"/api/sysonlineuser/forcelogout", "'get'", "'post'",
				"sys_api", "sys_menu", "sys_role_menu", "sys_menu_api",
				"sys_casbin_rule", "not exists",
			} {
				require.Contains(t, sql, token, "%s missing %s", filename, token)
			}
			require.NotContains(t, sql, "/api/users/session/heartbeat")
			require.NotContains(t, sql, "cross join", "permission mapping must join exact menu/API pairs")
		})
	}
}

func TestOnlineUserPermissionDownMigrationsDeleteInDependencyOrder(t *testing.T) {
	root := filepath.Join("..", "..", "resource", "database", "gb28181", "migrations")
	for _, filename := range []string{
		"2026-08-17-online-user-permissions-down.sql",
		"2026-08-17-online-user-permissions-postgresql-down.sql",
		"2026-08-17-online-user-permissions-sqlserver-down.sql",
	} {
		t.Run(filename, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, filename))
			require.NoError(t, err)
			sql := strings.ToLower(string(body))
			positions := []int{
				strings.Index(sql, "-- cleanup 1: sys_casbin_rule"),
				strings.Index(sql, "-- cleanup 2: sys_menu_api"),
				strings.Index(sql, "-- cleanup 3: sys_role_menu"),
				strings.Index(sql, "-- cleanup 4: sys_menu"),
				strings.Index(sql, "-- cleanup 5: sys_api"),
			}
			for index, position := range positions {
				require.GreaterOrEqual(t, position, 0, "%s missing cleanup table %d", filename, index)
				if index > 0 {
					require.Greater(t, position, positions[index-1], "%s cleanup order is unsafe", filename)
				}
			}
			for _, token := range []string{"system:online-user:list", "system:online-user:force-logout", "/api/sysonlineuser/list", "/api/sysonlineuser/forcelogout"} {
				require.Contains(t, sql, token)
			}
			require.NotContains(t, sql, "/api/users/session/heartbeat")
		})
	}
}

func TestOnlineUserFreshInstallSnapshots(t *testing.T) {
	root := filepath.Join("..", "..", "resource", "database")
	for _, filename := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(filename, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, filename))
			require.NoError(t, err)
			sql := strings.ToLower(string(body))
			for _, token := range []string{
				"sys_user_sessions", "refresh_token_hash", "idx_session_valid",
				"/system/online-user", "system/online-user/index", "lucide:usersround",
				"system:online-user:list", "system:online-user:force-logout",
				"/api/sysonlineuser/list", "/api/sysonlineuser/forcelogout",
				"sys_role_menu", "sys_menu_api", "sys_casbin_rule",
			} {
				require.Contains(t, sql, token, "%s missing %s", filename, token)
			}
			require.NotContains(t, sql, "/api/users/session/heartbeat")
		})
	}
}
