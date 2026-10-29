package migration

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCascadeSnapshotSessionMigrationsCoverEveryDialect(t *testing.T) {
	files := []struct {
		dialect Dialect
		up      string
		down    string
	}{
		{DialectMySQL, "2026-10-23-cascade-snapshot-session.sql", "2026-10-23-cascade-snapshot-session-down.sql"},
		{DialectPostgres, "2026-10-23-cascade-snapshot-session-postgresql.sql", "2026-10-23-cascade-snapshot-session-postgresql-down.sql"},
		{DialectSQLServer, "2026-10-23-cascade-snapshot-session-sqlserver.sql", "2026-10-23-cascade-snapshot-session-sqlserver-down.sql"},
		{DialectSQLite, "2026-10-23-cascade-snapshot-session-sqlite.sql", "2026-10-23-cascade-snapshot-session-sqlite-down.sql"},
	}
	for _, file := range files {
		t.Run(string(file.dialect), func(t *testing.T) {
			up, err := (&embedSource{dialect: file.dialect}).ReadSQL(file.up)
			require.NoError(t, err)
			if file.dialect == DialectSQLServer {
				require.Len(t, splitStatements(up), 2, "runner must execute complete conditional create and index statements")
			}
			require.Contains(t, up, "source_device_epoch", "capture completion must remain bound to its original device registration")
			for _, field := range []string{"request_key", "request_fingerprint", "request_result", "uk_cascade_snapshot_request"} {
				require.Contains(t, up, field)
			}
			_, err = (&embedSource{dialect: file.dialect}).ReadSQL(file.down)
			require.NoError(t, err)
			require.Equal(t, []string{"gb_cascade_snapshot_session"}, baselineTableMigrations[file.up].tables)
		})
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	up, err := (&embedSource{dialect: DialectSQLite}).ReadSQL("2026-10-23-cascade-snapshot-session-sqlite.sql")
	require.NoError(t, err)
	executor := &dbExecutor{db: db}
	require.NoError(t, executor.ExecSQL(up))
	require.NoError(t, executor.ExecSQL(up), "additive migration must be repeatable")
	columns := "(platform_id, upstream_session_id, request_key, request_fingerprint, published_channel_id, source_device_id, source_channel_id, config_revision, projection_revision, state, expires_at, created_at, updated_at)"
	for _, values := range []string{"(1,'sid','request-a','hash','channel-a',10,20,1,1,'pending','2030-01-01','2026-01-01','2026-01-01')", "(2,'sid','request-a','hash','channel-b',11,21,1,1,'pending','2030-01-01','2026-01-01','2026-01-01')"} {
		require.NoError(t, db.Exec("INSERT INTO gb_cascade_snapshot_session "+columns+" VALUES "+values).Error)
	}
	require.Error(t, db.Exec("INSERT INTO gb_cascade_snapshot_session "+columns+" VALUES (1,'sid','request-b','hash','channel-c',12,22,1,1,'pending','2030-01-01','2026-01-01','2026-01-01')").Error)
	require.Error(t, db.Exec("INSERT INTO gb_cascade_snapshot_session "+columns+" VALUES (1,'another-sid','request-a','hash','channel-c',12,22,1,1,'pending','2030-01-01','2026-01-01','2026-01-01')").Error, "a transaction cannot be rebound to a new SessionID")
}
