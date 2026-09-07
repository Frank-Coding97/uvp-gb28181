package models_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestPTZModels_AutoMigrateAndIndexes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbPTZOperation{},
		&gbmodels.GbPTZOperationAttempt{},
		&gbmodels.GbPTZHomePosition{},
		&gbmodels.GbPTZState{},
		&gbmodels.GbPTZPreset{},
		&gbmodels.GbPTZCruiseTrack{},
	))
	for _, model := range []interface{}{
		&gbmodels.GbPTZOperation{},
		&gbmodels.GbPTZOperationAttempt{},
		&gbmodels.GbPTZHomePosition{},
		&gbmodels.GbPTZState{},
		&gbmodels.GbPTZPreset{},
		&gbmodels.GbPTZCruiseTrack{},
	} {
		require.True(t, db.Migrator().HasTable(model))
	}

	for _, column := range []string{
		"response_required", "max_attempts", "queue_deadline_at", "dispatch_started_at",
		"transport_deadline_at", "deadline_at", "next_attempt_at", "response_call_id",
		"response_cseq", "response_at", "response_has_data", "trigger_operation_id",
		"reconcile_operation_id",
	} {
		require.Truef(t, db.Migrator().HasColumn(&gbmodels.GbPTZOperation{}, column), "missing operation column %s", column)
	}
	for _, index := range []string{
		"uk_ptz_operation_id",
		"uk_ptz_operation_idempotency",
		"idx_ptz_operation_channel_time",
		"idx_ptz_operation_channel_cmd_id",
		"idx_ptz_operation_device_sn",
		"idx_ptz_operation_status_time",
		"idx_ptz_operation_status_next_attempt",
		"idx_ptz_operation_status_queue_deadline",
		"idx_ptz_operation_status_transport_deadline",
		"idx_ptz_operation_status_deadline",
		"idx_ptz_operation_call_id",
		"idx_ptz_operation_target",
	} {
		require.Truef(t, db.Migrator().HasIndex(&gbmodels.GbPTZOperation{}, index), "missing operation index %s", index)
	}
	for index, columns := range map[string][]string{
		"uk_ptz_operation_idempotency":                {"channel_id", "idempotency_key"},
		"idx_ptz_operation_channel_time":              {"channel_id", "created_at"},
		"idx_ptz_operation_channel_cmd_id":            {"channel_id", "cmd_type", "id"},
		"idx_ptz_operation_device_sn":                 {"device_id", "sn"},
		"idx_ptz_operation_status_time":               {"status", "created_at"},
		"idx_ptz_operation_status_next_attempt":       {"status", "next_attempt_at"},
		"idx_ptz_operation_status_queue_deadline":     {"status", "queue_deadline_at"},
		"idx_ptz_operation_status_transport_deadline": {"status", "transport_deadline_at"},
		"idx_ptz_operation_status_deadline":           {"status", "deadline_at"},
		"idx_ptz_operation_target":                    {"device_code", "target_scope", "target_code", "status"},
	} {
		requirePTZIndexColumns(t, db, &gbmodels.GbPTZOperation{}, index, columns)
	}
	require.True(t, db.Migrator().HasIndex(&gbmodels.GbPTZOperationAttempt{}, "uk_ptz_operation_attempt"))
	require.True(t, db.Migrator().HasIndex(&gbmodels.GbPTZOperationAttempt{}, "idx_ptz_attempt_status_lease"))
	require.True(t, db.Migrator().HasIndex(&gbmodels.GbPTZHomePosition{}, "uk_ptz_home_position_channel"))
	require.True(t, db.Migrator().HasIndex(&gbmodels.GbPTZHomePosition{}, "idx_ptz_home_position_device"))
	require.True(t, db.Migrator().HasIndex(&gbmodels.GbPTZState{}, "uk_ptz_state_channel"))
	require.True(t, db.Migrator().HasIndex(&gbmodels.GbPTZState{}, "idx_ptz_state_device"))
	require.True(t, db.Migrator().HasIndex(&gbmodels.GbPTZState{}, "idx_ptz_state_received"))
	require.True(t, db.Migrator().HasColumn(&gbmodels.GbPTZState{}, "device_code"))
	require.True(t, db.Migrator().HasIndex(&gbmodels.GbPTZPreset{}, "uk_ptz_preset_channel_number"))
	require.True(t, db.Migrator().HasIndex(&gbmodels.GbPTZPreset{}, "idx_ptz_preset_device"))
	require.True(t, db.Migrator().HasIndex(&gbmodels.GbPTZCruiseTrack{}, "uk_ptz_cruise_channel_track"))
	require.True(t, db.Migrator().HasIndex(&gbmodels.GbPTZCruiseTrack{}, "idx_ptz_cruise_device"))

	for _, legacyColumn := range []string{"home_enabled", "home_pan", "home_tilt", "home_zoom"} {
		require.Falsef(t, db.Migrator().HasColumn(&gbmodels.GbPTZState{}, legacyColumn), "new baseline must not expose legacy column %s", legacyColumn)
	}
}

