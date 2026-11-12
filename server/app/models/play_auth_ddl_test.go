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
				"/api/gb28181/play/:deviceid/:channelid",
				"/api/gb28181/play/:deviceid/:channelid/authorization",
				"'get'", "'put'", "'post'",
				"gb28181:sip:config:view", "gb28181:sip:config:update",
				"gb28181:play:start",
				"sys_api", "sys_menu", "sys_role_menu", "sys_menu_api", "sys_casbin_rule", "not exists",
			} {
				require.Contains(t, sql, token)
			}
			assertLegacyAuthorizationPermissionCleanup(t, sql)
			assertPlayAPIsUseDedicatedPermission(t, sql)
		})
	}
}

func assertLegacyAuthorizationPermissionCleanup(t *testing.T, sql string) {
	t.Helper()
	normalized := strings.NewReplacer("`", "", "[", "", "]", "").Replace(sql)
	for _, table := range []string{"sys_casbin_rule", "sys_menu_api"} {
		found := false
		for _, statement := range strings.Split(normalized, ";") {
			if strings.Contains(statement, "delete") && strings.Contains(statement, table) &&
				strings.Contains(statement, "/api/gb28181/play/:deviceid/:channelid/authorization") &&
				strings.Contains(statement, "gb28181:sip:config:view") {
				found = true
				break
			}
		}
		require.Truef(t, found, "missing legacy config permission cleanup for %s", table)
	}
}

func assertPlayAPIsUseDedicatedPermission(t *testing.T, sql string) {
	t.Helper()
	for _, path := range []string{
		"/api/gb28181/play/:deviceid/:channelid",
		"/api/gb28181/play/:deviceid/:channelid/authorization",
	} {
		found := false
		for _, statement := range strings.Split(sql, ";") {
			if strings.Contains(statement, "insert into") && strings.Contains(statement, "sys_menu_api") && strings.Contains(statement, path) {
				found = true
				require.Contains(t, statement, "gb28181:play:start")
				require.NotContains(t, statement, "gb28181:sip:config:view")
			}
		}
		require.Truef(t, found, "missing sys_menu_api mapping for %s", path)
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
			require.Contains(t, sql, "/api/gb28181/play/:deviceid/:channelid")
			require.Contains(t, sql, "/api/gb28181/play/:deviceid/:channelid/authorization")
			require.Contains(t, sql, "gb28181:play:start")
			require.Contains(t, sql, "non-destructive")
			statements := stripSQLLineComments(sql)
			require.Equal(t, "select 1;", statements)
			for _, keyword := range []string{"delete", "update", "insert", "merge", "drop", "truncate", "alter"} {
				require.NotContainsf(t, statements, keyword, "down migration must not mutate shared authorization data with %s", keyword)
			}
		})
	}
}

func stripSQLLineComments(sql string) string {
	var statements []string
	for _, line := range strings.Split(sql, "\n") {
		if comment := strings.Index(line, "--"); comment >= 0 {
			line = line[:comment]
		}
		if line = strings.TrimSpace(line); line != "" {
			statements = append(statements, line)
		}
	}
	return strings.Join(statements, "\n")
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
				"/api/gb28181/play/:deviceid/:channelid",
				"/api/gb28181/play/:deviceid/:channelid/authorization",
				"'get'", "'put'", "'post'",
				"gb28181:sip:config:view", "gb28181:sip:config:update",
				"gb28181:play:start",
				"sys_menu_api", "sys_casbin_rule",
			} {
				require.Contains(t, sql, token)
			}
			require.NotContains(t, sql, "(140359,250)")
			require.NotContains(t, sql, "(140360,250)")
		})
	}
}
