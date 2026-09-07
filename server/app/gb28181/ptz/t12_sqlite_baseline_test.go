package ptz

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

func newPTZSQLiteBaselineDB(t *testing.T) (*gorm.DB, *gbmodels.GbDevice, *gbmodels.GbChannel) {
	t.Helper()
	db, err := gormhelper.NewSQLiteClient(filepath.Join(t.TempDir(), "ptz.db"))
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	_, err = sqlitebootstrap.Initialize(context.Background(), db)
	require.NoError(t, err)

	device := &gbmodels.GbDevice{
		DeviceID: "34020000001320005678", Name: "T12 PTZ device", IP: "192.0.2.20", Port: 5060,
		Transport: "TCP", Status: gbmodels.DeviceStatusOnline,
	}
	require.NoError(t, db.Create(device).Error)
	channel := &gbmodels.GbChannel{
		DeviceID: device.DeviceID, ChannelID: "34020000001310005678", Name: "T12 PTZ channel",
		Status: gbmodels.ChannelStatusOnline,
	}
	require.NoError(t, db.Create(channel).Error)
	return db, device, channel
}

func t12PTZTarget(device *gbmodels.GbDevice, channel *gbmodels.GbChannel) Target {
	return Target{
		DeviceID: device.ID, DeviceCode: device.DeviceID, ChannelID: channel.ID, ChannelCode: channel.ChannelID,
		IP: device.IP, Port: device.Port, Transport: device.Transport, DeviceOnline: true, ChannelOnline: true,
		Profile: protocol.ProfileFor(protocol.Version2022),
	}
}

func TestT12PTZSQLiteBaselinePersistsIdempotentControlAndRejectsOffline(t *testing.T) {
	db, device, channel := newPTZSQLiteBaselineDB(t)
	sender := &fakeTrackedSender{}
	now := time.Date(2026, 9, 7, 12, 3, 0, 0, time.UTC)
	service, err := NewService(db, sender, func() time.Time { return now })
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	command := Command{
		CmdType: "DeviceControl", Action: "left", IdempotencyKey: "t12-ptz-control",
		Payload: map[string]interface{}{"action": "left"},
		Build: func(sn int) ([]byte, error) {
			return []byte(fmt.Sprintf("<Control><SN>%d</SN><DeviceID>%s><PTZCmd>left</PTZCmd></Control>", sn, channel.ChannelID)), nil
		},
	}
	one, err := service.Execute(ctx, t12PTZTarget(device, channel), command)
	require.NoError(t, err)
	require.Equal(t, gbmodels.PTZOperationSent, one.Status)
	require.Equal(t, gbmodels.ControlTargetScopeChannel, one.TargetScope)
	require.Equal(t, channel.ChannelID, one.TargetCode)
	require.Equal(t, 1, sender.calls)

	two, err := service.Execute(ctx, t12PTZTarget(device, channel), command)
	require.NoError(t, err)
	require.Equal(t, one.OperationID, two.OperationID)
	require.Equal(t, 1, sender.calls, "相同幂等键重放不能再次发送")

	offline := t12PTZTarget(device, channel)
	offline.DeviceOnline = false
	_, err = service.Execute(ctx, offline, Command{
		CmdType: "DeviceControl", Action: "right", IdempotencyKey: "t12-ptz-offline",
		Build: func(int) ([]byte, error) { return []byte("<Control/>"), nil },
	})
	require.Error(t, err)
	require.Equal(t, 1, sender.calls, "非法状态不能发送协议消息")

	var count int64
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).
		Where("device_id = ? AND channel_id = ?", device.ID, channel.ID).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestT12DeviceRebootSQLiteBaselineLocksParentAndRejectsWrongTarget(t *testing.T) {
	db, device, _ := newPTZSQLiteBaselineDB(t)
	// Keep the response explicit so the baseline covers outbound SIP metadata.
	reboot := &rebootSender{results: []uac.TrackedMessageResult{{
		CallID: "t12-reboot-call", CSeq: "1", StatusCode: 200, Attempted: true,
	}}}
	now := time.Date(2026, 9, 7, 12, 4, 0, 0, time.UTC)
	service, err := NewService(db, reboot, func() time.Time { return now })
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	target := DeviceRebootTarget{
		DeviceID: device.ID, DeviceCode: device.DeviceID, IP: device.IP, Port: device.Port,
		Transport: device.Transport, DeviceOnline: true, Profile: protocol.ProfileFor(protocol.Version2022),
	}
	operation, err := service.ExecuteDeviceReboot(ctx, target, "t12-reboot", 7, 8)
	require.NoError(t, err)
	require.Equal(t, gbmodels.PTZOperationSent, operation.Status)
	require.Equal(t, "teleboot", operation.Action)
	require.Zero(t, operation.ChannelID)
	require.Equal(t, gbmodels.ControlTargetScopeDevice, operation.TargetScope)
	require.Equal(t, device.DeviceID, operation.TargetCode)
	require.Len(t, reboot.bodies, 1)
	require.Contains(t, string(reboot.bodies[0]), "<TeleBoot>Boot</TeleBoot>")

	wrong := target
	wrong.DeviceCode = "34020000001320009999"
	_, err = service.ExecuteDeviceReboot(ctx, wrong, "t12-reboot-wrong-target", 7, 8)
	require.Error(t, err)
	require.Len(t, reboot.bodies, 1, "目标编码不匹配不能发送重启")

	var stored gbmodels.GbPTZOperation
	require.NoError(t, db.Where("operation_id = ?", operation.OperationID).First(&stored).Error)
	require.Equal(t, gbmodels.PTZOperationSent, stored.Status)
}
