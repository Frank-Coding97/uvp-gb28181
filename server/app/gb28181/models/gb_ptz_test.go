package models_test

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestPTZModels_AutoMigrateAndIndexes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbPTZOperation{}, &gbmodels.GbPTZState{}, &gbmodels.GbPTZPreset{}, &gbmodels.GbPTZCruiseTrack{}))
	for _, model := range []interface{}{&gbmodels.GbPTZOperation{}, &gbmodels.GbPTZState{}, &gbmodels.GbPTZPreset{}, &gbmodels.GbPTZCruiseTrack{}} {
		require.True(t, db.Migrator().HasTable(model))
	}
	require.True(t, db.Migrator().HasIndex(&gbmodels.GbPTZOperation{}, "uk_ptz_operation_idempotency"))
	require.True(t, db.Migrator().HasIndex(&gbmodels.GbPTZPreset{}, "uk_ptz_preset_channel_number"))
	require.True(t, db.Migrator().HasIndex(&gbmodels.GbPTZCruiseTrack{}, "uk_ptz_cruise_channel_track"))
}

func TestPTZOperation_IdempotencyUnique(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbPTZOperation{}))
	first := &gbmodels.GbPTZOperation{ChannelID: 1, DeviceID: 2, OperationID: "op-1", IdempotencyKey: "same", Status: gbmodels.PTZOperationQueued}
	require.NoError(t, db.Create(first).Error)
	second := &gbmodels.GbPTZOperation{ChannelID: 1, DeviceID: 2, OperationID: "op-2", IdempotencyKey: "same", Status: gbmodels.PTZOperationQueued}
	require.Error(t, db.Create(second).Error)
}

func TestPTZPreset_SoftDeleteKeepsHistory(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbPTZPreset{}))
	preset := &gbmodels.GbPTZPreset{ChannelID: 1, PresetID: 3, Name: "门口", Status: gbmodels.PTZPresetActive}
	require.NoError(t, db.Create(preset).Error)
	require.NoError(t, db.Model(preset).Update("status", gbmodels.PTZPresetDeleted).Error)
	var got gbmodels.GbPTZPreset
	require.NoError(t, db.First(&got, preset.ID).Error)
	require.Equal(t, gbmodels.PTZPresetDeleted, got.Status)
}

func TestPTZState_StoresDeviceTimeAndReceiveTime(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbPTZState{}))
	deviceTime := time.Date(2026, 7, 19, 20, 0, 0, 0, time.UTC)
	received := deviceTime.Add(time.Second)
	state := &gbmodels.GbPTZState{ChannelID: 1, DeviceTime: &deviceTime, ReceivedAt: received, Freshness: gbmodels.PTZFreshnessFresh}
	require.NoError(t, db.Create(state).Error)
	var got gbmodels.GbPTZState
	require.NoError(t, db.First(&got, state.ID).Error)
	require.Equal(t, gbmodels.PTZFreshnessFresh, got.Freshness)
	require.Equal(t, received.Unix(), got.ReceivedAt.Unix())
}
