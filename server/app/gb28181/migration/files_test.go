package migration

import (
	"testing"

	"github.com/stretchr/testify/require"

	migrationsfs "uvplatform.cn/uvp-gb28181/resource/database/gb28181"
)

var mixedNames = []string{
	"2026-07-20-b.sql",
	"2026-07-20-a-postgresql.sql",
	"2026-07-20-a-sqlserver.sql",
	"2026-07-20-a-down.sql",
	"2026-07-20-a.sql",
}

func TestFilterUpFilesMySQL(t *testing.T) {
	got := FilterUpFiles(mixedNames, DialectMySQL)
	require.Equal(t, []string{"2026-07-20-a.sql", "2026-07-20-b.sql"}, got)
}

func TestFilterUpFilesPostgres(t *testing.T) {
	got := FilterUpFiles(mixedNames, DialectPostgres)
	require.Equal(t, []string{"2026-07-20-a-postgresql.sql"}, got)
}

func TestFilterUpFilesSQLServer(t *testing.T) {
	got := FilterUpFiles(mixedNames, DialectSQLServer)
	require.Equal(t, []string{"2026-07-20-a-sqlserver.sql"}, got)
}

func TestFilterUpFilesExcludesNonSQL(t *testing.T) {
	names := append([]string{}, mixedNames...)
	names = append(names, "remove_sip_log_new_menu.go", "run-merge-sip-log.go")
	got := FilterUpFiles(names, DialectMySQL)
	require.Equal(t, []string{"2026-07-20-a.sql", "2026-07-20-b.sql"}, got)
}

func TestFilterUpFilesIncludesBaselineMergePointForEveryDialect(t *testing.T) {
	for _, dialect := range []Dialect{DialectMySQL, DialectPostgres, DialectSQLServer} {
		t.Run(string(dialect), func(t *testing.T) {
			require.Equal(t, []string{baselineMergePointFile}, FilterUpFiles([]string{baselineMergePointFile}, dialect))
		})
	}
}

func TestDownFileName(t *testing.T) {
	require.Equal(t, "2026-07-20-a-down.sql", DownFileName("2026-07-20-a.sql"))
	require.Equal(t, "2026-07-20-a-postgresql-down.sql", DownFileName("2026-07-20-a-postgresql.sql"))
	require.Equal(t, "2026-07-20-a-sqlserver-down.sql", DownFileName("2026-07-20-a-sqlserver.sql"))
}

func TestEmbeddedMigrationsComplete(t *testing.T) {
	entries, err := migrationsfs.FS.ReadDir("migrations")
	require.NoError(t, err)
	require.NotEmpty(t, entries, "embed 的迁移目录为空")
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	require.Contains(t, names, baselineMergePointFile, "baseline-only runner 必须嵌入合并点标记")
}
