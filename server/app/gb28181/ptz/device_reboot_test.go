package ptz

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

type rebootSender struct {
	mu      sync.Mutex
	calls   int
	results []uac.TrackedMessageResult
	errors  []error
	bodies  [][]byte
}

func (s *rebootSender) SendMessageTracked(_ context.Context, _ string, _ string, _ string, body []byte) (uac.TrackedMessageResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.bodies = append(s.bodies, append([]byte(nil), body...))
	index := s.calls - 1
	var result uac.TrackedMessageResult
	if index < len(s.results) {
		result = s.results[index]
	}
	var err error
	if index < len(s.errors) {
		err = s.errors[index]
	}
	return result, err
}

func newDeviceRebootService(t *testing.T, sender TrackedSender, now func() time.Time) (*Service, *gorm.DB, *gbmodels.GbDevice) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbPTZOperation{}, &gbmodels.GbDeviceFirmwareUpgrade{}))
	device := &gbmodels.GbDevice{
		DeviceID: "34020000002000000001", Name: "测试设备", IP: "192.0.2.10", Port: 5060,
		Transport: "TCP", Status: gbmodels.DeviceStatusOnline,
	}
	require.NoError(t, db.Create(device).Error)
	service, err := NewService(db, sender, now)
	require.NoError(t, err)
	return service, db, device
}

func deviceRebootTarget(device *gbmodels.GbDevice, profile protocol.Profile) DeviceRebootTarget {
	return DeviceRebootTarget{
		DeviceID: device.ID, DeviceCode: device.DeviceID, IP: device.IP, Port: device.Port,
		Transport: device.Transport, DeviceOnline: true, Profile: profile,
	}
}

func TestExecuteDeviceRebootUsesDeviceScopeAndBothProtocolProfiles(t *testing.T) {
	for _, version := range []string{protocol.Version2016, protocol.Version2022} {
		t.Run(version, func(t *testing.T) {
			sender := &rebootSender{results: []uac.TrackedMessageResult{{CallID: "reboot-call", CSeq: "4", StatusCode: 200}}}
			service, db, device := newDeviceRebootService(t, sender, time.Now)
			op, err := service.ExecuteDeviceReboot(context.Background(), deviceRebootTarget(device, protocol.ProfileFor(protocol.Version(version))), "same-key", 17, 23)
			require.NoError(t, err)
			require.Equal(t, gbmodels.PTZOperationSent, op.Status)
			require.Equal(t, manscdpDeviceControl, op.CmdType)
			require.Equal(t, "teleboot", op.Action)
			require.Zero(t, op.ChannelID, "设备级重启不得伪造通道")
			require.Empty(t, op.ChannelCode)
			require.Equal(t, gbmodels.ControlTargetScopeDevice, op.TargetScope)
			require.Equal(t, device.DeviceID, op.TargetCode)
			require.Equal(t, uint(17), op.ActorID)
			require.Equal(t, uint(23), op.ActorDeptID)
			require.Equal(t, version, op.ProfileVersion)
			require.JSONEq(t, `{"confirmed":true}`, op.PayloadJSON)
			require.True(t, strings.HasPrefix(op.IdempotencyKey, "reboot:v1:"))
			require.Contains(t, string(sender.bodies[0]), "<TeleBoot>Boot</TeleBoot>")
			var stored gbmodels.GbPTZOperation
			require.NoError(t, db.First(&stored, op.ID).Error)
			require.Equal(t, 200, stored.SIPStatus)
		})
	}
}

func TestExecuteDeviceRebootDeduplicatesAcrossKeysAndDevices(t *testing.T) {
	sender := &rebootSender{results: []uac.TrackedMessageResult{
		{CallID: "one", CSeq: "1", StatusCode: 200},
		{CallID: "two", CSeq: "2", StatusCode: 200},
	}}
	service, db, first := newDeviceRebootService(t, sender, time.Now)
	second := &gbmodels.GbDevice{DeviceID: "34020000002000000002", IP: "192.0.2.11", Port: 5060, Transport: "UDP", Status: gbmodels.DeviceStatusOnline}
	require.NoError(t, db.Create(second).Error)
	one, err := service.ExecuteDeviceReboot(context.Background(), deviceRebootTarget(first, protocol.ProfileFor(protocol.Version2022)), "shared-key", 1, 2)
	require.NoError(t, err)
	replay, err := service.ExecuteDeviceReboot(context.Background(), deviceRebootTarget(first, protocol.ProfileFor(protocol.Version2022)), "shared-key", 3, 4)
	require.NoError(t, err)
	require.Equal(t, one.OperationID, replay.OperationID)
	require.Equal(t, uint(1), replay.ActorID, "幂等重放保留首次操作者")
	two, err := service.ExecuteDeviceReboot(context.Background(), deviceRebootTarget(second, protocol.ProfileFor(protocol.Version2022)), "shared-key", 5, 6)
	require.NoError(t, err)
	require.NotEqual(t, one.OperationID, two.OperationID, "不同设备同幂等键必须独立")
	require.Equal(t, 2, sender.calls)
}

