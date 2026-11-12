package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestJobResultSummaryMigrationScriptsAreAdditive(t *testing.T) {
	dir := "../../../resource/database/gb28181/migrations"
	upFiles := []string{
		"2026-10-09-job-result-summary.sql",
		"2026-10-09-job-result-summary-postgresql.sql",
		"2026-10-09-job-result-summary-sqlserver.sql",
		"2026-10-09-job-result-summary-sqlite.sql",
	}
	for _, name := range upFiles {
		body, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		sqlText := strings.ToLower(string(body))
		require.Contains(t, sqlText, "sys_job_results")
		require.Contains(t, sqlText, "summary")
		require.NotContains(t, sqlText, "drop table")
		require.NotContains(t, sqlText, "delete from")
	}

	downFiles := []string{
		"2026-10-09-job-result-summary-down.sql",
		"2026-10-09-job-result-summary-postgresql-down.sql",
		"2026-10-09-job-result-summary-sqlserver-down.sql",
		"2026-10-09-job-result-summary-sqlite-down.sql",
	}
	for _, name := range downFiles {
		body, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		sqlText := strings.ToLower(string(body))
		require.Contains(t, sqlText, "summary")
		require.Contains(t, sqlText, "drop column")
		require.NotContains(t, sqlText, "drop table")
		require.NotContains(t, sqlText, "delete from")
	}
}

func TestFullDatabaseSchemasIncludeJobResultSummary(t *testing.T) {
	tests := []struct {
		file string
		want string
	}{
		{"../../../resource/database/uvp-gb28181.sql", "`summary` text COLLATE utf8mb4_unicode_ci NULL"},
		{"../../../resource/database/postgresql_converted.sql", `"summary" TEXT`},
		{"../../../resource/database/sqlserver_converted.sql", "[summary] NVARCHAR(MAX)"},
	}
	for _, tt := range tests {
		t.Run(filepath.Base(tt.file), func(t *testing.T) {
			body, err := os.ReadFile(tt.file)
			require.NoError(t, err)
			require.Contains(t, string(body), tt.want)
		})
	}
}

func TestJobResultSummaryMigrationsAreEmbeddedForEveryDialect(t *testing.T) {
	for _, dialect := range []Dialect{DialectMySQL, DialectPostgres, DialectSQLServer, DialectSQLite} {
		names, err := (&embedSource{dialect: dialect}).UpFiles()
		require.NoError(t, err)
		require.Contains(t, names, migrationFileForDialect(dialect))
	}
}

func migrationFileForDialect(dialect Dialect) string {
	switch dialect {
	case DialectPostgres:
		return "2026-10-09-job-result-summary-postgresql.sql"
	case DialectSQLServer:
		return "2026-10-09-job-result-summary-sqlserver.sql"
	case DialectSQLite:
		return "2026-10-09-job-result-summary-sqlite.sql"
	default:
		return "2026-10-09-job-result-summary.sql"
	}
}

func TestSQLiteJobResultSummaryMigrationPreservesExistingRowsAndSupportsRunnerRetry(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE TABLE sys_job_results (id INTEGER PRIMARY KEY, status TEXT NOT NULL)").Error)
	require.NoError(t, db.Exec("INSERT INTO sys_job_results (id, status) VALUES (1, 'SUCCESS')").Error)

	dir := "../../../resource/database/gb28181/migrations"
	upName := "2026-10-09-job-result-summary-sqlite.sql"
	up, err := os.ReadFile(filepath.Join(dir, upName))
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(up)).Error)
	require.True(t, db.Migrator().HasColumn("sys_job_results", "summary"))

	store, lock, src, exec := newFakes()
	store.applied["previous.sql"] = true
	src.files[upName] = string(up)
	require.NoError(t, run(store, lock, src, exec, schemaProbe{
		hasTable: func(name string) (bool, error) { return db.Migrator().HasTable(name), nil },
		hasColumn: func(table, column string) (bool, error) {
			return db.Migrator().HasColumn(table, column), nil
		},
	}))
	require.Empty(t, exec.executed)
	require.True(t, store.applied[upName])

	var existing struct {
		ID      int
		Status  string
		Summary *string
	}
	require.NoError(t, db.Table("sys_job_results").First(&existing, 1).Error)
	require.Equal(t, 1, existing.ID)
	require.Equal(t, "SUCCESS", existing.Status)
	require.Nil(t, existing.Summary)

	storeDB := NewStore(db)
	require.NoError(t, storeDB.EnsureTable())
	require.NoError(t, storeDB.MarkApplied([]string{upName}))
	require.NoError(t, Down(db, DialectSQLite, upName))
	require.False(t, db.Migrator().HasColumn("sys_job_results", "summary"))
	require.NoError(t, db.Table("sys_job_results").First(&existing, 1).Error)
	require.NoError(t, Down(db, DialectSQLite, upName), "repeating down should be harmless")
	require.NoError(t, db.Table("sys_job_results").First(&existing, 1).Error)
}
