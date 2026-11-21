package migration

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var snapshotRoutingMigrations = []string{"01-downstream-id", "02-upload-token", "03-upstream-url", "04-unique-downstream", "05-unique-upload"}

func TestCascadeSnapshotRoutingMigrations(t *testing.T) {
	for _, dialect := range []Dialect{DialectMySQL, DialectPostgres, DialectSQLServer, DialectSQLite} {
		t.Run(string(dialect), func(t *testing.T) {
			suffix := map[Dialect]string{DialectMySQL: "", DialectPostgres: "-postgresql", DialectSQLServer: "-sqlserver", DialectSQLite: "-sqlite"}[dialect]
			source := &embedSource{dialect: dialect}
			files, err := source.UpFiles()
			require.NoError(t, err)
			for _, part := range snapshotRoutingMigrations {
				name := "2026-10-23-cascade-snapshot-upload-" + part + suffix + ".sql"
				up, err := source.ReadSQL(name)
				require.NoError(t, err)
				require.Contains(t, files, name)
				_, err = source.ReadSQL(DownFileName(name))
				require.NoError(t, err)
				if dialect == DialectSQLServer {
					require.Len(t, splitStatements(up), 1, "conditional DDL must remain one executable statement")
				}
				if dialect == DialectSQLServer && strings.Contains(part, "unique-") {
					require.Contains(t, up, "IS NOT NULL", "SQL Server must allow multiple legacy NULL routing values")
				}
			}
		})
	}
}

func TestCascadeSnapshotRoutingSQLitePreservesLegacyAndResumes(t *testing.T) {
	for _, state := range []string{"no capture table", "legacy table", "interrupted upgrade", "interrupted with ledger", "fresh baseline"} {
		t.Run(state, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			raw, err := db.DB()
			require.NoError(t, err)
			raw.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = raw.Close() })
			source := &embedSource{dialect: DialectSQLite}
			createName := "2026-10-23-cascade-snapshot-session-sqlite.sql"
			create, err := source.ReadSQL(createName)
			require.NoError(t, err)
			executor := &dbExecutor{db: db}
			if state == "fresh baseline" {
				baseline, err := os.ReadFile("../../../resource/database/sqlitebaseline/baseline.sql")
				require.NoError(t, err)
				for _, statement := range splitStatements(string(baseline)) {
					if strings.Contains(statement, `"gb_cascade_snapshot_session"`) {
						require.NoError(t, db.Exec(statement).Error)
					}
				}
				for _, column := range []string{"downstream_session_id", "upload_token", "upstream_upload_url"} {
					require.True(t, db.Migrator().HasColumn("gb_cascade_snapshot_session", column), "fresh baseline must include routing column %s", column)
				}
				for _, index := range []string{"uk_cascade_snapshot_downstream", "uk_cascade_snapshot_upload"} {
					require.True(t, db.Migrator().HasIndex("gb_cascade_snapshot_session", index))
				}
			} else if state != "no capture table" {
				require.NoError(t, executor.ExecSQL(create))
			}
			if state != "no capture table" {
				require.NoError(t, db.Exec(`INSERT INTO gb_cascade_snapshot_session (platform_id, upstream_session_id, request_key, request_fingerprint, published_channel_id, source_device_id, source_channel_id, config_revision, projection_revision, expires_at, created_at, updated_at) VALUES (1,'legacy-one','req-one','hash','channel',10,20,1,1,'2030-01-01','2026-01-01','2026-01-01'),(2,'legacy-two','req-two','hash','channel',10,20,1,1,'2030-01-01','2026-01-01','2026-01-01')`).Error)
			}
			store, lock, fakeSource, _ := newFakes()
			fakeSource.files[createName] = create
			for _, part := range snapshotRoutingMigrations {
				name := "2026-10-23-cascade-snapshot-upload-" + part + "-sqlite.sql"
				body, err := source.ReadSQL(name)
				require.NoError(t, err)
				fakeSource.files[name] = body
				if strings.HasPrefix(state, "interrupted") && part == "01-downstream-id" {
					require.NoError(t, executor.ExecSQL(body))
				}
			}
			probe := schemaProbe{
				hasIndex: func(table, index string) (bool, error) { return db.Migrator().HasIndex(table, index), nil },
				hasTable: func(table string) (bool, error) {
					if table == "gb_cascade_snapshot_session" {
						return db.Migrator().HasTable(table), nil
					}
					return true, nil
				},
				hasColumn: func(table, column string) (bool, error) { return db.Migrator().HasColumn(table, column), nil },
			}
			for attempt := range 3 {
				if attempt != 1 {
					store.applied = map[string]bool{}
				}
				if state == "interrupted with ledger" {
					store.applied["older.sql"] = true
				}
				require.NoError(t, run(store, lock, fakeSource, executor, probe))
			}
			for _, column := range []string{"downstream_session_id", "upload_token", "upstream_upload_url"} {
				require.True(t, db.Migrator().HasColumn("gb_cascade_snapshot_session", column))
			}
			var count int64
			require.NoError(t, db.Table("gb_cascade_snapshot_session").Where("downstream_session_id IS NULL AND upload_token IS NULL").Count(&count).Error)
			if state != "no capture table" {
				require.EqualValues(t, 2, count)
			}
			if state == "no capture table" {
				require.NoError(t, db.Exec(`INSERT INTO gb_cascade_snapshot_session (platform_id, upstream_session_id, request_key, request_fingerprint, published_channel_id, source_device_id, source_channel_id, config_revision, projection_revision, expires_at, created_at, updated_at) VALUES (1,'a','a','hash','channel',10,20,1,1,'2030-01-01','2026-01-01','2026-01-01'),(2,'b','b','hash','channel',10,20,1,1,'2030-01-01','2026-01-01','2026-01-01')`).Error)
			}
			for _, column := range []string{"downstream_session_id", "upload_token"} {
				require.NoError(t, db.Exec("UPDATE gb_cascade_snapshot_session SET "+column+"='unique' WHERE platform_id=1").Error)
				require.Error(t, db.Exec("UPDATE gb_cascade_snapshot_session SET "+column+"='unique' WHERE platform_id=2").Error)
			}
		})
	}
}

func TestCascadeSnapshotRoutingSQLServerBaselineNullableUniqueness(t *testing.T) {
	body, err := os.ReadFile("../../../resource/database/sqlserver_converted.sql")
	require.NoError(t, err)
	for index, column := range map[string]string{"uk_cascade_snapshot_downstream": "downstream_session_id", "uk_cascade_snapshot_upload": "upload_token"} {
		pattern := `CREATE UNIQUE INDEX \[` + index + `\] ON \[gb_cascade_snapshot_session\] \(\[` + column + `\]\) WHERE \[` + column + `\] IS NOT NULL;`
		require.True(t, regexp.MustCompile(pattern).Match(body), "missing filtered unique index %s", index)
		require.False(t, strings.Contains(string(body), "CONSTRAINT ["+index+"] UNIQUE"), "legacy NULLs must remain allowed")
	}
}
