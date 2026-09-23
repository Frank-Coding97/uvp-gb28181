package models_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestAlarmResourceMigrationsAreIdempotent(t *testing.T) {
	root := dualVersionDDLRoot(t)
	migrations := []struct {
		name   string
		path   string
		guards []string
	}{
		{"mysql", "resource/database/gb28181/migrations/2026-07-25-gb-alarm-resource.sql", []string{"information_schema.columns", "information_schema.statistics", "create table if not exists"}},
		{"postgresql", "resource/database/gb28181/migrations/2026-07-25-gb-alarm-resource-postgresql.sql", []string{"add column if not exists", "create table if not exists", "create index if not exists"}},
		{"sqlserver", "resource/database/gb28181/migrations/2026-07-25-gb-alarm-resource-sqlserver.sql", []string{"col_length", "object_id", "sys.indexes"}},
	}
	for _, migration := range migrations {
		migration := migration
		t.Run(migration.name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, migration.path))
			require.NoError(t, err)
			text := normalizeAlarmDDL(string(body))
			for _, token := range []string{
				"gb_alarm_resource", "gb_alarm_resource_parent", "gb_alarm_binding",
				"alarm_resource_id", "parent_code", "raw_parent_ids",
			} {
				require.Contains(t, text, token)
			}
			for _, guard := range migration.guards {
				require.Contains(t, text, guard)
			}
			require.Contains(t, text, "(owner_dept_id, device_code, alarm_code)", "报警编码必须按来源设备隔离")
			require.Contains(t, text, "idx_alarm_resource_device_id", "device_id 查询必须有索引")
			require.Contains(t, text, "(device_id, channel_code)", "手工绑定必须按设备隔离")
		})
	}
	require.Contains(t, mustReadAlarmDDL(t, root, migrations[0].path), "information_schema.tables")
	require.Contains(t, mustReadAlarmDDL(t, root, migrations[1].path), "alter table if exists gb_catalog_node")
	require.Contains(t, mustReadAlarmDDL(t, root, migrations[2].path), "object_id(n'gb_catalog_node', n'u') is not null")
}

func TestAlarmResourceFullSchemasContainNewInstallBaseline(t *testing.T) {
	root := dualVersionDDLRoot(t)
	schemas := []struct {
		name string
		path string
	}{
		{"mysql-ptz", "resource/database/gb28181/gb_ptz.sql"},
		{"mysql-full", "resource/database/uvp-gb28181.sql"},
		{"postgresql-full", "resource/database/postgresql_converted.sql"},
		{"sqlserver-full", "resource/database/sqlserver_converted.sql"},
	}
	for _, schema := range schemas {
		schema := schema
		t.Run(schema.name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(root, schema.path))
			require.NoError(t, err)
			text := normalizeAlarmDDL(string(body))
			for _, token := range []string{
				"gb_alarm_resource", "gb_alarm_resource_parent", "gb_alarm_binding",
				"raw_parent_ids", "parent_code", "alarm_resource_id",
				"(owner_dept_id, device_code, alarm_code)",
				"idx_alarm_resource_device_id", "(device_id, channel_code)",
			} {
				require.Contains(t, text, token)
			}
		})
	}
}

func TestCatalogBaselineLinksAlarmResources(t *testing.T) {
	root := dualVersionDDLRoot(t)
	text := mustReadAlarmDDL(t, root, "resource/database/gb28181/migrations/2026-06-26-catalog-b-plus.sql")
	catalogTable := ddlTableSection(text, "gb_catalog_node")
	require.Contains(t, catalogTable, "alarm_resource_id")
	require.Contains(t, catalogTable, "idx_catalog_alarm_resource")
}

func TestAlarmResourceIdentityAndQueriesAreDeviceScoped(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbAlarmResource{}))

	const alarmCode = "34020000001340000001"
	first := gbmodels.GbAlarmResource{
		OwnerDeptID: 1, DeviceID: 11, DeviceCode: "34020000001180000001",
		AlarmCode: alarmCode, ResourceType: gbmodels.AlarmResourceInput,
	}
	second := gbmodels.GbAlarmResource{
		OwnerDeptID: 1, DeviceID: 22, DeviceCode: "34020000001180000002",
		AlarmCode: alarmCode, ResourceType: gbmodels.AlarmResourceInput,
	}
	require.NoError(t, db.Create(&first).Error)
	require.NoError(t, db.Create(&second).Error, "不同来源设备可上报相同报警编码")

	duplicate := first
	duplicate.ID = 0
	require.Error(t, db.Create(&duplicate).Error, "同一来源设备的报警编码必须唯一")

	var resources []gbmodels.GbAlarmResource
	require.NoError(t, db.Where("device_id = ?", second.DeviceID).Find(&resources).Error)
	require.Len(t, resources, 1)
	require.Equal(t, second.DeviceCode, resources[0].DeviceCode)
}

func normalizeAlarmDDL(body string) string {
	replacer := strings.NewReplacer(
		"`", "", "[", "", "]", "", "\"", "",
		"\r", " ", "\n", " ", "\t", " ",
	)
	return strings.Join(strings.Fields(strings.ToLower(replacer.Replace(body))), " ")
}

func mustReadAlarmDDL(t *testing.T, root, path string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, path))
	require.NoError(t, err)
	return normalizeAlarmDDL(string(body))
}
