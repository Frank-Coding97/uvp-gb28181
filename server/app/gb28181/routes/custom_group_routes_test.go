package routes

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCustomGroupPermissionMigrations(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	for _, name := range []string{"2026-08-02-custom-group-permissions.sql", "2026-08-02-custom-group-permissions-postgresql.sql", "2026-08-02-custom-group-permissions-sqlserver.sql"} {
		body, err := os.ReadFile(filepath.Join(root, "resource/database/gb28181/migrations", name))
		require.NoError(t, err)
		text := strings.ToLower(string(body))
		for _, token := range []string{"gb28181:device-group:manage", "sys_api", "sys_menu_api", "sys_casbin_rule", "not exists", "/api/gb28181/device-mgmt/custom-groups"} {
			require.Contains(t, text, token)
		}
		require.NotContains(t, text, "component", "不得创建独立页面菜单")
	}
}
