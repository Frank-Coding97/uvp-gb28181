package models

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlayAuthPermissionMigrations(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	serverRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	for _, filename := range []string{
		"2026-08-11-play-auth-permissions.sql",
		"2026-08-11-play-auth-permissions-postgresql.sql",
		"2026-08-11-play-auth-permissions-sqlserver.sql",
	} {
		t.Run(filename, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(serverRoot, "resource", "database", "gb28181", "migrations", filename))
			require.NoError(t, err)
			sql := strings.ToLower(string(body))
			for _, token := range []string{
				"/api/gb28181/sip/service-config/play-auth",
				"'get'", "'put'",
				"gb28181:sip:config:view", "gb28181:sip:config:update",
				"sys_api", "sys_menu_api", "sys_casbin_rule", "role_1", "not exists",
			} {
				require.Contains(t, sql, token)
			}
		})
	}
}

func TestPlayAuthPermissionDownMigrations(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	serverRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	for _, filename := range []string{
		"2026-08-11-play-auth-permissions-down.sql",
		"2026-08-11-play-auth-permissions-postgresql-down.sql",
		"2026-08-11-play-auth-permissions-sqlserver-down.sql",
	} {
		t.Run(filename, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(serverRoot, "resource", "database", "gb28181", "migrations", filename))
			require.NoError(t, err)
			sql := strings.ToLower(string(body))
			require.Contains(t, sql, "/api/gb28181/sip/service-config/play-auth")
			require.Contains(t, sql, "delete")
			require.Contains(t, sql, "sys_casbin_rule")
			require.Contains(t, sql, "sys_menu_api")
			require.Contains(t, sql, "sys_api")
		})
	}
}

func TestPlayAuthFreshInstallSeeds(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	serverRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	for _, filename := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(filename, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(serverRoot, "resource", "database", filename))
			require.NoError(t, err)
			sql := strings.ToLower(string(body))
			for _, token := range []string{
				"/api/gb28181/sip/service-config/play-auth",
				"'get'", "'put'",
				"gb28181:sip:config:view", "gb28181:sip:config:update",
				"sys_menu_api", "sys_casbin_rule",
			} {
				require.Contains(t, sql, token)
			}
		})
	}
}
