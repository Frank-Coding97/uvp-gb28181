package ptz

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func newDeviceStatusTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbPTZOperation{}, &gbmodels.GbDeviceControlState{}))
	service, err := NewService(db, &fakeTrackedSender{}, func() time.Time {
		return time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	})
	require.NoError(t, err)
	return service, db
}

func createDeviceStatusOperation(t *testing.T, db *gorm.DB, operationID string, deviceID, channelID uint, targetCode string) gbmodels.GbPTZOperation {
	t.Helper()
	operation := gbmodels.GbPTZOperation{
		OperationID: operationID, IdempotencyKey: operationID,
		DeviceID: deviceID, DeviceCode: "D", ChannelID: channelID, ChannelCode: targetCode,
		CmdType: manscdp.CmdDeviceStatus, Action: "device_status",
		TargetScope: gbmodels.ControlTargetScopeChannel, TargetCode: targetCode,
		SN: int(channelID), Status: gbmodels.PTZOperationSent, CreatedAt: time.Now(),
	}
	require.NoError(t, db.Create(&operation).Error)
	return operation
}

func createScopedDeviceStatusOperation(t *testing.T, db *gorm.DB, operationID string, deviceID, channelID uint, channelCode, scope, targetCode, payload string) gbmodels.GbPTZOperation {
	t.Helper()
	operation := gbmodels.GbPTZOperation{
		OperationID: operationID, IdempotencyKey: operationID,
		DeviceID: deviceID, DeviceCode: "D", ChannelID: channelID, ChannelCode: channelCode,
		CmdType: manscdp.CmdDeviceStatus, Action: "device_status", PayloadJSON: payload,
		TargetScope: scope, TargetCode: targetCode,
		SN: int(channelID), Status: gbmodels.PTZOperationSent, ResponseRequired: true, CreatedAt: time.Now(),
	}
	require.NoError(t, db.Create(&operation).Error)
	return operation
}

func TestPersistDeviceStatusKeepsNewerOperationWhenResponsesArriveOutOfOrder(t *testing.T) {
	service, db := newDeviceStatusTestService(t)
	older := createDeviceStatusOperation(t, db, "status-older", 1, 11, "C")
	newer := createDeviceStatusOperation(t, db, "status-newer", 1, 11, "C")
	require.Greater(t, newer.ID, older.ID)

	require.NoError(t, service.persistDeviceStatus(context.Background(), newer, manscdp.DeviceStatus{
		Record: manscdp.ControlStateOn,
	}, []byte("newer")))
	require.NoError(t, service.persistDeviceStatus(context.Background(), older, manscdp.DeviceStatus{
		Record: manscdp.ControlStateOff,
	}, []byte("older")))

	var state gbmodels.GbDeviceControlState
	require.NoError(t, db.Where("device_id = ? AND target_scope = ? AND target_code = ?", 1, gbmodels.ControlTargetScopeChannel, "C").First(&state).Error)
	require.Equal(t, gbmodels.ControlStateOn, state.RecordState)
	require.Equal(t, newer.OperationID, *state.SourceOperationID)
	require.Equal(t, newer.ID, state.SourceOperationSeq)
	require.Equal(t, "newer", state.RawSummary)
}

func TestPersistDeviceStatusIsolatesSameTargetCodeAcrossDevices(t *testing.T) {
	service, db := newDeviceStatusTestService(t)
	first := createDeviceStatusOperation(t, db, "device-one-status", 1, 11, "C")
	second := createDeviceStatusOperation(t, db, "device-two-status", 2, 22, "C")

	require.NoError(t, service.persistDeviceStatus(context.Background(), first, manscdp.DeviceStatus{
		Record: manscdp.ControlStateOn,
	}, []byte("device-one")))
	require.NoError(t, service.persistDeviceStatus(context.Background(), second, manscdp.DeviceStatus{
		Record: manscdp.ControlStateOff,
	}, []byte("device-two")))

	var states []gbmodels.GbDeviceControlState
	require.NoError(t, db.Where("target_scope = ? AND target_code = ?", gbmodels.ControlTargetScopeChannel, "C").Order("device_id").Find(&states).Error)
	require.Len(t, states, 2)
	require.Equal(t, uint(1), states[0].DeviceID)
	require.Equal(t, gbmodels.ControlStateOn, states[0].RecordState)
	require.Equal(t, uint(2), states[1].DeviceID)
	require.Equal(t, gbmodels.ControlStateOff, states[1].RecordState)
}

