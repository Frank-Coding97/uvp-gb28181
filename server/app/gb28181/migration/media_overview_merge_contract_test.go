package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	migrationsfs "uvplatform.cn/uvp-gb28181/resource/database/gb28181"
)

func TestMediaOverviewMergeMigrationsKeepOneVisibleOverview(t *testing.T) {
	files := []string{
		"2026-08-30-zlm-overview-merge.sql",
		"2026-08-30-zlm-overview-merge-postgresql.sql",
		"2026-08-30-zlm-overview-merge-sqlserver.sql",
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			body, err := migrationsfs.FS.ReadFile("migrations/" + name)
			require.NoError(t, err)
			normalized := strings.NewReplacer("`", "", "[", "", "]", "").Replace(strings.ToLower(string(body)))
			normalized = strings.Join(strings.Fields(normalized), " ")
			require.Contains(t, normalized, "path='/gb28181/zlm/overview'")
			require.Contains(t, normalized, "总览")
			require.Contains(t, normalized, "path='/gb28181/zlm/runtime'")
			require.Regexp(t, `hide=(1|true)`, normalized)
			require.Contains(t, normalized, "/api/gb28181/zlm/nodes/:id/runtime")
			require.Contains(t, normalized, "sys_menu_api")
			require.Contains(t, normalized, "sys_role_menu")
			require.Contains(t, normalized, "sys_casbin_rule")
			require.Contains(t, normalized, "not exists")
		})
	}
}

func TestMediaOverviewMergeFreshBaselinesMatchVisibleMenu(t *testing.T) {
	files := []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("..", "..", "..", "resource", "database", name))
			require.NoError(t, err)
			normalized := strings.NewReplacer("`", "", "[", "", "]", "").Replace(strings.ToLower(string(body)))
			normalized = strings.Join(strings.Fields(normalized), " ")
			require.Contains(t, normalized, "zlm-overview-merge:start")
			require.Contains(t, normalized, "/api/gb28181/zlm/nodes/:id/runtime")
			require.Regexp(t, `hide=(1|true)`, normalized)
		})
	}
}
