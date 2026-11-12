package models

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSIPLogPermissionMigrations(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	serverRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))

	for _, filename := range []string{
		"2026-08-09-sip-log-permissions.sql",
		"2026-08-09-sip-log-permissions-postgresql.sql",
		"2026-08-09-sip-log-permissions-sqlserver.sql",
	} {
		t.Run(filename, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(serverRoot, "resource", "database", "gb28181", "migrations", filename))
			require.NoError(t, err)
			sql := strings.ToLower(string(body))
			for _, token := range []string{
				"/api/gb28181/sip/service-config/sip-log",
				"'get'",
				"'put'",
				"gb28181:sip:config:view",
				"gb28181:sip:config:update",
				"sys_api",
				"sys_menu_api",
				"sys_casbin_rule",
				"role_1",
			} {
				require.Contains(t, sql, token)
			}
		})
	}
}