func requirePTZIndexColumns(t *testing.T, db *gorm.DB, model interface{}, name string, want []string) {
	t.Helper()
	indexes, err := db.Migrator().GetIndexes(model)
	require.NoError(t, err)
	for _, index := range indexes {
		if index.Name() == name {
			require.Equal(t, want, index.Columns(), name)
			return
		}
	}
	require.Failf(t, "missing index", "%s was not returned by Migrator.GetIndexes", name)
}

func TestPTZHomePosition_StoresZeroValuesIndependently(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbPTZState{}, &gbmodels.GbPTZHomePosition{}))

	precise := &gbmodels.GbPTZState{
		DeviceID: 2, ChannelID: 1, ChannelCode: "34020000001320000001",
		ReceivedAt: time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC),
		Freshness:  gbmodels.PTZFreshnessFresh,
	}
	require.NoError(t, db.Create(precise).Error)
	zero := 0
	home := &gbmodels.GbPTZHomePosition{
		DeviceID:           2,
		ChannelID:          1,
		ChannelCode:        "34020000001320000001",
		Enabled:            false,
		ResetTime:          &zero,
		PresetID:           &zero,
		EnabledEncoding:    gbmodels.PTZHomePositionEnabledNumeric,
		ConfirmedAt:        time.Date(2026, 7, 24, 9, 1, 0, 0, time.UTC),
		Source:             gbmodels.PTZHomePositionSourceDeviceQuery,
		Verification:       gbmodels.PTZHomePositionVerificationVerified,
		SourceOperationSeq: 7,
	}
	require.NoError(t, db.Create(home).Error)

	var got gbmodels.GbPTZHomePosition
	require.NoError(t, db.First(&got, home.ID).Error)
	require.False(t, got.Enabled)
	require.NotNil(t, got.ResetTime)
	require.Zero(t, *got.ResetTime)
	require.NotNil(t, got.PresetID)
	require.Zero(t, *got.PresetID)

	var gotPrecise gbmodels.GbPTZState
	require.NoError(t, db.First(&gotPrecise, precise.ID).Error)
	require.Equal(t, precise.ReceivedAt.Unix(), gotPrecise.ReceivedAt.Unix())
}

