package migration

import (
	"os"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCascadeConfigPermissionMigration(t *testing.T) {
	for _, dialect := range []Dialect{DialectMySQL, DialectPostgres, DialectSQLServer, DialectSQLite} {
		t.Run(string(dialect), func(t *testing.T) {
			suffix := map[Dialect]string{DialectMySQL: "", DialectPostgres: "-postgresql", DialectSQLServer: "-sqlserver", DialectSQLite: "-sqlite"}[dialect]
			name := "2026-10-23-cascade-config-enabled" + suffix + ".sql"
			source := &embedSource{dialect: dialect}
			body, err := source.ReadSQL(name)
			require.NoError(t, err)
			require.Contains(t, body, "config_enabled")
			files, err := source.UpFiles()
			require.NoError(t, err)
			require.Contains(t, files, name)
			require.Equal(t, baselineColumnRequirement{table: "gb_cascade_platform", column: "config_enabled"}, baselineColumnMigrations[name])
			_, err = source.ReadSQL(DownFileName(name))
			require.NoError(t, err)
		})
	}
}

func TestCascadeConfigPermissionSQLiteUpgradeAndFreshBaseline(t *testing.T) {
	for _, fresh := range []bool{false, true} {
		t.Run(map[bool]string{false: "upgrade", true: "baseline"}[fresh], func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			raw, err := db.DB()
			require.NoError(t, err)
			raw.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = raw.Close() })
			if fresh {
				body, err := os.ReadFile("../../../resource/database/sqlitebaseline/baseline.sql")
				require.NoError(t, err)
				start := strings.Index(string(body), `CREATE TABLE IF NOT EXISTS "gb_cascade_platform" (`)
				require.NotEqual(t, -1, start)
				section := string(body)[start:]
				end := strings.Index(section, "\n);")
				require.NotEqual(t, -1, end)
				require.NoError(t, db.Exec(section[:end+3]).Error)
				require.NoError(t, db.Exec(`INSERT INTO gb_cascade_platform (name,upstream_server_id,upstream_domain,host,port,local_device_id,local_domain,local_sip_ip,local_sip_port,ptz_enabled,created_at,updated_at) VALUES ('legacy','34020000002000000001','3402000000','192.0.2.1',5060,'34020000002000000009','3402000000','192.0.2.2',5060,1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`).Error)
			} else {
				require.NoError(t, db.Exec("CREATE TABLE gb_cascade_platform (id INTEGER PRIMARY KEY, name TEXT, ptz_enabled INTEGER NOT NULL DEFAULT 0)").Error)
				require.NoError(t, db.Exec("INSERT INTO gb_cascade_platform VALUES(1,'legacy',1)").Error)
			}
			name := "2026-10-23-cascade-config-enabled-sqlite.sql"
			source := &embedSource{dialect: DialectSQLite}
			body, err := source.ReadSQL(name)
			require.NoError(t, err)
			store, lock, fakeSource, _ := newFakes()
			fakeSource.files[name] = body
			probe := schemaProbe{hasTable: func(string) (bool, error) { return true, nil }, hasColumn: func(table, column string) (bool, error) { return db.Migrator().HasColumn(table, column), nil }}
			for range 2 {
				store.applied = make(map[string]bool)
				require.NoError(t, run(store, lock, fakeSource, &dbExecutor{db: db}, probe))
			}
			var row struct {
				Name                      string
				PTZEnabled, ConfigEnabled bool
			}
			require.NoError(t, db.Table("gb_cascade_platform").First(&row).Error)
			require.Equal(t, "legacy", row.Name)
			require.True(t, row.PTZEnabled)
			require.False(t, row.ConfigEnabled, "existing PTZ grant must not authorize configuration writes")
			for _, enabled := range []bool{true, false} {
				require.NoError(t, db.Table("gb_cascade_platform").Where("id=?", 1).Update("config_enabled", enabled).Error)
				require.NoError(t, db.Table("gb_cascade_platform").First(&row).Error)
				require.Equal(t, enabled, row.ConfigEnabled)
			}
		})
	}
}

