package models_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCustomGroupMigrationContracts(t *testing.T) {
	root := dualVersionDDLRoot(t)
	files := map[string]string{
		"mysql":      "2026-08-02-device-directory-custom-groups.sql",
		"postgresql": "2026-08-02-device-directory-custom-groups-postgresql.sql",
		"sqlserver":  "2026-08-02-device-directory-custom-groups-sqlserver.sql",
	}
	for dialect, name := range files {
		t.Run(dialect, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, "resource/database/gb28181/migrations", name))
			require.NoError(t, err)
			text := normalizeAlarmDDL(string(body))
			for _, token := range []string{
				"gb_custom_group", "gb_custom_group_device", "owner_dept_id", "parent_id",
				"path", "depth", "name", "group_id", "device_id", "created_by",
				"uk_custom_group_sibling_name", "uk_custom_group_device",
				"idx_custom_group_dept_path", "idx_custom_group_device_device",
			} {
				require.Contains(t, text, token)
			}
			require.NotContains(t, text, "foreign key")
			require.NotContains(t, text, "deleted_at")
		})
	}
	mysqlBody, err := os.ReadFile(filepath.Join(root, "resource/database/gb28181/migrations/2026-08-02-device-directory-custom-groups.sql"))
	require.NoError(t, err)
	require.Contains(t, strings.ToLower(string(mysqlBody)), "`path`(191)", "utf8mb4 path index must stay below MySQL's 3072-byte key limit")
}

func TestCustomGroupDownMigrationsProtectData(t *testing.T) {
	root := dualVersionDDLRoot(t)
	files := []string{
		"2026-08-02-device-directory-custom-groups-down.sql",
		"2026-08-02-device-directory-custom-groups-postgresql-down.sql",
		"2026-08-02-device-directory-custom-groups-sqlserver-down.sql",
	}
	for _, name := range files {
		body, err := os.ReadFile(filepath.Join(root, "resource/database/gb28181/migrations", name))
		require.NoError(t, err)
		text := normalizeAlarmDDL(string(body))
		require.Contains(t, text, "gb_custom_group_device")
		require.Contains(t, text, "gb_custom_group")
		require.True(t, strings.Contains(text, "signal") || strings.Contains(text, "raise exception") || strings.Contains(text, "throw"), "down 必须拒绝删除非空表")
		require.Less(t, strings.Index(text, "gb_custom_group_device"), strings.LastIndex(text, "gb_custom_group"), "关系表必须先于分组表处理")
	}
}

func TestCustomGroupFreshSchemasMatchMigration(t *testing.T) {
	root := dualVersionDDLRoot(t)
	for _, path := range []string{
		"resource/database/uvp-gb28181.sql",
		"resource/database/postgresql_converted.sql",
		"resource/database/sqlserver_converted.sql",
	} {
		body, err := os.ReadFile(filepath.Join(root, path))
		require.NoError(t, err)
		text := normalizeAlarmDDL(string(body))
		group := ddlTableSection(text, "gb_custom_group")
		member := ddlTableSection(text, "gb_custom_group_device")
		for _, token := range []string{"owner_dept_id", "parent_id", "path", "depth", "name", "created_by", "uk_custom_group_sibling_name"} {
			require.Contains(t, group, token, path)
		}
		for _, token := range []string{"group_id", "device_id", "created_by", "uk_custom_group_device"} {
			require.Contains(t, member, token, path)
		}
	}
	mysqlBody, err := os.ReadFile(filepath.Join(root, "resource/database/uvp-gb28181.sql"))
	require.NoError(t, err)
	require.Contains(t, strings.ToLower(string(mysqlBody)), "`path`(191)", "fresh MySQL schema must use the same bounded path index")
}