func TestPTZHomePositionAndAttempt_UniqueConstraints(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbPTZOperation{}, &gbmodels.GbPTZOperationAttempt{}, &gbmodels.GbPTZHomePosition{}))

	confirmedAt := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	firstHome := &gbmodels.GbPTZHomePosition{
		ChannelID: 1, Enabled: false, ConfirmedAt: confirmedAt,
		EnabledEncoding: gbmodels.PTZHomePositionEnabledNumeric,
		Source:          gbmodels.PTZHomePositionSourceDeviceQuery, Verification: gbmodels.PTZHomePositionVerificationVerified,
	}
	require.NoError(t, db.Create(firstHome).Error)
	duplicateHome := *firstHome
	duplicateHome.ID = 0
	require.Error(t, db.Create(&duplicateHome).Error)

	op := &gbmodels.GbPTZOperation{OperationID: "op-unique", IdempotencyKey: "key-unique", ChannelID: 1, Status: gbmodels.PTZOperationQueued}
	require.NoError(t, db.Create(op).Error)
	startedAt := confirmedAt
	leaseUntil := confirmedAt.Add(time.Second)
	firstAttempt := &gbmodels.GbPTZOperationAttempt{
		OperationID: op.ID, AttemptNo: 1, SN: 11, Status: gbmodels.PTZOperationAttemptDispatching,
		StartedAt: startedAt, LeaseUntil: leaseUntil,
	}
	require.NoError(t, db.Create(firstAttempt).Error)
	duplicateAttempt := *firstAttempt
	duplicateAttempt.ID = 0
	require.Error(t, db.Create(&duplicateAttempt).Error)
}

func TestPTZOperation_ResponseDefaultsAndExplicitZeroAttempt(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbPTZOperation{}))

	legacy := &gbmodels.GbPTZOperation{
		OperationID: "op-legacy", IdempotencyKey: "key-legacy", ChannelID: 1, Status: gbmodels.PTZOperationQueued,
	}
	require.NoError(t, db.Create(legacy).Error)
	var gotLegacy gbmodels.GbPTZOperation
	require.NoError(t, db.First(&gotLegacy, legacy.ID).Error)
	require.False(t, gotLegacy.ResponseRequired)
	require.Equal(t, 1, gotLegacy.MaxAttempts)
	require.Equal(t, 1, gotLegacy.Attempt)
	require.Nil(t, gotLegacy.QueueDeadlineAt)
	require.Nil(t, gotLegacy.DispatchStartedAt)
	require.Nil(t, gotLegacy.TransportDeadlineAt)
	require.Nil(t, gotLegacy.DeadlineAt)
	require.Nil(t, gotLegacy.NextAttemptAt)
	require.Nil(t, gotLegacy.ResponseCallID)
	require.Nil(t, gotLegacy.ResponseCSeq)
	require.Nil(t, gotLegacy.ResponseAt)
	require.Nil(t, gotLegacy.ResponseHasData)
	require.Nil(t, gotLegacy.TriggerOperationID)
	require.Nil(t, gotLegacy.ReconcileOperationID)

	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Create(map[string]interface{}{
		"operation_id":      "op-home",
		"idempotency_key":   "key-home",
		"device_id":         1,
		"device_code":       "34020000002000000001",
		"channel_id":        2,
		"channel_code":      "34020000001320000001",
		"cmd_type":          "HomePositionQuery",
		"sn":                12,
		"status":            gbmodels.PTZOperationQueued,
		"attempt":           0,
		"response_required": true,
		"max_attempts":      3,
		"created_at":        time.Date(2026, 7, 24, 9, 2, 0, 0, time.UTC),
	}).Error)
	var gotHome gbmodels.GbPTZOperation
	require.NoError(t, db.Where("operation_id = ?", "op-home").First(&gotHome).Error)
	require.True(t, gotHome.ResponseRequired)
	require.Equal(t, 3, gotHome.MaxAttempts)
	require.Zero(t, gotHome.Attempt)
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
	state := &gbmodels.GbPTZState{
		DeviceID: 2, DeviceCode: "34020000002000000001", ChannelID: 1,
		ChannelCode: "34020000001320000001", DeviceTime: &deviceTime,
		ReceivedAt: received, Freshness: gbmodels.PTZFreshnessFresh,
	}
	require.NoError(t, db.Create(state).Error)
	var got gbmodels.GbPTZState
	require.NoError(t, db.First(&got, state.ID).Error)
	require.Equal(t, gbmodels.PTZFreshnessFresh, got.Freshness)
	require.Equal(t, state.DeviceCode, got.DeviceCode)
	require.Equal(t, received.Unix(), got.ReceivedAt.Unix())
}
