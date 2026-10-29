package migration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var mixedNames = []string{
	"2026-07-20-b.sql",
	"2026-07-20-a-postgresql.sql",
	"2026-07-20-a-sqlserver.sql",
	"2026-07-20-a-sqlite.sql",
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

func TestFilterUpFilesSQLite(t *testing.T) {
	got := FilterUpFiles(mixedNames, DialectSQLite)
	require.Equal(t, []string{"2026-07-20-a-sqlite.sql"}, got)
}

func TestFilterUpFilesExcludesNonSQL(t *testing.T) {
	names := append([]string{}, mixedNames...)
	names = append(names, "remove_sip_log_new_menu.go", "run-merge-sip-log.go")
	got := FilterUpFiles(names, DialectMySQL)
	require.Equal(t, []string{"2026-07-20-a.sql", "2026-07-20-b.sql"}, got)
}

func TestDownFileName(t *testing.T) {
	require.Equal(t, "2026-07-20-a-down.sql", DownFileName("2026-07-20-a.sql"))
	require.Equal(t, "2026-07-20-a-postgresql-down.sql", DownFileName("2026-07-20-a-postgresql.sql"))
	require.Equal(t, "2026-07-20-a-sqlserver-down.sql", DownFileName("2026-07-20-a-sqlserver.sql"))
}

func TestEmbedSourceUpFilesIncludesPasswordStateMigrationByDialect(t *testing.T) {
	base := "2026-10-08-initial-admin-password-change"
	for _, tc := range []struct {
		dialect Dialect
		want    string
	}{
		{DialectMySQL, base + ".sql"},
		{DialectPostgres, base + "-postgresql.sql"},
		{DialectSQLServer, base + "-sqlserver.sql"},
		{DialectSQLite, base + "-sqlite.sql"},
	} {
		t.Run(string(tc.dialect), func(t *testing.T) {
			got, err := (&embedSource{dialect: tc.dialect}).UpFiles()
			require.NoError(t, err)
			suffix := strings.TrimPrefix(tc.want, base)
			require.Equal(t, []string{
				tc.want,
				"2026-10-08-meta-node-hook-ip" + suffix,
				"2026-10-08-sip-platform-hook-ip" + suffix,
				"2026-10-08-sip-platform-stream-ip" + suffix,
			}, got)
		})
	}
}
