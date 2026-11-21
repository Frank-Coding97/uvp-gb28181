package subscribe

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"uvplatform.com/uvp-gb28181/app/gb28181/alarmpopup"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

// 弹窗与落库是两条独立链路：关闭「存储报警消息」（出厂默认）时，
// 报警仍必须推到前端浮层 —— 否则默认配置下这个功能等于不存在。
func TestAlarmProcessorPublishesPopupWhenPersistenceDisabled(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbAlarmEvent{}))
	device := &gbmodels.GbDevice{DeviceID: "34020000001320000001", Name: "东门摄像机", OwnerDeptID: 7}
	require.NoError(t, db.Create(device).Error)

	events, cancel := alarmpopup.Subscribe()
	defer cancel()

	p := NewAlarmProcessor(db, func() time.Time { return time.Date(2026, 11, 17, 11, 0, 0, 0, time.Local) })
	p.saveEnabled = func() bool { return false }
	p.popupEnabled = func() bool { return true }

	body := []byte(`<Notify><CmdType>Alarm</CmdType><SN>7</SN><DeviceID>UNKNOWN</DeviceID><AlarmPriority>2</AlarmPriority><AlarmMethod>5</AlarmMethod><AlarmTime>2026-11-17T10:59:00</AlarmTime></Notify>`)
	require.NoError(t, p.Process(context.Background(), device, Notification{CallID: "call", CSeq: "9", Body: body}))

	select {
	case event := <-events:
		require.Equal(t, "34020000001320000001", event.DeviceCode)
		require.Equal(t, "东门摄像机", event.DeviceName)
		require.Equal(t, uint(7), event.OwnerDeptID)
		require.Equal(t, device.ID, event.DeviceDBID)
		require.Zero(t, event.AlarmID, "未落库时不该有告警主键")
		require.Equal(t, "UNKNOWN", event.SourceCode)
		require.NotNil(t, event.Priority)
		require.Equal(t, 2, *event.Priority)
		require.NotNil(t, event.AlarmTime)
	case <-time.After(2 * time.Second):
		t.Fatal("未收到实时弹窗事件")
	}

	var count int64
	require.NoError(t, db.Model(&gbmodels.GbAlarmEvent{}).Count(&count).Error)
	require.Zero(t, count, "弹窗不能反过来把存储开关打开")
}

// 开关关闭时一条都不许推（前端不再需要按开关判断，服务端就是唯一判据）。
func TestAlarmProcessorSkipsPopupWhenDisabled(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbAlarmEvent{}))
	device := &gbmodels.GbDevice{DeviceID: "D", Name: "设备"}
	require.NoError(t, db.Create(device).Error)

	events, cancel := alarmpopup.Subscribe()
	defer cancel()

	p := NewAlarmProcessor(db, time.Now)
	p.saveEnabled = func() bool { return false }
	p.popupEnabled = func() bool { return false }

	body := []byte(`<Notify><CmdType>Alarm</CmdType><SN>8</SN><DeviceID>D</DeviceID><AlarmPriority>1</AlarmPriority></Notify>`)
	require.NoError(t, p.Process(context.Background(), device, Notification{CallID: "c", CSeq: "1", Body: body}))

	select {
	case event := <-events:
		t.Fatalf("开关关闭时不该推送，却收到 %+v", event)
	case <-time.After(200 * time.Millisecond):
	}
}

// 落库侧的重发去重要延续到弹窗：同一报文被设备重发时不能刷两遍。
func TestAlarmProcessorDoesNotRepeatPopupForRetransmission(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbAlarmEvent{}))
	device := &gbmodels.GbDevice{DeviceID: "D", Name: "设备"}
	require.NoError(t, db.Create(device).Error)

	events, cancel := alarmpopup.Subscribe()
	defer cancel()

	p := NewAlarmProcessor(db, time.Now)
	p.saveEnabled = func() bool { return true }
	p.popupEnabled = func() bool { return true }

	body := []byte(`<Notify><CmdType>Alarm</CmdType><SN>9</SN><DeviceID>D</DeviceID><AlarmPriority>1</AlarmPriority></Notify>`)
	notification := Notification{CallID: "dup", CSeq: "1", Body: body}
	require.NoError(t, p.Process(context.Background(), device, notification))
	require.NoError(t, p.Process(context.Background(), device, notification))

	count := 0
	for {
		select {
		case <-events:
			count++
			continue
		case <-time.After(200 * time.Millisecond):
		}
		break
	}
	require.Equal(t, 1, count, "重发的同一条报警只该弹一次")
}
