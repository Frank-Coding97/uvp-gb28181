package models_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	cascademodel "uvplatform.cn/uvp-gb28181/app/gb28181/cascade/model"
)

func TestCascadeModelsAutoMigrateAndUniqueConstraints(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&cascademodel.GbCascadePlatform{},
		&cascademodel.GbCascadeDeviceProjection{},
		&cascademodel.GbCascadeChannelProjection{},
		&cascademodel.GbCascadeMediaSession{},
	))

	for _, model := range []interface{}{
		&cascademodel.GbCascadePlatform{},
		&cascademodel.GbCascadeDeviceProjection{},
		&cascademodel.GbCascadeChannelProjection{},
		&cascademodel.GbCascadeMediaSession{},
	} {
		require.True(t, db.Migrator().HasTable(model))
	}
	for _, column := range []string{
		"secret_nonce", "secret_ciphertext", "secret_alg", "secret_key_version",
		"config_revision", "register_expires_at", "heartbeat_at", "deleted_at",
	} {
		require.True(t, db.Migrator().HasColumn(&cascademodel.GbCascadePlatform{}, column), column)
	}
	for _, column := range []string{"dialog_key", "state", "sender_ssrc", "closed_at"} {
		require.True(t, db.Migrator().HasColumn(&cascademodel.GbCascadeMediaSession{}, column), column)
	}

	platform := cascademodel.GbCascadePlatform{
		Name: "upstream-a", UpstreamServerID: "34020000002000000001", UpstreamDomain: "3402000000",
		Host: "192.0.2.1", Port: 5060, LocalDeviceID: "34020000001320000001", LocalDomain: "3402000000",
		LocalSIPIP: "192.0.2.2", LocalSIPPort: 5060,
	}
	require.NoError(t, db.Create(&platform).Error)
	duplicatePlatform := platform
	duplicatePlatform.ID = 0
	duplicatePlatform.Name = "upstream-b"
	require.Error(t, db.Create(&duplicatePlatform).Error, "local SIP identity must be unique")

	device := cascademodel.GbCascadeDeviceProjection{PlatformID: platform.ID, SourceDeviceID: 1, PublishedDeviceID: "34020000001320000011", Active: true}
	require.NoError(t, db.Create(&device).Error)
	duplicateDevice := device
	duplicateDevice.ID = 0
	duplicateDevice.PublishedDeviceID = "34020000001320000012"
	require.Error(t, db.Create(&duplicateDevice).Error, "a source device can be projected once per platform")
	require.NoError(t, db.Delete(&device).Error)
	var activeDeviceCount int64
	require.NoError(t, db.Model(&cascademodel.GbCascadeDeviceProjection{}).Count(&activeDeviceCount).Error)
	require.Zero(t, activeDeviceCount, "projection deletion must preserve history through GORM soft delete")
	var historicalDeviceCount int64
	require.NoError(t, db.Unscoped().Model(&cascademodel.GbCascadeDeviceProjection{}).Count(&historicalDeviceCount).Error)
	require.EqualValues(t, 1, historicalDeviceCount)

	channel := cascademodel.GbCascadeChannelProjection{PlatformID: platform.ID, DeviceProjectionID: device.ID, SourceChannelID: 2, PublishedChannelID: "34020000001320000021", Active: true}
	require.NoError(t, db.Create(&channel).Error)
	duplicateChannel := channel
	duplicateChannel.ID = 0
	duplicateChannel.SourceChannelID = 3
	require.Error(t, db.Create(&duplicateChannel).Error, "a published channel ID must be unique per platform")

	session := cascademodel.GbCascadeMediaSession{PlatformID: platform.ID, DialogKey: "call-a/from-a/to-a", CallID: "call-a", State: cascademodel.CascadeMediaSessionStateReceived}
	require.NoError(t, db.Create(&session).Error)
	duplicateSession := session
	duplicateSession.ID = 0
	require.Error(t, db.Create(&duplicateSession).Error, "dialog keys must remain unique for idempotent invite handling")
}

func TestCascadeMigrationContracts(t *testing.T) {
	root := cascadeDDLServerRoot(t)
	for _, name := range []string{
		"2026-08-10-gb-cascade.sql",
		"2026-08-10-gb-cascade-postgresql.sql",
		"2026-08-10-gb-cascade-sqlserver.sql",
	} {
		body, err := os.ReadFile(filepath.Join(root, "resource/database/gb28181/migrations", name))
		require.NoError(t, err, name)
		text := strings.ToLower(string(body))
		for _, token := range []string{
			"gb_cascade_platform", "gb_cascade_device_projection", "gb_cascade_channel_projection", "gb_cascade_media_session",
			"secret_nonce", "secret_ciphertext", "secret_alg", "secret_key_version",
			"config_revision", "deleted_at", "dialog_key", "state",
			"uk_cascade_platform_name", "uk_cascade_platform_local_identity",
			"uk_cascade_device_source", "uk_cascade_device_published",
			"uk_cascade_channel_source", "uk_cascade_channel_published", "uk_cascade_media_dialog",
		} {
			require.Contains(t, text, token, name)
		}
		require.NotContains(t, text, "foreign key", name)
		switch name {
		case "2026-08-10-gb-cascade.sql":
			require.Contains(t, text, "create table if not exists", name)
		case "2026-08-10-gb-cascade-postgresql.sql":
			require.Contains(t, text, "create table if not exists", name)
			require.Contains(t, text, "create index if not exists", name)
		case "2026-08-10-gb-cascade-sqlserver.sql":
			require.Contains(t, text, "object_id(", name)
		}
	}
}

func cascadeDDLServerRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}
