package models

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type currentSSRCTestConfig struct{}

func (currentSSRCTestConfig) ConfigFileChangeListen(...func()) {}
func (currentSSRCTestConfig) Get(string) interface{}           { return nil }
func (currentSSRCTestConfig) GetString(key string) string      { return "mysql" }
func (currentSSRCTestConfig) GetBool(string) bool              { return false }
func (currentSSRCTestConfig) GetInt(string) int                { return 0 }
func (currentSSRCTestConfig) GetInt32(string) int32            { return 0 }
func (currentSSRCTestConfig) GetInt64(string) int64            { return 0 }
func (currentSSRCTestConfig) GetFloat64(string) float64        { return 0 }
func (currentSSRCTestConfig) GetDuration(string) time.Duration { return 0 }
func (currentSSRCTestConfig) GetStringSlice(string) []string   { return nil }
func (currentSSRCTestConfig) GetUintSlice(string) []uint       { return nil }
func (currentSSRCTestConfig) Set(string, interface{})          {}
func (currentSSRCTestConfig) SaveConfig() error                { return nil }

func TestGbChannelCurrentSSRCModelContract(t *testing.T) {
	field, ok := reflect.TypeOf(GbChannel{}).FieldByName("CurrentSSRC")
	require.True(t, ok)
	require.Equal(t, "currentSsrc", field.Tag.Get("json"))
	gormTag := field.Tag.Get("gorm")
	require.Contains(t, gormTag, "column:current_ssrc")
	require.Contains(t, gormTag, "size:10")
	require.Contains(t, gormTag, "not null")
	require.Contains(t, gormTag, "default:''")
}

func TestChannelCurrentSSRCAtomicCAS(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&GbChannel{}))
	oldDB, oldConfig := app.GormDbMysql, app.ConfigYml
	app.GormDbMysql = db
	app.ConfigYml = currentSSRCTestConfig{}
	t.Cleanup(func() {
		app.GormDbMysql = oldDB
		app.ConfigYml = oldConfig
	})

	ctx := context.Background()
	const deviceID = "34020000001320000001"
	const channelID = "34020000001310000001"
	const streamID = "34020000001320000001_34020000001310000001"
	require.NoError(t, db.Create(&GbChannel{DeviceID: deviceID, ChannelID: channelID}).Error)

	require.NoError(t, SetChannelCurrent(ctx, deviceID, channelID, streamID, "0200000001"))
	got, err := FindChannel(ctx, deviceID, channelID)
	require.NoError(t, err)
	require.Equal(t, streamID, got.StreamID)
	require.Equal(t, "0200000001", got.CurrentSSRC)

	affected, err := ClearChannelCurrentIfCurrent(ctx, streamID, "0200000000")
	require.NoError(t, err)
	require.False(t, affected)
	got, err = FindChannel(ctx, deviceID, channelID)
	require.NoError(t, err)
	require.Equal(t, streamID, got.StreamID)
	require.Equal(t, "0200000001", got.CurrentSSRC)

	affected, err = ClearChannelCurrentIfCurrent(ctx, streamID, "0200000001")
	require.NoError(t, err)
	require.True(t, affected)
	got, err = FindChannel(ctx, deviceID, channelID)
	require.NoError(t, err)
	require.Empty(t, got.StreamID)
	require.Empty(t, got.CurrentSSRC)
}

func TestCurrentSSRCMigrationContracts(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	migrations := []struct {
		name string
		up   string
		down string
	}{
		{name: "mysql", up: "2026-08-10-fixed-address-current-ssrc.sql", down: "2026-08-10-fixed-address-current-ssrc-down.sql"},
		{name: "postgresql", up: "2026-08-10-fixed-address-current-ssrc-postgresql.sql", down: "2026-08-10-fixed-address-current-ssrc-postgresql-down.sql"},
		{name: "sqlserver", up: "2026-08-10-fixed-address-current-ssrc-sqlserver.sql", down: "2026-08-10-fixed-address-current-ssrc-sqlserver-down.sql"},
	}
	for _, migration := range migrations {
		t.Run(migration.name, func(t *testing.T) {
			upPath := filepath.Join(root, "resource", "database", "gb28181", "migrations", migration.up)
			downPath := filepath.Join(root, "resource", "database", "gb28181", "migrations", migration.down)
			up, err := os.ReadFile(upPath)
			require.NoError(t, err)
			down, err := os.ReadFile(downPath)
			require.NoError(t, err)
			upText := strings.ToLower(string(up))
			downText := strings.ToLower(string(down))
			require.Contains(t, upText, "current_ssrc")
			require.Contains(t, upText, "varchar(10)")
			require.Contains(t, upText, "default")
			require.Contains(t, downText, "current_ssrc")
		})
	}
}