func TestExecuteDeviceRebootDeduplicatesLegacyChannelRecordWithinSixtySeconds(t *testing.T) {
	sender := &rebootSender{results: []uac.TrackedMessageResult{{CallID: "new", CSeq: "1", StatusCode: 200}}}
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	service, db, device := newDeviceRebootService(t, sender, func() time.Time { return now })
	legacy := &gbmodels.GbPTZOperation{
		OperationID: "legacy-operation", IdempotencyKey: "legacy-key", DeviceID: device.ID, DeviceCode: device.DeviceID,
		ChannelID: 42, ChannelCode: "34020000001310000042", CmdType: manscdpDeviceControl, Action: "teleboot",
		Status: gbmodels.PTZOperationAccepted, SN: 8, CreatedAt: now.Add(-30 * time.Second), TargetScope: gbmodels.ControlTargetScopeChannel,
	}
	require.NoError(t, db.Create(legacy).Error)
	op, err := service.ExecuteDeviceReboot(context.Background(), deviceRebootTarget(device, protocol.ProfileFor(protocol.Version2022)), "new-key", 17, 23)
	require.NoError(t, err)
	require.Equal(t, legacy.OperationID, op.OperationID)
	require.Equal(t, 0, sender.calls)
}

func TestExecuteDeviceRebootBlocksActiveFirmwareUpgrade(t *testing.T) {
	sender := &rebootSender{results: []uac.TrackedMessageResult{{CallID: "should-not-send", CSeq: "1", StatusCode: 200}}}
	service, db, device := newDeviceRebootService(t, sender, time.Now)
	now := time.Now()
	upgrade := &gbmodels.GbDeviceFirmwareUpgrade{
		OperationID: "active-upgrade", IdempotencyKey: "active-upgrade-key", DeviceID: device.ID, DeviceCode: device.DeviceID,
		Firmware: "v2.3.4", FileURL: "https://fixture.example/v2.3.4.bin", Manufacturer: "UVP",
		SessionID: "0123456789abcdef0123456789abcdef", SN: 88, ProfileVersion: protocol.Version2022,
		ProfileCharset: "GB18030", Status: gbmodels.FirmwareUpgradeAccepted, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(upgrade).Error)

	_, err := service.ExecuteDeviceReboot(context.Background(), deviceRebootTarget(device, protocol.ProfileFor(protocol.Version2022)), "reboot-during-upgrade", 1, 1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "设备固件升级进行中")
	require.Zero(t, sender.calls)
}

func TestExecuteDeviceRebootRejectsOfflineAndMissingSource(t *testing.T) {
	sender := &rebootSender{}
	service, db, device := newDeviceRebootService(t, sender, time.Now)
	require.NoError(t, db.Model(device).Update("status", gbmodels.DeviceStatusOffline).Error)
	_, err := service.ExecuteDeviceReboot(context.Background(), deviceRebootTarget(device, protocol.ProfileFor(protocol.Version2022)), "offline", 1, 2)
	require.Error(t, err)
	require.Contains(t, err.Error(), "离线")
	require.NoError(t, db.Model(device).Updates(map[string]interface{}{"status": gbmodels.DeviceStatusOnline, "ip": "", "port": 0}).Error)
	_, err = service.ExecuteDeviceReboot(context.Background(), deviceRebootTarget(device, protocol.ProfileFor(protocol.Version2022)), "missing-source", 1, 2)
	require.Error(t, err)
	require.Empty(t, sender.calls)
}

func TestExecuteDeviceRebootPersistsSentRejectedAndUnknownWithoutRetry(t *testing.T) {
	tests := []struct {
		name   string
		result uac.TrackedMessageResult
		err    error
		status gbmodels.PTZOperationStatus
	}{
		{name: "sip 200", result: uac.TrackedMessageResult{CallID: "ok", CSeq: "1", StatusCode: 200, Attempted: true}, status: gbmodels.PTZOperationSent},
		{name: "final non 2xx", result: uac.TrackedMessageResult{CallID: "reject", CSeq: "2", StatusCode: 486, Attempted: true}, err: errors.New("MESSAGE 应答非2xx"), status: gbmodels.PTZOperationRejected},
		{name: "not attempted", result: uac.TrackedMessageResult{}, err: errors.New("connect failed"), status: gbmodels.PTZOperationRejected},
		{name: "provisional 1xx", result: uac.TrackedMessageResult{CallID: "provisional", CSeq: "3", StatusCode: 100, Attempted: true}, err: errors.New("MESSAGE 未收到最终应答"), status: gbmodels.PTZOperationUnknown},
		{name: "network unknown", result: uac.TrackedMessageResult{CallID: "unknown", CSeq: "4", Attempted: true}, err: errors.New("timeout"), status: gbmodels.PTZOperationUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sender := &rebootSender{results: []uac.TrackedMessageResult{tt.result}, errors: []error{tt.err}}
			service, db, device := newDeviceRebootService(t, sender, time.Now)
			op, err := service.ExecuteDeviceReboot(context.Background(), deviceRebootTarget(device, protocol.ProfileFor(protocol.Version2022)), tt.name, 1, 2)
			require.NoError(t, err, "已入库的设备重启结果应携 operationId 返回")
			require.Equal(t, tt.status, op.Status)
			require.Equal(t, 1, op.MaxAttempts)
			require.False(t, op.ResponseRequired)
			var count int64
			require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Where("device_id = ? AND action = ?", device.ID, "teleboot").Count(&count).Error)
			require.EqualValues(t, 1, count)
			require.Equal(t, 1, sender.calls)
		})
	}
}

func TestExecuteDeviceRebootRejectsRetiredService(t *testing.T) {
	service, _, device := newDeviceRebootService(t, &rebootSender{}, time.Now)
	service.Retire()

	_, err := service.ExecuteDeviceReboot(context.Background(), deviceRebootTarget(device, protocol.ProfileFor(protocol.Version2022)), "retired", 1, 2)
	var operationErr *OperationError
	require.ErrorAs(t, err, &operationErr)
	require.Equal(t, ErrorCodeHomePositionUnavailable, operationErr.Code)
}

const manscdpDeviceControl = "DeviceControl"
