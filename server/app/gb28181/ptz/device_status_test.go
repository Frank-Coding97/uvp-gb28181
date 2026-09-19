package ptz

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

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

func loadDeviceStatusFact(t *testing.T, db *gorm.DB, deviceID uint, scope, targetCode string) gbmodels.GbDeviceControlState {
	t.Helper()
	var state gbmodels.GbDeviceControlState
	require.NoError(t, db.Where("device_id = ? AND target_scope = ? AND target_code = ?", deviceID, scope, targetCode).First(&state).Error)
	return state
}

// 设备在应答里报的四项事实必须真的落库 —— 这正是它们此前被解析器丢掉的地方。
func TestPersistDeviceStatusStoresDeviceReportedFacts(t *testing.T) {
	service, db := newDeviceStatusTestService(t)
	operation := createDeviceStatusOperation(t, db, "facts-op", 1, 11, "C")

	require.NoError(t, service.persistDeviceStatus(context.Background(), operation, manscdp.DeviceStatus{
		Record:        manscdp.ControlStateOn,
		Encode:        manscdp.ControlStateOn,
		Online:        manscdp.DeviceOnlineStateOnline,
		SelfTest:      manscdp.DeviceSelfTestOK,
		DeviceTime:    "2026-09-19T20:03:58",
		AlarmNumKnown: true,
		AlarmNum:      0,
	}, []byte("facts")))

	state := loadDeviceStatusFact(t, db, 1, gbmodels.ControlTargetScopeChannel, "C")
	require.NotNil(t, state.OnlineState)
	require.Equal(t, gbmodels.DeviceOnlineStateOnline, *state.OnlineState)
	require.NotNil(t, state.SelfTestState)
	require.Equal(t, gbmodels.DeviceSelfTestOK, *state.SelfTestState)
	require.NotNil(t, state.EncodeState)
	require.Equal(t, gbmodels.ControlStateOn, *state.EncodeState)
	require.NotNil(t, state.DeviceTime)
	require.Equal(t, 20, state.DeviceTime.Hour())
	require.NotNil(t, state.AlarmInputCount, "设备明确给了数量就必须落库")
	require.Zero(t, *state.AlarmInputCount, "设备声明的 0 个报警输入要落成 0,不是 NULL")
}

func TestPersistDeviceStatusStoresOfflineAndErrorFacts(t *testing.T) {
	service, db := newDeviceStatusTestService(t)
	operation := createDeviceStatusOperation(t, db, "facts-offline", 1, 11, "C")

	require.NoError(t, service.persistDeviceStatus(context.Background(), operation, manscdp.DeviceStatus{
		Record:   manscdp.ControlStateOff,
		Encode:   manscdp.ControlStateOff,
		Online:   manscdp.DeviceOnlineStateOffline,
		SelfTest: manscdp.DeviceSelfTestError,
	}, []byte("offline")))

	state := loadDeviceStatusFact(t, db, 1, gbmodels.ControlTargetScopeChannel, "C")
	require.Equal(t, gbmodels.DeviceOnlineStateOffline, *state.OnlineState)
	require.Equal(t, gbmodels.DeviceSelfTestError, *state.SelfTestState)
	require.Equal(t, gbmodels.ControlStateOff, *state.EncodeState)
}

// 「设备没提」不能沿用上一条应答的值：这一行的 observed_at 已经换成了新应答的时刻，
// 内容却是旧的，读的人分不出来。
func TestPersistDeviceStatusClearsFactsTheDeviceDidNotReport(t *testing.T) {
	service, db := newDeviceStatusTestService(t)
	ctx := context.Background()

	first := createDeviceStatusOperation(t, db, "facts-first", 1, 11, "C")
	require.NoError(t, service.persistDeviceStatus(ctx, first, manscdp.DeviceStatus{
		Record:        manscdp.ControlStateOn,
		Online:        manscdp.DeviceOnlineStateOnline,
		SelfTest:      manscdp.DeviceSelfTestOK,
		Encode:        manscdp.ControlStateOn,
		DeviceTime:    "2026-09-19T20:03:58",
		AlarmNumKnown: true,
		AlarmNum:      0,
	}, []byte("first")))

	second := createDeviceStatusOperation(t, db, "facts-second", 1, 11, "C")
	require.NoError(t, service.persistDeviceStatus(ctx, second, manscdp.DeviceStatus{
		Record: manscdp.ControlStateOff,
	}, []byte("second")))

	state := loadDeviceStatusFact(t, db, 1, gbmodels.ControlTargetScopeChannel, "C")
	require.Equal(t, gbmodels.ControlStateOff, state.RecordState)
	require.Nil(t, state.OnlineState, "新应答没报在线状态,列要回到 NULL")
	require.Nil(t, state.SelfTestState)
	require.Nil(t, state.EncodeState)
	require.Nil(t, state.DeviceTime)
	require.Nil(t, state.AlarmInputCount, "设备没提报警输入数时不得沿用上一条的 0")
}

// 报警输入的 0 与"设备整段没提"必须是两个不同的事实：前者是已知的能力缺失，
// 后者是未知。这个区分就是 guard_state 能不能摆脱永久 unknown 的前提。
func TestPersistDeviceStatusSeparatesZeroAlarmInputsFromUnreported(t *testing.T) {
	service, db := newDeviceStatusTestService(t)
	declared := createDeviceStatusOperation(t, db, "alarm-declared-zero", 1, 11, "C")
	require.NoError(t, service.persistDeviceStatus(context.Background(), declared, manscdp.DeviceStatus{
		Record:        manscdp.ControlStateOn,
		AlarmNumKnown: true,
		AlarmNum:      0,
	}, []byte("declared")))

	state := loadDeviceStatusFact(t, db, 1, gbmodels.ControlTargetScopeChannel, "C")
	require.NotNil(t, state.AlarmInputCount)
	require.Zero(t, *state.AlarmInputCount)

	silent := createDeviceStatusOperation(t, db, "alarm-silent", 2, 22, "D")
	require.NoError(t, service.persistDeviceStatus(context.Background(), silent, manscdp.DeviceStatus{
		Record: manscdp.ControlStateOn,
	}, []byte("silent")))

	other := loadDeviceStatusFact(t, db, 2, gbmodels.ControlTargetScopeChannel, "D")
	require.Nil(t, other.AlarmInputCount, "设备没提数量时是未知,不能塌成 0")
}
