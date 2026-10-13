package models_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManagedResourceMediaIdentityMigrationAddsBoundedFieldsWithoutBackfill(t *testing.T) {
	root := filepath.Join("..", "..", "..", "resource", "database")
	migrationRoot := filepath.Join(root, "gb28181", "migrations")
	migrations := []struct {
		name string
		up   string
		down string
	}{
		{name: "mysql", up: "2026-08-30-zlm-managed-resource-media-identity.sql", down: "2026-08-30-zlm-managed-resource-media-identity-down.sql"},
		{name: "postgresql", up: "2026-08-30-zlm-managed-resource-media-identity-postgresql.sql", down: "2026-08-30-zlm-managed-resource-media-identity-postgresql-down.sql"},
		{name: "sqlserver", up: "2026-08-30-zlm-managed-resource-media-identity-sqlserver.sql", down: "2026-08-30-zlm-managed-resource-media-identity-sqlserver-down.sql"},
	}
	for _, migration := range migrations {
		migration := migration
		t.Run(migration.name, func(t *testing.T) {
			up := normalizeManagedResourceMediaIdentityDDL(t, filepath.Join(migrationRoot, migration.up))
			down := normalizeManagedResourceMediaIdentityDDL(t, filepath.Join(migrationRoot, migration.down))
			for _, token := range []string{"gb_zlm_managed_resource", "schema", "vhost", "32", "128", "uk_gb_zlm_managed_resource_identity"} {
				require.Containsf(t, up, token, "%s 增量迁移缺少 %s", migration.name, token)
			}
			require.NotContains(t, up, "update gb_zlm_managed_resource", "旧账本身份必须保持空值，不能伪造回填")
			require.NotContains(t, up, "set schema")
			require.NotContains(t, up, "set vhost")
			require.NotContains(t, down, "drop table")
			require.NotContains(t, down, "delete from")
		})
	}
}

func normalizeManagedResourceMediaIdentityDDL(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err, path)
	return strings.Join(strings.Fields(strings.ToLower(strings.NewReplacer("`", "", "[", "", "]", "", "\"", "", "\r", " ", "\n", " ", "\t", " ").Replace(string(body)))), " ")
}
