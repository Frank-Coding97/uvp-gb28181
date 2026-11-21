package migration

import (
	"os"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var cascadeUpgradeColumns = map[string]string{
	"01-binding": "cascade_binding", "02-request-key": "cascade_request_key", "03-session-key": "cascade_session_key", "04-business-result": "cascade_business_result", "05-business-delivered": "cascade_business_delivered", "06-final-delivered": "cascade_final_delivered",
}

func TestCascadeUpgradeBindingMigrations(t *testing.T) {
	for _, dialect := range []Dialect{DialectMySQL, DialectPostgres, DialectSQLServer, DialectSQLite} {
		t.Run(string(dialect), func(t *testing.T) {
			source := &embedSource{dialect: dialect}
			suffix := map[Dialect]string{DialectMySQL: "", DialectPostgres: "-postgresql", DialectSQLServer: "-sqlserver", DialectSQLite: "-sqlite"}[dialect]
			files, err := source.UpFiles()
			require.NoError(t, err)
			for part, column := range cascadeUpgradeColumns {
				name := "2026-10-23-cascade-upgrade-binding-" + part + suffix + ".sql"
				require.Contains(t, files, name)
				body, err := source.ReadSQL(name)
				require.NoError(t, err)
				require.Contains(t, body, column)
				require.Equal(t, "gb_device_firmware_upgrade", baselineColumnMigrations[name].table)
				require.Equal(t, column, baselineColumnMigrations[name].column)
				down, err := source.ReadSQL(DownFileName(name))
				require.NoError(t, err)
				require.Len(t, splitStatements(body), 1)
				require.Len(t, splitStatements(down), 1)
			}
			for _, part := range []string{"07-unique-request", "08-unique-session"} {
				name := "2026-10-23-cascade-upgrade-binding-" + part + suffix + ".sql"
				require.Contains(t, files, name)
				body, err := source.ReadSQL(name)
				require.NoError(t, err)
				require.Len(t, splitStatements(body), 1)
				require.Equal(t, "gb_device_firmware_upgrade", baselineIndexMigrations[name].table)
				if dialect == DialectSQLServer {
					require.Contains(t, body, "IS NOT NULL")
				}
				_, err = source.ReadSQL(DownFileName(name))
				require.NoError(t, err)
			}
		})
	}
}

func TestCascadeUpgradeBindingSQLiteUpgradeAndFresh(t *testing.T) {
	source := &embedSource{dialect: DialectSQLite}
	files, err := source.UpFiles()
	require.NoError(t, err)
	for _, fresh := range []bool{false, true} {
		t.Run(map[bool]string{false: "upgrade", true: "fresh"}[fresh], func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			raw, err := db.DB()
			require.NoError(t, err)
			raw.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = raw.Close() })
			if fresh {
				body, err := os.ReadFile("../../../resource/database/sqlitebaseline/baseline.sql")
				require.NoError(t, err)
				for _, statement := range splitStatements(string(body)) {
					if strings.Contains(statement, `"gb_device_firmware_upgrade"`) {
						require.NoError(t, db.Exec(statement).Error)
					}
				}
			} else {
				require.NoError(t, db.Exec("CREATE TABLE gb_device_firmware_upgrade(id INTEGER PRIMARY KEY)").Error)
				require.NoError(t, db.Exec("INSERT INTO gb_device_firmware_upgrade(id) VALUES(1),(2)").Error)
				for _, name := range files {
					if !strings.HasPrefix(name, "2026-10-23-cascade-upgrade-binding-") {
						continue
					}
					body, err := source.ReadSQL(name)
					require.NoError(t, err)
					require.NoError(t, db.Exec(body).Error)
				}
				require.NoError(t, db.Exec("UPDATE gb_device_firmware_upgrade SET cascade_request_key='one', cascade_session_key='session' WHERE id=1").Error)
				require.Error(t, db.Exec("UPDATE gb_device_firmware_upgrade SET cascade_request_key='one' WHERE id=2").Error)
				require.Error(t, db.Exec("UPDATE gb_device_firmware_upgrade SET cascade_session_key='session' WHERE id=2").Error)
				var flags []bool
				require.NoError(t, db.Raw("SELECT cascade_business_delivered FROM gb_device_firmware_upgrade").Scan(&flags).Error)
				require.Equal(t, []bool{false, false}, flags)
			}
			for _, column := range cascadeUpgradeColumns {
				require.True(t, db.Migrator().HasColumn("gb_device_firmware_upgrade", column), column)
			}
		})
	}
}

func TestCascadeUpgradeBindingRunnerResumesWithoutReapplyingColumns(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		t.Run(map[bool]string{false: "old baseline", true: "interrupted with ledger"}[interrupted], func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			raw, err := db.DB()
			require.NoError(t, err)
			raw.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = raw.Close() })
			require.NoError(t, db.Exec("CREATE TABLE gb_device_firmware_upgrade(id INTEGER PRIMARY KEY)").Error)
			require.NoError(t, db.Exec("INSERT INTO gb_device_firmware_upgrade(id) VALUES(1),(2)").Error)
			source := &embedSource{dialect: DialectSQLite}
			files, err := source.UpFiles()
			require.NoError(t, err)
			store, lock, fakeSource, _ := newFakes()
			for _, name := range files {
				if !strings.HasPrefix(name, "2026-10-23-cascade-upgrade-binding-") {
					continue
				}
				body, err := source.ReadSQL(name)
				require.NoError(t, err)
				fakeSource.files[name] = body
				if interrupted && strings.Contains(name, "01-binding") {
					require.NoError(t, db.Exec(body).Error)
				}
			}
			probe := schemaProbe{
				hasTable: func(table string) (bool, error) {
					if table == "gb_device_firmware_upgrade" {
						return db.Migrator().HasTable(table), nil
					}
					return true, nil
				},
				hasColumn: func(table, column string) (bool, error) { return db.Migrator().HasColumn(table, column), nil },
				hasIndex:  func(table, index string) (bool, error) { return db.Migrator().HasIndex(table, index), nil },
			}
			for attempt := range 3 {
				if attempt != 1 {
					store.applied = map[string]bool{}
				}
				if interrupted {
					store.applied["older.sql"] = true
				}
				require.NoError(t, run(store, lock, fakeSource, &dbExecutor{db: db}, probe))
			}
			var count int64
			require.NoError(t, db.Table("gb_device_firmware_upgrade").Where("cascade_request_key IS NULL AND cascade_session_key IS NULL").Count(&count).Error)
			require.EqualValues(t, 2, count)
			for _, column := range cascadeUpgradeColumns {
				require.True(t, db.Migrator().HasColumn("gb_device_firmware_upgrade", column))
			}
		})
	}
}
