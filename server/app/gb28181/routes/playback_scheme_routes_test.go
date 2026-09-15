package routes

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPlaybackSchemeRoutesRegisteredOnlyInProtectedGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterRoutes(engine.Group("/api"))
	registered := make(map[string]bool)
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, route := range []string{
		"GET /api/gb28181/playback-schemes",
		"GET /api/gb28181/playback-schemes/:id",
		"POST /api/gb28181/playback-schemes",
		"PATCH /api/gb28181/playback-schemes/:id",
		"PUT /api/gb28181/playback-schemes/:id/layout",
		"DELETE /api/gb28181/playback-schemes/:id",
	} {
		require.True(t, registered[route], route)
	}

	publicEngine := gin.New()
	RegisterPublicRoutes(publicEngine.Group("/api"))
	for _, route := range publicEngine.Routes() {
		require.NotContains(t, route.Path, "playback-schemes")
	}
}

func TestPlaybackSchemePermissionMigrations(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../.."))
	files := []string{
		"2026-08-07-playback-scheme-permissions.sql",
		"2026-08-07-playback-scheme-permissions-postgresql.sql",
		"2026-08-07-playback-scheme-permissions-sqlserver.sql",
	}
	for _, name := range files {
		body, err := os.ReadFile(filepath.Join(root, "resource/database/gb28181/migrations", name))
		require.NoError(t, err, name)
		text := strings.ToLower(string(body))
		for _, token := range []string{
			"gb28181:playback-scheme:manage", "sys_api", "sys_menu_api", "sys_casbin_rule",
			"not exists", "/api/gb28181/playback-schemes", "role_1",
		} {
			require.Contains(t, text, token, name)
		}
		require.NotContains(t, text, "multi-screen-playback", "不得创建可见页面菜单")
	}

	for _, path := range []string{
		"resource/database/uvp-gb28181.sql",
		"resource/database/postgresql_converted.sql",
		"resource/database/sqlserver_converted.sql",
	} {
		body, err := os.ReadFile(filepath.Join(root, path))
		require.NoError(t, err, path)
		text := strings.ToLower(string(body))
		require.Contains(t, text, "gb28181:playback-scheme:manage", path)
		require.Contains(t, text, "/api/gb28181/playback-schemes", path)
	}
}
