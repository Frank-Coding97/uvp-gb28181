package models_test

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestDualVersionModelsExposeProfileSnapshotAndControlState(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbDevice{},
		&gbmodels.GbPTZOperation{},
		&gbmodels.GbDeviceControlState{},
	))

	for _, column := range []string{
		"reported_version", "reported_version_at", "protocol_override",
		"effective_version", "effective_version_source", "effective_version_at",
	} {
		require.Truef(t, db.Migrator().HasColumn(&gbmodels.GbDevice{}, column), "missing gb_device column %s", column)
	}
	for _, column := range []string{"profile_version", "profile_charset", "target_scope", "target_code", "scope_key"} {
		require.Truef(t, db.Migrator().HasColumn(&gbmodels.GbPTZOperation{}, column), "missing operation snapshot column %s", column)
	}
	require.True(t, db.Migrator().HasTable(&gbmodels.GbDeviceControlState{}))
	for _, column := range []string{"device_id", "target_scope", "target_code", "record_state", "guard_state", "freshness", "observed_at", "source", "source_operation_seq"} {
		require.Truef(t, db.Migrator().HasColumn(&gbmodels.GbDeviceControlState{}, column), "missing state column %s", column)
	}
	requirePTZIndexColumns(t, db, &gbmodels.GbDeviceControlState{}, "idx_control_state_device_target", []string{"device_id", "target_scope", "target_code"})
}

func TestControlStateUniqueKeyIncludesDevice(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDeviceControlState{}))

	now := time.Now().UTC()
	for _, deviceID := range []uint{1, 2} {
		require.NoError(t, db.Create(&gbmodels.GbDeviceControlState{
			DeviceID: deviceID, TargetScope: gbmodels.ControlTargetScopeChannel,
			TargetCode: "same-target", ObservedAt: now,
		}).Error)
	}
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbDeviceControlState{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
}

func TestDualVersionModelDefaultsPreserveUnknownState(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbDeviceControlState{}))

	device := &gbmodels.GbDevice{DeviceID: "34020000001320000001", Name: "legacy"}
	require.NoError(t, db.Create(device).Error)
	var gotDevice gbmodels.GbDevice
	require.NoError(t, db.First(&gotDevice, device.ID).Error)
	require.Equal(t, gbmodels.ProtocolVersion2016, gotDevice.EffectiveVersion)
	require.Equal(t, gbmodels.ProtocolVersionSourceDefault, gotDevice.EffectiveVersionSource)
	require.Equal(t, gbmodels.ProtocolOverrideAuto, gotDevice.ProtocolOverride)

	state := &gbmodels.GbDeviceControlState{
		DeviceID: device.ID, TargetScope: gbmodels.ControlTargetScopeChannel,
		TargetCode: "34020000001320000002", ObservedAt: time.Now().UTC(),
	}
	require.NoError(t, db.Create(state).Error)
	var gotState gbmodels.GbDeviceControlState
	require.NoError(t, db.First(&gotState, state.ID).Error)
	require.Equal(t, gbmodels.ControlStateUnknown, gotState.RecordState)
	require.Equal(t, gbmodels.ControlStateUnknown, gotState.GuardState)
	require.Equal(t, gbmodels.ControlStateUnknown, gotState.Freshness)
}

func TestLegacyOperationProfileCanBeBackfilled(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbPTZOperation{}))
	op := &gbmodels.GbPTZOperation{
		OperationID: "legacy-op", IdempotencyKey: "legacy-key", DeviceID: 1,
		DeviceCode: "D", ChannelID: 2, ChannelCode: "C", CmdType: "DeviceControl",
		Status: gbmodels.PTZOperationQueued, SN: 1,
	}
	require.NoError(t, db.Create(op).Error)
	var got gbmodels.GbPTZOperation
	require.NoError(t, db.First(&got, op.ID).Error)
	require.Empty(t, got.ProfileVersion, "legacy rows remain nullable until migration backfill")
	require.Empty(t, got.TargetCode, "legacy rows remain nullable until migration backfill")
}
