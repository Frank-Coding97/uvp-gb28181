package migration

import (
	"testing"

	dameng "github.com/godoes/gorm-dameng"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlserver"
)

func TestDialectOf(t *testing.T) {
	require.Equal(t, DialectMySQL, DialectOf(&mysql.Dialector{Config: &mysql.Config{}}))
	require.Equal(t, DialectPostgres, DialectOf(&postgres.Dialector{Config: &postgres.Config{}}))
	require.Equal(t, DialectSQLServer, DialectOf(&sqlserver.Dialector{Config: &sqlserver.Config{}}))
}

// 国产库只新增了达梦一个方言。金仓与 PolarDB **必须**继续被判成
// postgres / mysql —— 它们靠这一条复用已验证的迁移集，一旦这里改成
// 独立方言，就要凭空多出两批内容相同的 SQL 文件。
func TestDialectOfDomesticDatabases(t *testing.T) {
	require.Equal(t, DialectDM, DialectOf(dameng.Open("dm://SYSDBA:x@127.0.0.1:5236?schema=SYSDBA")))

	// 金仓走 postgres 驱动、PolarDB 走 mysql 驱动，因此这里传的就是那两个
	// dialector —— 断言的是「复用关系不会被悄悄改掉」。
	require.Equal(t, DialectPostgres, DialectOf(postgres.Open("host=127.0.0.1 port=54321 sslmode=disable")),
		"金仓必须继续判定为 postgres，否则 -postgresql.sql 迁移集会失效")
	require.Equal(t, DialectMySQL, DialectOf(mysql.Open("u:p@tcp(127.0.0.1:3306)/db")),
		"PolarDB(MySQL 版) 必须继续判定为 mysql")
}

func TestDialectOfUnknown(t *testing.T) {
	require.Equal(t, DialectUnknown, DialectOf(nil))
}
