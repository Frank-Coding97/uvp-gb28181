package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	migrationsfs "uvplatform.cn/uvp-gb28181/resource/database/gb28181"
)

const schedulerPermissionParentMigration = "2026-09-17-scheduler-permission-parent"

func TestSchedulerPermissionMovesUnderNodeManagement(t *testing.T) {
	for _, suffix := range []string{".sql", "-postgresql.sql", "-sqlserver.sql"} {
		name := schedulerPermissionParentMigration + suffix
		t.Run(name, func(t *testing.T) {
			body, err := migrationsfs.FS.ReadFile("migrations/" + name)
			require.NoError(t, err)
			normalized := normalizeSchedulerPermissionSQL(body)

			require.Contains(t, normalized, "gb28181:zlm:scheduler:manage")
			require.Contains(t, normalized, "path='/media/nodes'")
			require.Contains(t, normalized, "path='/media/scheduling'")
			require.Contains(t, normalized, "title='调度日志'")
			require.NotContains(t, normalized, "delete from sys_role_menu")
			require.NotContains(t, normalized, "insert into sys_role_menu")
			require.NotContains(t, normalized, "delete from sys_menu_api")
		})
	}
}

func TestSchedulerPermissionParentRollbackRestoresSchedulingMenu(t *testing.T) {
	for _, suffix := range []string{"-down.sql", "-postgresql-down.sql", "-sqlserver-down.sql"} {
		name := schedulerPermissionParentMigration + suffix
		t.Run(name, func(t *testing.T) {
			body, err := migrationsfs.FS.ReadFile("migrations/" + name)
			require.NoError(t, err)
			normalized := normalizeSchedulerPermissionSQL(body)

			require.Contains(t, normalized, "gb28181:zlm:scheduler:manage")
			require.Contains(t, normalized, "path='/media/scheduling'")
			require.Contains(t, normalized, "title='调度管理'")
			require.NotContains(t, normalized, "delete from sys_role_menu")
			require.NotContains(t, normalized, "insert into sys_role_menu")
		})
	}
}

func TestSchedulerPermissionParentFreshBaselinesMatchFinalState(t *testing.T) {
	for _, name := range []string{"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("..", "..", "..", "resource", "database", name))
			require.NoError(t, err)
			normalized := normalizeSchedulerPermissionSQL(body)
			start := strings.LastIndex(normalized, "scheduler-permission-parent:start")
			require.GreaterOrEqual(t, start, 0)
			section := normalized[start:]

			require.Contains(t, section, "gb28181:zlm:scheduler:manage")
			require.Contains(t, section, "path='/media/nodes'")
			require.Contains(t, section, "path='/media/scheduling'")
			require.Contains(t, section, "title='调度日志'")
		})
	}
}

func normalizeSchedulerPermissionSQL(body []byte) string {
	normalized := strings.NewReplacer("`", "", "[", "", "]", "", "n'", "'").Replace(strings.ToLower(string(body)))
	return strings.Join(strings.Fields(normalized), " ")
}
