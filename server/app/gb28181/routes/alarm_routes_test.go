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

func TestAlarmRoutesRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterRoutes(engine.Group("/api"))
	registered := make(map[string]bool)
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, route := range []string{
		"GET /api/gb28181/alarms",
		"GET /api/gb28181/alarms/:id",
		"DELETE /api/gb28181/alarms/:id",
		"POST /api/gb28181/alarms/batch-delete",
	} {
		require.True(t, registered[route], route)
	}
	require.True(t, registered["GET /api/gb28181/device-mgmt/device/:id/alarms"], "旧按设备查询接口必须保留")
}

func TestAlarmPermissionMigrationSeparatesViewAndDelete(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../.."))
	for _, path := range []string{
		"resource/database/gb28181/migrations/2026-08-04-alarm-management.sql",
		"resource/database/gb28181/migrations/2026-08-04-alarm-management-postgresql.sql",
		"resource/database/gb28181/migrations/2026-08-04-alarm-management-sqlserver.sql",
	} {
		body, err := os.ReadFile(filepath.Join(root, path))
		require.NoError(t, err)
		text := strings.NewReplacer("`", "", "[", "", "]", "").Replace(strings.ToLower(string(body)))
		require.Contains(t, text, "gb28181:alarm:view")
		require.Contains(t, text, "gb28181:alarm:delete")
		require.Contains(t, text, "a.method='get'")
		require.Contains(t, text, "a.method='delete'")
		require.Contains(t, text, "a.method='post'")
		require.Contains(t, text, "role_1")
	}
}
