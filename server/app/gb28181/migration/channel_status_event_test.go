package migration

import (
	"os"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/catalog"
)

func TestChannelStatusEventMigrations(t *testing.T) {
	for _, dialect := range []Dialect{DialectMySQL, DialectPostgres, DialectSQLServer, DialectSQLite} {
		t.Run(string(dialect), func(t *testing.T) {
			suffix := map[Dialect]string{DialectMySQL: "", DialectPostgres: "-postgresql", DialectSQLServer: "-sqlserver", DialectSQLite: "-sqlite"}[dialect]
			name := "2026-10-23-channel-status-event" + suffix + ".sql"
			source := &embedSource{dialect: dialect}
			up, err := source.ReadSQL(name)
			require.NoError(t, err)
			require.Contains(t, up, "status_event")
			require.Equal(t, baselineColumnRequirement{table: "gb_channel", column: "status_event"}, baselineColumnMigrations[name])
			files, err := source.UpFiles()
			require.NoError(t, err)
			require.Contains(t, files, name)
			down, err := source.ReadSQL(DownFileName(name))
			require.NoError(t, err)
			require.Contains(t, down, "status_event")
			if dialect == DialectSQLServer {
				require.Len(t, splitStatements(up), 1)
				require.Len(t, splitStatements(down), 1)
				require.Contains(t, down, "sys.default_constraints", "rollback must find defaults from either fresh baseline or incremental migration")
				require.Contains(t, down, "QUOTENAME")
			}
		})
	}
}

func TestChannelStatusEventSQLiteUpgradeAndFreshBaseline(t *testing.T) {
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
				start := strings.Index(string(body), `CREATE TABLE IF NOT EXISTS "gb_channel" (`)
				require.NotEqual(t, -1, start)
				section := string(body)[start:]
				end := strings.Index(section, "\n);")
				require.NotEqual(t, -1, end)
				require.NoError(t, db.Exec(section[:end+3]).Error)
				require.True(t, db.Migrator().HasColumn("gb_channel", "status_event"), "fresh baseline must include the column before upgrades run")
			} else {
				require.NoError(t, db.Exec(`CREATE TABLE gb_channel (id INTEGER PRIMARY KEY, device_id TEXT, channel_id TEXT, owner_dept_id INTEGER, status INTEGER DEFAULT 0 CHECK(status IN (0,1)), updated_at DATETIME, deleted_at DATETIME)`).Error)
			}
			name := "2026-10-23-channel-status-event-sqlite.sql"
			up, err := (&embedSource{dialect: DialectSQLite}).ReadSQL(name)
			require.NoError(t, err)
			store, lock, source, _ := newFakes()
			source.files[name] = up
			probe := schemaProbe{hasTable: func(string) (bool, error) { return true, nil }, hasColumn: func(table, column string) (bool, error) { return db.Migrator().HasColumn(table, column), nil }}
			for range 2 {
				store.applied = make(map[string]bool)
				require.NoError(t, run(store, lock, source, &dbExecutor{db: db}, probe))
			}
			require.NoError(t, db.Exec(`INSERT INTO gb_channel (id,device_id,channel_id,owner_dept_id,status) VALUES (1,'device','channel',1,1)`).Error)
			pipeline := catalog.New(db)
			for _, action := range []string{"VLOST", "DEFECT", "OFF", "VLOST", "ON"} {
				require.NoError(t, pipeline.IngestDelta(t.Context(), catalog.Sender{OwnerDeptID: 1, SourceDeviceID: "device"}, action, catalog.CatalogItem{DeviceID: "channel"}))
				var row struct {
					Status      int
					StatusEvent string
				}
				require.NoError(t, db.Table("gb_channel").Select("status,status_event").Where("id=1").Take(&row).Error)
				expectedStatus, expectedEvent := 0, action
				if action == "ON" {
					expectedStatus = 1
					expectedEvent = ""
				}
				if action == "OFF" {
					expectedEvent = ""
				}
				require.Equal(t, expectedStatus, row.Status)
				require.Equal(t, expectedEvent, row.StatusEvent)
			}
			require.Error(t, db.Exec(`UPDATE gb_channel SET status=2 WHERE id=1`).Error, "online state retains its boolean contract")
		})
	}
}
