package device

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	basemodels "uvplatform.cn/uvp-gb28181/app/models"
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
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbDeviceStatusEvent{}, &basemodels.SysDepartment{}))
	app.GormDbMysql = db
	app.ConfigYml = testConfig{}
	require.NoError(t, db.Create(&basemodels.SysDepartment{
		BaseModel: basemodels.BaseModel{ID: 1},
		Name:      "接入池",
	}).Error)

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
	assert.EqualValues(t, 1, got.OwnerDeptID)
}

func TestHandleRegister_RejectsMissingDefaultOwnerDeptConfig(t *testing.T) {
	prevDB := app.GormDbMysql
	prevConfig := app.ConfigYml
	t.Cleanup(func() {
		app.GormDbMysql = prevDB
		app.ConfigYml = prevConfig
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbDeviceStatusEvent{}, &basemodels.SysDepartment{}))
	app.GormDbMysql = db
	app.ConfigYml = testConfigWithoutDefaultDept{}

	_, err = HandleRegister(context.Background(), RegisterInfo{
		DeviceID:  "34020000002000000002",
		Transport: "UDP",
		IP:        "127.0.0.1",
		Port:      5060,
		Expires:   3600,
	}, 60)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "default_owner_dept_id")
}

func TestHandleRegister_PreallocationRejectsUnknownDevice(t *testing.T) {
	prevDB := app.GormDbMysql
	prevConfig := app.ConfigYml
	t.Cleanup(func() {
		app.GormDbMysql = prevDB
		app.ConfigYml = prevConfig
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbDeviceStatusEvent{}, &basemodels.SysDepartment{}))
	app.GormDbMysql = db
	app.ConfigYml = preallocationTestConfig{enabled: true}

	_, err = HandleRegister(context.Background(), RegisterInfo{DeviceID: "34020000002000000003", Expires: 3600}, 60)
	require.ErrorIs(t, err, ErrDeviceNotPreallocated)

	var count int64
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestHandleRegister_PreallocationAllowsExistingDevice(t *testing.T) {
	prevDB := app.GormDbMysql
	prevConfig := app.ConfigYml
	t.Cleanup(func() {
		app.GormDbMysql = prevDB
		app.ConfigYml = prevConfig
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbDeviceStatusEvent{}, &basemodels.SysDepartment{}))
	app.GormDbMysql = db
	app.ConfigYml = preallocationTestConfig{enabled: true}
	require.NoError(t, db.Create(&gbmodels.GbDevice{DeviceID: "34020000002000000004", Status: gbmodels.DeviceStatusOffline, OwnerDeptID: 9}).Error)

	_, err = HandleRegister(context.Background(), RegisterInfo{DeviceID: "34020000002000000004", Expires: 3600}, 60)
	require.NoError(t, err)

	var got gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", "34020000002000000004").First(&got).Error)
	require.Equal(t, gbmodels.DeviceStatusOnline, got.Status)
	require.EqualValues(t, 9, got.OwnerDeptID)
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
func (testConfig) GetBool(string) bool { return false }
func (testConfig) GetInt(key string) int {
	if key == "gb28181.device.default_owner_dept_id" {
		return 1
	}
	return 0
}
func (testConfig) GetInt32(string) int32            { return 0 }
func (testConfig) GetInt64(string) int64            { return 0 }
func (testConfig) GetFloat64(string) float64        { return 0 }
func (testConfig) GetDuration(string) time.Duration { return 0 }
func (testConfig) GetStringSlice(string) []string   { return nil }
func (testConfig) GetUintSlice(string) []uint       { return nil }
func (testConfig) Set(string, interface{})          {}
func (testConfig) SaveConfig() error                { return nil }

type testConfigWithoutDefaultDept struct{ testConfig }

func (testConfigWithoutDefaultDept) GetInt(string) int { return 0 }

type preallocationTestConfig struct {
	testConfig
	enabled bool
}

func (c preallocationTestConfig) Get(key string) interface{} {
	if key == "gb28181.device.preallocation_mode" {
		return c.enabled
	}
	return c.testConfig.Get(key)
}

func (c preallocationTestConfig) GetBool(key string) bool {
	return key == "gb28181.device.preallocation_mode" && c.enabled
}
