package models_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAlarmManagementMigrationsContainIndexMenuAndPermissions(t *testing.T) {
	root := dualVersionDDLRoot(t)
	migrations := []struct {
		name string
		up   string
		down string
	}{
		{"mysql", "resource/database/gb28181/migrations/2026-08-04-alarm-management.sql", "resource/database/gb28181/migrations/2026-08-04-alarm-management-down.sql"},
		{"postgresql", "resource/database/gb28181/migrations/2026-08-04-alarm-management-postgresql.sql", "resource/database/gb28181/migrations/2026-08-04-alarm-management-postgresql-down.sql"},
		{"sqlserver", "resource/database/gb28181/migrations/2026-08-04-alarm-management-sqlserver.sql", "resource/database/gb28181/migrations/2026-08-04-alarm-management-sqlserver-down.sql"},
	}

	for _, migration := range migrations {
		migration := migration
		t.Run(migration.name, func(t *testing.T) {
			up := readNormalizedAlarmManagementDDL(t, root, migration.up)
			for _, token := range []string{
				"idx_alarm_time", "alarm_time", "/gb28181/alarm-management",
				"gb28181/alarm-management/index", "lucide:bellring",
				"gb28181:alarm:view", "gb28181:alarm:delete",
				"/api/gb28181/alarms", "/api/gb28181/alarms/:id",
				"/api/gb28181/alarms/batch-delete", "get", "delete", "post",
				"sys_menu_api", "sys_role_menu", "sys_casbin_rule", "role_1",
			} {
				require.Contains(t, up, token)
			}
			require.Contains(t, up, "device-mgmt", "父菜单和默认查看角色必须从设备管理菜单继承")
			require.Contains(t, up, "not exists", "up migration 必须可重复执行")

			down := readNormalizedAlarmManagementDDL(t, root, migration.down)
			for _, token := range []string{
				"idx_alarm_time", "/gb28181/alarm-management", "gb28181:alarm:view",
				"gb28181:alarm:delete", "/api/gb28181/alarms", "sys_casbin_rule",
			} {
				require.Contains(t, down, token)
			}
			require.NotContains(t, down, "delete from gb_alarm_event", "down 不得删除告警业务数据")
			require.NotContains(t, down, "drop table gb_alarm_event", "down 不得删除告警业务表")
		})
	}
}

func TestAlarmManagementBaselinesDeclareAlarmTimeIndex(t *testing.T) {
	root := dualVersionDDLRoot(t)
	modelBody, err := os.ReadFile(filepath.Join(root, "app/gb28181/models/gb_subscription.go"))
	require.NoError(t, err)
	require.Contains(t, strings.ToLower(string(modelBody)), "index:idx_alarm_time")

	baseline := readNormalizedAlarmManagementDDL(t, root, "resource/database/gb28181/gb_subscription.sql")
	require.Contains(t, baseline, "idx_alarm_time")
	require.Contains(t, baseline, "alarm_time")
}

func TestAlarmInfoBackfillMigrationsAreBoundedAndIdempotent(t *testing.T) {
	root := dualVersionDDLRoot(t)
	paths := []string{
		"resource/database/gb28181/migrations/2026-08-05-alarm-info-backfill.sql",
		"resource/database/gb28181/migrations/2026-08-05-alarm-info-backfill-postgresql.sql",
		"resource/database/gb28181/migrations/2026-08-05-alarm-info-backfill-sqlserver.sql",
	}
	for _, path := range paths {
		body := readNormalizedAlarmManagementDDL(t, root, path)
		for _, token := range []string{
			"update", "gb_alarm_event", "alarm_type = 0", "raw_summary",
			"<info>", "<alarmtype>", "</alarmtype>",
			"method = 2", "between 1 and 5",
			"method = 5", "between 1 and 13",
			"method = 6", "between 1 and 2",
		} {
			require.Contains(t, body, token, path)
		}
		require.NotContains(t, body, "delete from gb_alarm_event", path)
	}
}

func readNormalizedAlarmManagementDDL(t *testing.T, root, path string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, path))
	require.NoError(t, err)
	replacer := strings.NewReplacer("`", "", "[", "", "]", "", "\"", "", "\r", " ", "\n", " ", "\t", " ")
	return strings.Join(strings.Fields(strings.ToLower(replacer.Replace(string(body)))), " ")
}
