package models

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGBMenuFlattenMigrations(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	serverRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))

	for _, filename := range []string{
		"2026-08-09-flatten-gb-menu.sql",
		"2026-08-09-flatten-gb-menu-postgresql.sql",
		"2026-08-09-flatten-gb-menu-sqlserver.sql",
	} {
		t.Run(filename, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(serverRoot, "resource", "database", "gb28181", "migrations", filename))
			require.NoError(t, err)
			sql := strings.ToLower(string(body))

			for _, token := range []string{
				"/home",
				"/media",
				"/gb28181",
				"/security-preview",
				"parent_id",
				"deleted_at",
				"lucide:cctv",
				"lucide:server",
				"lucide:workflow",
				"lucide:history",
				"lucide:router",
				"lucide:filetext",
				"lucide:monitorplay",
				"lucide:bellring",
			} {
				require.Contains(t, sql, token)
			}
			require.Contains(t, sql, "= 0")
			require.Contains(t, sql, "= 1")
			require.Contains(t, sql, "= 9")
			require.NotContains(t, sql, "140350", "migration must resolve the obsolete directory by path")
		})
	}
}
