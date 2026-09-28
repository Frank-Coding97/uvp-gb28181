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
