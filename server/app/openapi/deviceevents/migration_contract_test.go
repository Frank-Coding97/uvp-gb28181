package deviceevents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDeviceSubscriptionSQLiteMigrationCreatesAndDropsSchema(t *testing.T) {
	directory := filepath.Join("..", "..", "..", "resource", "database", "gb28181", "migrations")
	up, err := os.ReadFile(filepath.Join(directory, "2026-10-11-openapi-device-event-subscription-sqlite.sql"))
	require.NoError(t, err)
	down, err := os.ReadFile(filepath.Join(directory, "2026-10-11-openapi-device-event-subscription-sqlite-down.sql"))
	require.NoError(t, err)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	for _, statement := range strings.Split(string(up), ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		require.NoError(t, db.Exec(statement).Error)
	}
	require.True(t, db.Migrator().HasTable("sys_openapi_device_subscription"))
	for _, index := range []string{"idx_openapi_device_sub_client", "idx_openapi_device_sub_enabled", "idx_openapi_device_sub_expires"} {
		require.True(t, db.Migrator().HasIndex("sys_openapi_device_subscription", index), index)
	}

	insert := "INSERT INTO sys_openapi_device_subscription (id, client_id, device_id, kind, enabled, expires_seconds, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)"
	values := []any{"sub-1", 71, 10, "alarm", true, 600, "2026-10-11 10:00:00", "2026-10-11 09:00:00", "2026-10-11 09:00:00"}
	require.NoError(t, db.Exec(insert, values...).Error)
	values[0] = "sub-2"
	require.Error(t, db.Exec(insert, values...).Error, "one client cannot create duplicate kind rows for one device")

	for _, statement := range strings.Split(string(down), ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		require.NoError(t, db.Exec(statement).Error)
	}
	require.False(t, db.Migrator().HasTable("sys_openapi_device_subscription"))
}

func TestDeviceSubscriptionMigrationFilesCoverSupportedDialects(t *testing.T) {
	directory := filepath.Join("..", "..", "..", "resource", "database", "gb28181", "migrations")
	for _, dialect := range []string{"", "-postgresql", "-sqlserver", "-sqlite"} {
		upName := "2026-10-11-openapi-device-event-subscription" + dialect + ".sql"
		downName := "2026-10-11-openapi-device-event-subscription" + dialect + "-down.sql"
		up, err := os.ReadFile(filepath.Join(directory, upName))
		require.NoError(t, err, upName)
		down, err := os.ReadFile(filepath.Join(directory, downName))
		require.NoError(t, err, downName)
		require.Contains(t, strings.ToLower(string(up)), "sys_openapi_device_subscription")
		require.Contains(t, strings.ToLower(string(down)), "sys_openapi_device_subscription")
	}
}
