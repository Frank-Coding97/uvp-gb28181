package models

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOnlineUserSessionMigrationsDefinePortableSessionTable(t *testing.T) {
	root := filepath.Join("..", "..", "resource", "database", "gb28181", "migrations")
	files := []string{
		"2026-08-17-online-user-sessions.sql",
		"2026-08-17-online-user-sessions-postgresql.sql",
		"2026-08-17-online-user-sessions-sqlserver.sql",
	}
	wanted := []string{
		"sys_user_sessions", "sid", "user_id", "refresh_token_hash", "refresh_jti",
		"client_ip", "login_location", "user_agent", "browser", "os", "login_at",
		"last_active_at", "session_expires_at", "revoked_at", "revoke_reason", "revoked_by",
		"idx_user_id", "idx_session_valid", "idx_client_ip", "not exists",
	}
	for _, filename := range files {
		t.Run(filename, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, filename))
			require.NoError(t, err)
			text := strings.ToLower(string(body))
			for _, token := range wanted {
				require.Contains(t, text, token, "%s missing %s", filename, token)
			}
			require.NotContains(t, text, "refresh token", "migration must not document or store a plaintext refresh token")
		})
	}
}

func TestOnlineUserSessionDownMigrationsAreExplicitlyScoped(t *testing.T) {
	root := filepath.Join("..", "..", "resource", "database", "gb28181", "migrations")
	for _, filename := range []string{
		"2026-08-17-online-user-sessions-down.sql",
		"2026-08-17-online-user-sessions-postgresql-down.sql",
		"2026-08-17-online-user-sessions-sqlserver-down.sql",
	} {
		t.Run(filename, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, filename))
			require.NoError(t, err)
			text := strings.ToLower(string(body))
			require.Contains(t, text, "sys_user_sessions")
			require.NotContains(t, text, "sys_users")
			require.NotContains(t, text, "sys_operation_logs")
		})
	}
}
