package migration

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// newUnconnectedMySQLDB 构造一个不真正连接的 MySQL GORM 实例,
// 仅用于方言判定与注入透传测试(直接填 Dialector,gorm.Open 会在
// Initialize 阶段发起真实连接,测试环境无库,故绕过)。
func newUnconnectedMySQLDB(t *testing.T) *gorm.DB {
	t.Helper()
	return &gorm.DB{
		Config: &gorm.Config{Dialector: mysql.New(mysql.Config{})},
	}
}

// 4.1:成功透传
func TestRunMigrationsSuccess(t *testing.T) {
	old := runUp
	defer func() { runUp = old }()

	var gotDialect Dialect
	runUp = func(_ *gorm.DB, d Dialect) error {
		gotDialect = d
		return nil
	}

	err := RunMigrations(map[string]*gorm.DB{"mysql": newUnconnectedMySQLDB(t)})
	require.NoError(t, err)
	require.Equal(t, DialectMySQL, gotDialect)
}

// 4.2:失败透传(调用处 log.Fatal)
func TestRunMigrationsErrorPassthrough(t *testing.T) {
	old := runUp
	defer func() { runUp = old }()

	boom := errors.New("migration boom")
	runUp = func(_ *gorm.DB, _ Dialect) error { return boom }

	err := RunMigrations(map[string]*gorm.DB{"mysql": newUnconnectedMySQLDB(t)})
	require.ErrorIs(t, err, boom)
	require.Contains(t, err.Error(), "mysql", "错误应含数据库名")
}

// nil 连接被忽略,不调用 runUp
func TestRunMigrationsSkipsNil(t *testing.T) {
	old := runUp
	defer func() { runUp = old }()

	called := 0
	runUp = func(_ *gorm.DB, _ Dialect) error {
		called++
		return nil
	}

	err := RunMigrations(map[string]*gorm.DB{"mysql": nil})
	require.NoError(t, err)
	require.Zero(t, called)
}
