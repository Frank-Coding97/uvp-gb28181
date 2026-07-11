package device

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

func TestHandleRegister_AutoCreatesDevice(t *testing.T) {
	prevDB := app.GormDbMysql
	prevConfig := app.ConfigYml
	t.Cleanup(func() {
		app.GormDbMysql = prevDB
		app.ConfigYml = prevConfig
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}))
	app.GormDbMysql = db
	app.ConfigYml = testConfig{}

	_, err = HandleRegister(context.Background(), RegisterInfo{
		DeviceID:  "34020000002000000001",
		Transport: "UDP",
		IP:        "127.0.0.1",
		Port:      5060,
		Expires:   3600,
	}, 60)
	require.NoError(t, err)

	var got gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", "34020000002000000001").First(&got).Error)
	assert.Equal(t, gbmodels.DeviceStatusOnline, got.Status)
}

type testConfig struct{}

func (testConfig) ConfigFileChangeListen(...func()) {}
func (testConfig) Get(string) interface{}           { return nil }
func (testConfig) GetString(key string) string {
	if key == "gormv2.usedbtype" {
		return "mysql"
	}
	return ""
}
func (testConfig) GetBool(string) bool              { return false }
func (testConfig) GetInt(string) int                { return 0 }
func (testConfig) GetInt32(string) int32            { return 0 }
func (testConfig) GetInt64(string) int64            { return 0 }
func (testConfig) GetFloat64(string) float64        { return 0 }
func (testConfig) GetDuration(string) time.Duration { return 0 }
func (testConfig) GetStringSlice(string) []string   { return nil }
func (testConfig) GetUintSlice(string) []uint       { return nil }
func (testConfig) Set(string, interface{})          {}
func (testConfig) SaveConfig() error                { return nil }
