package migration

import (
	"testing"

	"github.com/stretchr/testify/require"
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

func TestDownFileName(t *testing.T) {
	require.Equal(t, "2026-07-20-a-down.sql", DownFileName("2026-07-20-a.sql"))
	require.Equal(t, "2026-07-20-a-postgresql-down.sql", DownFileName("2026-07-20-a-postgresql.sql"))
	require.Equal(t, "2026-07-20-a-sqlserver-down.sql", DownFileName("2026-07-20-a-sqlserver.sql"))
}

// ⭐ 防回归:历史迁移归档清理后 embed 里不再有 migrations 目录。
// 启动链路必须把「目录不存在」当成「无增量迁移」,而不是把它当启动故障 ——
// 曾经的写法直接 ReadDir 并把 error 往上抛,结果是后端起不来,而报错被收敛成
// *fmt.wrapError 不带原文,只能看到 `class=unknown type=*fmt.wrapError`。
// 契约:runner.go 的 embedSource.UpFiles 是启动链路唯一的迁移目录入口。
func TestEmbedSourceUpFilesToleratesArchivedMigrationsDir(t *testing.T) {
	for _, dialect := range []Dialect{DialectMySQL, DialectPostgres, DialectSQLServer} {
		t.Run(string(dialect), func(t *testing.T) {
			got, err := (&embedSource{dialect: dialect}).UpFiles()
			require.NoError(t, err, "migrations 目录缺失时 UpFiles 必须返回空列表而不是错误")
			require.Empty(t, got)
		})
	}
}
