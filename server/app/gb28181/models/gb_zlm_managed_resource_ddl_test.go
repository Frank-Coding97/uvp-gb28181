package models_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManagedResourceMigrationContractAcrossSupportedDatabases(t *testing.T) {
	root := filepath.Join("..", "..", "..", "resource", "database")
	migrationRoot := filepath.Join(root, "gb28181", "migrations")
	migrations := []struct {
		name  string
		up    string
		down  string
		guard string
	}{
		{name: "mysql", up: "2026-08-29-zlm-managed-resource.sql", down: "2026-08-29-zlm-managed-resource-down.sql", guard: "create table if not exists"},
		{name: "postgresql", up: "2026-08-29-zlm-managed-resource-postgresql.sql", down: "2026-08-29-zlm-managed-resource-postgresql-down.sql", guard: "create table if not exists"},
		{name: "sqlserver", up: "2026-08-29-zlm-managed-resource-sqlserver.sql", down: "2026-08-29-zlm-managed-resource-sqlserver-down.sql", guard: "object_id"},
	}

	for _, migration := range migrations {
		migration := migration
		t.Run(migration.name, func(t *testing.T) {
			up := normalizeManagedResourceDDL(t, filepath.Join(migrationRoot, migration.up))
			down := normalizeManagedResourceDDL(t, filepath.Join(migrationRoot, migration.down))
			require.Contains(t, up, migration.guard)
			for _, token := range []string{
				"gb_zlm_managed_resource", "node_id", "resource_type", "resource_key", "app", "stream",
				"identity_fingerprint", "summary", "created_by", "created_at", "last_observed_at",
				"tombstoned_at", "updated_at", "uk_gb_zlm_managed_resource_identity",
				"idx_gb_zlm_managed_resource_observed", "idx_gb_zlm_managed_resource_tombstone",
			} {
				require.Containsf(t, up, token, "%s 缺少账本字段或索引 %s", migration.name, token)
			}
			for _, forbidden := range []string{" enum", " json", "source_url", "target_url", "secret", "password", "token", "desired_state", "runtime_state", "online"} {
				require.NotContainsf(t, up, forbidden, "%s 不得持久化敏感字段或运行态字段 %s", migration.name, forbidden)
			}
			require.Contains(t, down, "select 1", "down 必须是可执行的安全兼容回滚")
			require.NotContains(t, down, "drop table", "回滚应用时必须保留来源账本")
			require.NotContains(t, down, "delete from", "回滚应用时不得删除来源账本")
		})
	}
}

func TestManagedResourceFreshSchemasHaveEquivalentPortableTable(t *testing.T) {
	root := filepath.Join("..", "..", "..", "resource", "database")
	files := []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, name))
			require.NoError(t, err)
			section := managedResourceTableSection(string(body))
			for _, token := range []string{
				"gb_zlm_managed_resource", "node_id", "resource_type", "resource_key", "app", "stream",
				"identity_fingerprint", "summary", "created_by", "created_at", "last_observed_at",
				"tombstoned_at", "updated_at", "uk_gb_zlm_managed_resource_identity",
				"idx_gb_zlm_managed_resource_observed", "idx_gb_zlm_managed_resource_tombstone",
			} {
				require.Containsf(t, strings.ToLower(section), token, "%s fresh schema 缺少 %s", name, token)
			}
			for _, forbidden := range []string{" enum", " json", "source_url", "target_url", "secret", "password", "token", "desired_state", "runtime_state", "online"} {
				require.NotContainsf(t, strings.ToLower(section), forbidden, "%s fresh schema 不得保存敏感或运行态字段 %s", name, forbidden)
			}
		})
	}
}

func normalizeManagedResourceDDL(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err, path)
	return strings.Join(strings.Fields(strings.ToLower(strings.NewReplacer("`", "", "[", "", "]", "", "\"", "", "\r", " ", "\n", " ", "\t", " ").Replace(string(body)))), " ")
}

func managedResourceTableSection(body string) string {
	text := strings.ToLower(strings.NewReplacer("`", "", "[", "", "]", "", "\"", "").Replace(body))
	start := strings.Index(text, "create table gb_zlm_managed_resource")
	if start < 0 {
		return ""
	}
	section := text[start:]
	for _, marker := range []string{"\n-- table structure for ", "\ncreate table ", "\nif object_id(n'"} {
		if end := strings.Index(section[1:], marker); end >= 0 {
			section = section[:end+1]
		}
	}
	return section
}
