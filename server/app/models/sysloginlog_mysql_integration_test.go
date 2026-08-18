//go:build integration

package models_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/app/utils/ymlconfig"
)

func TestLoginLogMySQLSchemaAndPermissionSeed(t *testing.T) {
	if os.Getenv("UVP_RUN_MYSQL_INTEGRATION") != "1" {
		t.Skip("set UVP_RUN_MYSQL_INTEGRATION=1 to verify the configured development MySQL")
	}
	app.ConfigYml = ymlconfig.CreateYamlFactory("../../config")
	app.ZapLog = zap.NewNop()
	db, err := gormhelper.GetOneMysqlClient()
	require.NoError(t, err)
	rawDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = rawDB.Close() })
	require.NoError(t, rawDB.PingContext(context.Background()))
	require.True(t, db.Migrator().HasTable("sys_login_logs"))

	columns, err := db.Migrator().ColumnTypes("sys_login_logs")
	require.NoError(t, err)
	columnNames := make(map[string]bool, len(columns))
	for _, column := range columns {
		columnNames[column.Name()] = true
	}
	for _, name := range []string{"id", "user_id", "username", "result", "failure_reason", "ip", "location", "user_agent", "browser", "os", "created_at"} {
		require.Truef(t, columnNames[name], "missing sys_login_logs.%s", name)
	}

	assertMySQLCount(t, db, "SELECT COUNT(*) FROM gb_schema_migrations WHERE version = ?", 1, "2026-08-18-login-logs.sql")
	assertMySQLCount(t, db, "SELECT COUNT(*) FROM sys_api WHERE path IN (?, ?) AND method = 'GET' AND deleted_at IS NULL", 2, "/api/sysLoginLog/list", "/api/sysLoginLog/:id")
	assertMySQLCount(t, db, "SELECT COUNT(*) FROM sys_menu WHERE path = ? AND permission = ? AND deleted_at IS NULL", 1, "/system/login-log", "system:login-log:list")
	assertMySQLMinimum(t, db, "SELECT COUNT(*) FROM sys_role_menu rm JOIN sys_menu m ON m.id = rm.menu_id WHERE rm.role_id = 1 AND m.path = ?", 1, "/system/login-log")
	assertMySQLMinimum(t, db, "SELECT COUNT(*) FROM sys_menu_api ma JOIN sys_menu m ON m.id = ma.menu_id WHERE m.path = ?", 2, "/system/login-log")
	assertMySQLMinimum(t, db, "SELECT COUNT(*) FROM sys_casbin_rule WHERE v1 IN (?, ?) AND v2 = 'GET'", 2, "/api/sysLoginLog/list", "/api/sysLoginLog/:id")
}

type mysqlQueryer interface {
	Raw(string, ...interface{}) *gorm.DB
}

func assertMySQLCount(t *testing.T, db mysqlQueryer, query string, want int64, args ...interface{}) {
	t.Helper()
	var count int64
	require.NoError(t, db.Raw(query, args...).Scan(&count).Error)
	require.Equal(t, want, count)
}

func assertMySQLMinimum(t *testing.T, db mysqlQueryer, query string, minimum int64, args ...interface{}) {
	t.Helper()
	var count int64
	require.NoError(t, db.Raw(query, args...).Scan(&count).Error)
	require.GreaterOrEqual(t, count, minimum)
}
