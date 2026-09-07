package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

type sqliteSystemTestConfig struct {
	strings map[string]string
	uints   map[string][]uint
}

func (c sqliteSystemTestConfig) ConfigFileChangeListen(...func()) {}
func (c sqliteSystemTestConfig) Get(key string) interface{} {
	if c.strings != nil {
		return c.strings[key]
	}
	return nil
}
func (c sqliteSystemTestConfig) GetString(key string) string {
	if c.strings != nil {
		return c.strings[key]
	}
	return ""
}
func (sqliteSystemTestConfig) GetBool(string) bool              { return false }
func (sqliteSystemTestConfig) GetInt(string) int                { return 0 }
func (sqliteSystemTestConfig) GetInt32(string) int32            { return 0 }
func (sqliteSystemTestConfig) GetInt64(string) int64            { return 0 }
func (sqliteSystemTestConfig) GetFloat64(string) float64        { return 0 }
func (sqliteSystemTestConfig) GetDuration(string) time.Duration { return 0 }
func (c sqliteSystemTestConfig) GetStringSlice(string) []string { return nil }
func (c sqliteSystemTestConfig) GetUintSlice(key string) []uint {
	if c.uints != nil {
		return c.uints[key]
	}
	return nil
}
func (sqliteSystemTestConfig) Set(string, interface{}) {}
func (sqliteSystemTestConfig) SaveConfig() error       { return nil }

func newSQLiteSystemTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return newSQLiteSystemTestDBAt(t, filepath.Join(t.TempDir(), "system.db"))
}

func newSQLiteSystemTestDBAt(t *testing.T, path string) *gorm.DB {
	t.Helper()
	oldDB, oldConfig, oldLog := app.GormDbSQLite, app.ConfigYml, app.ZapLog
	app.ConfigYml = sqliteSystemTestConfig{strings: map[string]string{
		"gormv2.usedbtype":    "sqlite",
		"casbin.tableprefix":  "",
		"casbin.tablename":    "sys_casbin_rule",
		"server.notcheckuser": "",
	}}
	app.ZapLog = zap.NewNop()
	db, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	_, err = sqlitebootstrap.Initialize(ctx, db)
	require.NoError(t, err)
	app.GormDbSQLite = db
	t.Cleanup(func() {
		raw, dbErr := db.DB()
		if dbErr == nil {
			_ = raw.Close()
		}
		app.GormDbSQLite, app.ConfigYml, app.ZapLog = oldDB, oldConfig, oldLog
	})
	return db
}
