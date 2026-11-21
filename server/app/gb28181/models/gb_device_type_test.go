package models_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// 2026-11-16：设备类型（ipc/nvr/platform）由用户手动维护，默认 IPC。
// 本用例锁三件事：四方言迁移与四份快照都带该列、新建/自动建档默认落 ipc、
// 设备重新注册不会把用户手工维护的类型冲回 ipc。
func TestDeviceTypeDDLIsAvailableForEverySupportedDatabase(t *testing.T) {
	root := dualVersionDDLRoot(t)
	for _, migration := range []struct {
		name, up, down, guard string
	}{
		{"mysql", "2026-11-16-device-type.sql", "2026-11-16-device-type-down.sql", "information_schema.columns"},
		{"postgresql", "2026-11-16-device-type-postgresql.sql", "2026-11-16-device-type-postgresql-down.sql", "if not exists"},
		{"sqlserver", "2026-11-16-device-type-sqlserver.sql", "2026-11-16-device-type-sqlserver-down.sql", "col_length"},
		{"sqlite", "2026-11-16-device-type-sqlite.sql", "2026-11-16-device-type-sqlite-down.sql", "add column"},
	} {
		t.Run(migration.name, func(t *testing.T) {
			for _, file := range []string{migration.up, migration.down} {
				body, err := os.ReadFile(filepath.Join(root, "resource/database/gb28181/migrations", file))
				require.NoError(t, err)
				require.Contains(t, strings.ToLower(string(body)), "device_type", file)
			}
			body, err := os.ReadFile(filepath.Join(root, "resource/database/gb28181/migrations", migration.up))
			require.NoError(t, err)
			text := strings.ToLower(string(body))
			require.Contains(t, text, migration.guard, migration.up+" 必须幂等")
			require.Contains(t, text, "ipc", migration.up+" 必须带默认值 ipc")
		})
	}

	// 快照是新装环境的唯一来源：漏一次，新库就永远没有这一列。
	for _, file := range []string{
		"uvp-gb28181.sql", "postgresql_converted.sql", "sqlserver_converted.sql", "dameng_converted.sql",
	} {
		body, err := os.ReadFile(filepath.Join(root, "resource/database", file))
		require.NoError(t, err)
		text := strings.ToLower(string(body))
		require.Contains(t, text, "device_type", file)
		require.Contains(t, text, "'ipc'", file+" 的默认值必须是 ipc")
	}
}

func newDeviceTypeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}))
	return db
}

// 新建（含自动建档）不指定类型时必须落 ipc，而不是空串。
func TestDeviceTypeDefaultsToIPCOnCreate(t *testing.T) {
	db := newDeviceTypeTestDB(t)
	device := &gbmodels.GbDevice{DeviceID: "34020000002000000001", SubscribeCapability: gbmodels.SubscribeUnknown}
	require.NoError(t, db.Create(device).Error)
	require.Equal(t, gbmodels.DeviceTypeIPC, device.DeviceType)

	var stored string
	require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("device_id = ?", device.DeviceID).Pluck("device_type", &stored).Error)
	require.Equal(t, gbmodels.DeviceTypeIPC, stored)
}

// ⛔ 这是本功能最容易写坏的地方：设备每次重新注册都会走 Upsert 的更新分支，
// 那里 d.DeviceType 是零值。一旦把它写进 update，用户设的 NVR / 下级平台
// 会在下一次心跳/注册后悄悄变回 IPC。
func TestUpsertPreservesManuallyMaintainedDeviceType(t *testing.T) {
	db := newDeviceTypeTestDB(t)
	ctx := context.Background()

	seed := &gbmodels.GbDevice{DeviceID: "34020000002000000001", DeviceType: gbmodels.DeviceTypeNVR}
	require.NoError(t, db.Create(seed).Error)

	// 模拟注册续期：新构造的档案没有类型（零值），只能覆盖注册事实。
	reregister := &gbmodels.GbDevice{
		DeviceID:            seed.DeviceID,
		Transport:           "TCP",
		SubscribeCapability: gbmodels.SubscribeUnknown,
	}
	require.NoError(t, gbmodels.UpsertWithDB(ctx, db, reregister))

	var stored gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", seed.DeviceID).Limit(1).Find(&stored).Error)
	require.Equal(t, gbmodels.DeviceTypeNVR, stored.DeviceType, "重新注册不得覆盖用户维护的设备类型")
	require.Equal(t, "TCP", stored.Transport, "注册事实仍应更新")
}

func TestNormalizeDeviceType(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
		ok   bool
	}{
		{"", gbmodels.DeviceTypeIPC, true},
		{"ipc", gbmodels.DeviceTypeIPC, true},
		{" IPC ", gbmodels.DeviceTypeIPC, true},
		{"NVR", gbmodels.DeviceTypeNVR, true},
		{"platform", gbmodels.DeviceTypePlatform, true},
		{"Platform", gbmodels.DeviceTypePlatform, true},
	} {
		got, ok := gbmodels.NormalizeDeviceType(tc.raw)
		require.Equal(t, tc.ok, ok, tc.raw)
		require.Equal(t, tc.want, got, tc.raw)
	}

	for _, bad := range []string{"camera", "下级平台", "nvr2"} {
		got, ok := gbmodels.NormalizeDeviceType(bad)
		require.False(t, ok, bad)
		require.Empty(t, got, bad)
	}
}