func TestCascadeDeviceControlPermissionMigrationDefaultsToDenied(t *testing.T) {
	for _, dialect := range []Dialect{DialectMySQL, DialectPostgres, DialectSQLServer, DialectSQLite} {
		suffix := map[Dialect]string{DialectMySQL: "", DialectPostgres: "-postgresql", DialectSQLServer: "-sqlserver", DialectSQLite: "-sqlite"}[dialect]
		name := "2026-10-23-cascade-device-control-enabled" + suffix + ".sql"
		source := &embedSource{dialect: dialect}
		body, err := source.ReadSQL(name)
		require.NoError(t, err)
		require.Contains(t, body, "device_control_enabled")
		files, err := source.UpFiles()
		require.NoError(t, err)
		require.Contains(t, files, name)
		require.Equal(t, baselineColumnRequirement{table: "gb_cascade_platform", column: "device_control_enabled"}, baselineColumnMigrations[name])
		_, err = source.ReadSQL(DownFileName(name))
		require.NoError(t, err)
	}
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.Exec("CREATE TABLE gb_cascade_platform (id INTEGER PRIMARY KEY, name TEXT, ptz_enabled INTEGER NOT NULL DEFAULT 0)").Error)
	require.NoError(t, db.Exec("INSERT INTO gb_cascade_platform VALUES(1,'legacy',1)").Error)
	name := "2026-10-23-cascade-device-control-enabled-sqlite.sql"
	source := &embedSource{dialect: DialectSQLite}
	body, err := source.ReadSQL(name)
	require.NoError(t, err)
	store, lock, fakeSource, _ := newFakes()
	fakeSource.files[name] = body
	probe := schemaProbe{hasTable: func(string) (bool, error) { return true, nil }, hasColumn: func(table, column string) (bool, error) { return db.Migrator().HasColumn(table, column), nil }}
	require.NoError(t, run(store, lock, fakeSource, &dbExecutor{db: db}, probe))
	var row struct{ PTZEnabled, DeviceControlEnabled bool }
	require.NoError(t, db.Table("gb_cascade_platform").First(&row).Error)
	require.True(t, row.PTZEnabled)
	require.False(t, row.DeviceControlEnabled)
}

func TestCascadeMaintenancePermissionMigrationDefaultsToDenied(t *testing.T) {
	for _, dialect := range []Dialect{DialectMySQL, DialectPostgres, DialectSQLServer, DialectSQLite} {
		suffix := map[Dialect]string{DialectMySQL: "", DialectPostgres: "-postgresql", DialectSQLServer: "-sqlserver", DialectSQLite: "-sqlite"}[dialect]
		name := "2026-10-23-cascade-maintenance-enabled" + suffix + ".sql"
		source := &embedSource{dialect: dialect}
		body, err := source.ReadSQL(name)
		require.NoError(t, err)
		require.Contains(t, body, "maintenance_enabled")
		files, err := source.UpFiles()
		require.NoError(t, err)
		require.Contains(t, files, name)
		require.Equal(t, baselineColumnRequirement{table: "gb_cascade_platform", column: "maintenance_enabled"}, baselineColumnMigrations[name])
		_, err = source.ReadSQL(DownFileName(name))
		require.NoError(t, err)
	}
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.Exec("CREATE TABLE gb_cascade_platform (id INTEGER PRIMARY KEY, name TEXT)").Error)
	require.NoError(t, db.Exec("INSERT INTO gb_cascade_platform VALUES(1,'legacy')").Error)
	name := "2026-10-23-cascade-maintenance-enabled-sqlite.sql"
	source := &embedSource{dialect: DialectSQLite}
	body, err := source.ReadSQL(name)
	require.NoError(t, err)
	store, lock, fakeSource, _ := newFakes()
	fakeSource.files[name] = body
	probe := schemaProbe{hasTable: func(string) (bool, error) { return true, nil }, hasColumn: func(table, column string) (bool, error) { return db.Migrator().HasColumn(table, column), nil }}
	require.NoError(t, run(store, lock, fakeSource, &dbExecutor{db: db}, probe))
	var row struct {
		Name               string
		MaintenanceEnabled bool
	}
	require.NoError(t, db.Table("gb_cascade_platform").First(&row).Error)
	require.Equal(t, "legacy", row.Name)
	require.False(t, row.MaintenanceEnabled)
}