func TestPersistDeviceStatusSeparatesChannelRecordAndAlarmFacts(t *testing.T) {
	service, db := newDeviceStatusTestService(t)
	channel := createScopedDeviceStatusOperation(t, db, "channel-status", 1, 11, "C", gbmodels.ControlTargetScopeChannel, "C", `{}`)
	parent := createScopedDeviceStatusOperation(t, db, "parent-status", 1, 12, "C", gbmodels.ControlTargetScopeDevice, "D", `{"alarmTargetCode":"A"}`)

	require.NoError(t, service.persistDeviceStatus(context.Background(), channel, manscdp.DeviceStatus{
		Record: manscdp.ControlStateOn,
	}, []byte("channel")))
	require.NoError(t, service.persistDeviceStatus(context.Background(), parent, manscdp.DeviceStatus{
		Record: manscdp.ControlStateOff,
		AlarmItems: []manscdp.DeviceStatusAlarmItem{
			{DeviceID: "A", DutyStatus: manscdp.DutyStatusOnDuty},
			{DeviceID: "B", DutyStatus: manscdp.DutyStatusAlarm},
		},
	}, []byte("parent")))

	var channelState, deviceState, alarmA, alarmB gbmodels.GbDeviceControlState
	require.NoError(t, db.Where("device_id = ? AND target_scope = ? AND target_code = ?", 1, gbmodels.ControlTargetScopeChannel, "C").First(&channelState).Error)
	require.NoError(t, db.Where("device_id = ? AND target_scope = ? AND target_code = ?", 1, gbmodels.ControlTargetScopeDevice, "D").First(&deviceState).Error)
	require.NoError(t, db.Where("device_id = ? AND target_scope = ? AND target_code = ?", 1, gbmodels.ControlTargetScopeAlarm, "A").First(&alarmA).Error)
	require.NoError(t, db.Where("device_id = ? AND target_scope = ? AND target_code = ?", 1, gbmodels.ControlTargetScopeAlarm, "B").First(&alarmB).Error)

	require.Equal(t, gbmodels.ControlStateOn, channelState.RecordState)
	require.Equal(t, gbmodels.ControlStateOff, deviceState.RecordState)
	require.Equal(t, gbmodels.ControlStateOn, alarmA.GuardState)
	require.Equal(t, gbmodels.ControlStateAlarm, alarmB.GuardState)
	require.Equal(t, gbmodels.ControlStateUnknown, alarmA.RecordState)
	require.Equal(t, gbmodels.ControlStateUnknown, alarmB.RecordState)
}

func TestPersistDeviceStatusMarksExpectedMissingAlarmUnknown(t *testing.T) {
	service, db := newDeviceStatusTestService(t)
	operation := createScopedDeviceStatusOperation(t, db, "missing-alarm", 1, 11, "C", gbmodels.ControlTargetScopeDevice, "D", `{"alarmTargetCode":"A"}`)

	require.NoError(t, service.persistDeviceStatus(context.Background(), operation, manscdp.DeviceStatus{Record: manscdp.ControlStateOff}, []byte("missing")))

	var state gbmodels.GbDeviceControlState
	require.NoError(t, db.Where("device_id = ? AND target_scope = ? AND target_code = ?", 1, gbmodels.ControlTargetScopeAlarm, "A").First(&state).Error)
	require.Equal(t, gbmodels.ControlStateUnknown, state.GuardState)
}

func TestApplyDeviceStatusResponseDoesNotPersistAfterOperationTimeout(t *testing.T) {
	service, db := newDeviceStatusTestService(t)
	operation := createScopedDeviceStatusOperation(t, db, "late-status", 1, 11, "C", gbmodels.ControlTargetScopeChannel, "C", `{}`)
	require.NoError(t, db.Model(&operation).Update("status", gbmodels.PTZOperationTimeout).Error)
	operation.Status = gbmodels.PTZOperationTimeout
	body := []byte(`<Response><CmdType>DeviceStatus</CmdType><SN>11</SN><DeviceID>C</DeviceID><Result>OK</Result><Record>ON</Record></Response>`)

	require.Error(t, service.applyDeviceStatusResponse(context.Background(), operation, "late", "1", body))
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbDeviceControlState{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestFindPTZMessageOperationExcludesTerminalDeviceStatus(t *testing.T) {
	service, db := newDeviceStatusTestService(t)
	operation := createScopedDeviceStatusOperation(t, db, "terminal-status", 1, 11, "C", gbmodels.ControlTargetScopeChannel, "C", `{}`)
	require.NoError(t, db.Model(&operation).Update("status", gbmodels.PTZOperationTimeout).Error)

	_, matched, _, reason, err := service.findPTZMessageOperation(context.Background(), "D", "", "", manscdp.MessageHead{
		CmdType: manscdp.CmdDeviceStatus, SN: "11", DeviceID: "C",
	})
	require.NoError(t, err)
	require.False(t, matched)
	require.Equal(t, "no_candidate", reason)
}
