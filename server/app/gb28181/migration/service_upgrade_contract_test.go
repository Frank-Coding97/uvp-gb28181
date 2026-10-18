package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServiceUpgradePermissionMigrationsCoverEveryDialect(t *testing.T) {
	dir := filepath.Join("../../../resource", "database", "gb28181", "migrations")
	variants := []string{"", "-postgresql", "-sqlserver", "-sqlite"}
	for _, variant := range variants {
		t.Run(variant, func(t *testing.T) {
			permission := filepath.Join(dir, "2026-10-16-service-upgrade-permission"+variant+".sql")
			permissionDown := filepath.Join(dir, "2026-10-16-service-upgrade-permission"+variant+"-down.sql")
			plan := filepath.Join(dir, "2026-10-16-service-upgrade-migration-plan"+variant+".sql")
			planDown := filepath.Join(dir, "2026-10-16-service-upgrade-migration-plan"+variant+"-down.sql")
			for _, file := range []string{permission, permissionDown, plan, planDown} {
				_, err := os.Stat(file)
				require.NoError(t, err, "服务升级迁移文件缺失: %s", file)
			}
			permissionBody, err := os.ReadFile(permission)
			require.NoError(t, err)
			permissionSQL := strings.ToLower(string(permissionBody))
			require.Contains(t, permissionSQL, "system:upgrade:read")
			require.Contains(t, permissionSQL, "system:upgrade:manage")
			require.Contains(t, permissionSQL, "/api/system/upgrade/migration-plan")
			require.Contains(t, permissionSQL, "'/api/system/upgrade/policy'")
			require.Contains(t, permissionSQL, "system:upgrade:manage")

			downBody, err := os.ReadFile(permissionDown)
			require.NoError(t, err)
			downSQL := strings.ToLower(string(downBody))
			require.NotContains(t, downSQL, "api_group='服务升级'")
			require.Contains(t, downSQL, "/api/system/upgrade/%")
			planBody, err := os.ReadFile(plan)
			require.NoError(t, err)
			require.Contains(t, strings.ToLower(string(planBody)), "system:upgrade:read")
		})
	}
}

func TestServiceUpgradeMigrationsAreEmbeddedForEveryDialect(t *testing.T) {
	wants := map[Dialect]string{
		DialectMySQL:     "2026-10-16-service-upgrade-permission.sql",
		DialectPostgres:  "2026-10-16-service-upgrade-permission-postgresql.sql",
		DialectSQLServer: "2026-10-16-service-upgrade-permission-sqlserver.sql",
		DialectSQLite:    "2026-10-16-service-upgrade-permission-sqlite.sql",
	}
	for dialect, permission := range wants {
		names, err := (&embedSource{dialect: dialect}).UpFiles()
		require.NoError(t, err)
		require.Contains(t, names, permission)
		plan := strings.Replace(permission, "service-upgrade-permission", "service-upgrade-migration-plan", 1)
		require.Contains(t, names, plan)
	}
}
