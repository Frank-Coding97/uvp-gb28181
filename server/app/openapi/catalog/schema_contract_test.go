package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var capabilityCatalogBaselines = []string{
	"uvp-gb28181.sql",
	"postgresql_converted.sql",
	"sqlserver_converted.sql",
}

const (
	capabilityCatalogBegin = "-- openapi-capability-catalog:begin"
	capabilityCatalogEnd   = "-- openapi-capability-catalog:end"
)

func baselineCatalogBlock(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("../../../resource/database", name))
	require.NoError(t, err)
	text := strings.ToLower(string(body))
	start := strings.Index(text, capabilityCatalogBegin)
	end := strings.Index(text, capabilityCatalogEnd)
	require.GreaterOrEqualf(t, start, 0, "%s 缺少 catalog begin marker", name)
	require.Greaterf(t, end, start, "%s 缺少 catalog end marker", name)
	return text[start : end+len(capabilityCatalogEnd)]
}

func TestCapabilityCatalogBaselinesContainCompleteThreeDialectSchema(t *testing.T) {
	for _, name := range capabilityCatalogBaselines {
		t.Run(name, func(t *testing.T) {
			block := baselineCatalogBlock(t, name)
			for _, token := range []string{
				"sys_openapi_capability_group",
				"sys_openapi_capability",
				"sys_openapi_operation",
				"sys_openapi_release",
				"sys_openapi_release_item",
				"sys_openapi_runtime_state",
				"uk_openapi_capability_scope",
				"uk_openapi_operation_route",
				"snapshot_json",
				"adapter_contract_version",
				"sys_api_id",
			} {
				require.Containsf(t, block, token, "%s 的 catalog baseline 缺少 %s", name, token)
			}
			require.NotContains(t, block, "references sys_openapi_client_scope")
			require.NotContains(t, block, "uk_openapi_release_scope", "a capability may expose multiple operations in one release")
		})
	}
}

func TestCapabilityCatalogBaselinesUseTheSameSchemaMarkers(t *testing.T) {
	for _, name := range capabilityCatalogBaselines {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database", name))
			require.NoError(t, err)
			text := strings.ToLower(string(body))
			require.Equal(t, 1, strings.Count(text, capabilityCatalogBegin), "%s 的 begin marker 必须唯一", name)
			require.Equal(t, 1, strings.Count(text, capabilityCatalogEnd), "%s 的 end marker 必须唯一", name)
		})
	}
}

func TestCapabilityCatalogBaselineMergePointIsCommentOnly(t *testing.T) {
	dir := filepath.Join("../../../resource/database/gb28181/migrations")
	body, err := os.ReadFile(filepath.Join(dir, "2026-09-22-baseline-merge-point.sql"))
	require.NoError(t, err)
	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		require.Truef(t, strings.HasPrefix(trimmed, "--"), "合并点标记必须是纯注释，发现可执行内容 %q", trimmed)
	}
}

func TestCapabilityCatalogIncrementalMigrationsContainRuntimeSchema(t *testing.T) {
	dir := filepath.Join("../../../resource/database/gb28181/migrations")
	files := []string{
		"2026-09-21-openapi-capability-catalog.sql",
		"2026-09-21-openapi-capability-catalog-postgresql.sql",
		"2026-09-21-openapi-capability-catalog-sqlserver.sql",
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(dir, name))
			require.NoError(t, err)
			normalized := strings.NewReplacer("`", "", "[", "", "]", "").Replace(strings.ToLower(string(body)))
			normalized = strings.Join(strings.Fields(normalized), " ")

			for _, table := range []string{
				"sys_openapi_capability_group",
				"sys_openapi_capability",
				"sys_openapi_operation",
				"sys_openapi_release",
				"sys_openapi_release_item",
				"sys_openapi_runtime_state",
			} {
				require.Containsf(t, normalized, table, "%s 缺少 catalog 表", name)
			}
			for _, column := range []string{
				"group_id", "scope", "sys_api_id", "sys_api_path", "capability_id",
				"external_path", "adapter_key", "adapter_contract_version", "request_schema",
				"response_schema", "version", "snapshot_hash", "parent_release_id",
				"rollback_of_id", "release_id", "operation_id", "group_code", "capability_code",
				"snapshot_json", "active_release_id", "active_version", "runtime_epoch",
			} {
				require.Containsf(t, normalized, column, "%s 缺少 catalog 字段 %s", name, column)
			}
			for _, constraint := range []string{
				"uk_openapi_capability_group_code",
				"uk_openapi_capability_scope",
				"uk_openapi_operation_route",
				"uk_openapi_operation_capability_code",
				"uk_openapi_release_version",
				"uk_openapi_release_item_route",
			} {
				require.Containsf(t, normalized, constraint, "%s 缺少 catalog 约束 %s", name, constraint)
			}
			require.Contains(t, normalized, "primary key")
			require.Contains(t, normalized, "sys_api")
			require.Contains(t, normalized, "sys_menu_api")
			require.Contains(t, normalized, "sys_casbin_rule")
			require.Contains(t, normalized, "not exists")
		})
	}
}

func TestCapabilityCatalogIncrementalDownDropsTablesBeforeRoutePermissions(t *testing.T) {
	dir := filepath.Join("../../../resource/database/gb28181/migrations")
	for _, name := range []string{
		"2026-09-21-openapi-capability-catalog-down.sql",
		"2026-09-21-openapi-capability-catalog-postgresql-down.sql",
		"2026-09-21-openapi-capability-catalog-sqlserver-down.sql",
	} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(dir, name))
			require.NoError(t, err)
			normalized := strings.NewReplacer("`", "", "[", "", "]", "").Replace(strings.ToLower(string(body)))
			normalized = strings.Join(strings.Fields(normalized), " ")
			firstRouteMutation := len(normalized)
			for _, token := range []string{"delete from sys_casbin_rule", "delete from sys_menu_api", "delete from sys_api"} {
				index := strings.Index(normalized, token)
				require.GreaterOrEqualf(t, index, 0, "%s 缺少路由权限清理语句 %s", name, token)
				if index < firstRouteMutation {
					firstRouteMutation = index
				}
			}
			for _, table := range []string{
				"sys_openapi_release_item",
				"sys_openapi_runtime_state",
				"sys_openapi_release",
				"sys_openapi_operation",
				"sys_openapi_capability",
				"sys_openapi_capability_group",
			} {
				dropIndex := strings.Index(normalized, table)
				require.GreaterOrEqualf(t, dropIndex, 0, "%s 缺少目录表 %s", name, table)
				require.Lessf(t, dropIndex, firstRouteMutation, "%s 必须先删除目录表再处理 sys_api 路由", name)
			}
		})
	}
}
