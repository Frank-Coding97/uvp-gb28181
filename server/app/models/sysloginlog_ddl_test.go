package models

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoginLogMigrationContract(t *testing.T) {
	root := filepath.Join("..", "..", "resource", "database", "gb28181", "migrations")
	files := []string{
		"2026-08-18-login-logs.sql", "2026-08-18-login-logs-postgresql.sql", "2026-08-18-login-logs-sqlserver.sql",
		"2026-08-18-login-logs-down.sql", "2026-08-18-login-logs-postgresql-down.sql", "2026-08-18-login-logs-sqlserver-down.sql",
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, name))
			require.NoError(t, err)
			text := strings.ToLower(strings.ReplaceAll(string(body), "`", ""))
			require.Contains(t, text, "sys_login_logs")
			if !strings.Contains(name, "down") {
				require.Contains(t, text, "username")
				require.Contains(t, text, "failure_reason")
			}
			require.NotContains(t, text, "password")
			require.NotContains(t, text, "refresh_token")
			require.NotContains(t, text, "cross join")
		})
	}
}

func TestLoginLogFreshInstallSnapshots(t *testing.T) {
	root := filepath.Join("..", "..", "resource", "database")
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, name))
			require.NoError(t, err)
			text := strings.ToLower(string(body))
			for _, token := range []string{"sys_login_logs", "failure_reason", "/system/login-log", "system/login-log/index", "system:login-log:list", "/api/sysloginlog/list", "/api/sysloginlog/:id"} {
				require.Contains(t, text, token)
			}
		})
	}
}
