package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAPIClientMenuIsIncludedInFullInitialization(t *testing.T) {
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("../../../resource/database", name))
			require.NoError(t, err)
			text := string(body)
			require.Contains(t, text, openAPIClientMenuPath)
			require.Contains(t, text, openAPIClientMenuName)
			require.Contains(t, text, openAPIClientMenuComponent)
			for _, button := range openAPIClientMenuButtons {
				require.Contains(t, text, button.permission)
			}
			apiIndex := strings.Index(text, "/api/gb28181/openapi-clients")
			menuIndex := strings.Index(text, openAPIClientMenuPath)
			require.GreaterOrEqual(t, apiIndex, 0)
			require.Greater(t, menuIndex, apiIndex, "管理 API 种子必须先于菜单种子写入")
		})
	}
}
