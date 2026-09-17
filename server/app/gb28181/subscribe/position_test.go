package subscribe

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestPositionProcessor_UpsertsChannelLatestAndRejectsZero(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbMobilePositionLatest{}, &gbmodels.GbMobilePositionHistory{}))
	device := &gbmodels.GbDevice{DeviceID: "D"}
	channel := &gbmodels.GbChannel{DeviceID: "D", ChannelID: "C"}
	require.NoError(t, db.Create(device).Error)
	require.NoError(t, db.Create(channel).Error)
	p := NewPositionProcessor(db, func() time.Time { return time.Date(2026, 7, 19, 16, 30, 0, 0, time.Local) }, func() bool { return true })
	first := []byte(`<Notify><CmdType>MobilePosition</CmdType><DeviceID>C</DeviceID><Time>2026-07-19T16:00:00</Time><Longitude>116.4</Longitude><Latitude>39.9</Latitude></Notify>`)
	require.NoError(t, p.Process(context.Background(), device, Notification{Body: first}))
	second := []byte(`<Notify><CmdType>MobilePosition</CmdType><DeviceID>C</DeviceID><Time>2026-07-19T16:01:00</Time><Longitude>116.5</Longitude><Latitude>39.8</Latitude></Notify>`)
	require.NoError(t, p.Process(context.Background(), device, Notification{Body: second}))
	var latest gbmodels.GbMobilePositionLatest
	require.NoError(t, db.Where("device_id = ? AND source_code = ?", device.ID, "C").First(&latest).Error)
	require.Equal(t, channel.ID, *latest.ChannelID)
	require.Equal(t, 116.5, latest.Longitude)
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbMobilePositionLatest{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Model(&gbmodels.GbMobilePositionHistory{}).Count(&count).Error)
	require.EqualValues(t, 2, count)

	disabledHistory := NewPositionProcessor(db, func() time.Time { return time.Date(2026, 7, 19, 16, 30, 0, 0, time.Local) }, func() bool { return false })
	third := []byte(`<Notify><CmdType>MobilePosition</CmdType><DeviceID>C</DeviceID><Time>2026-07-19T16:02:00</Time><Longitude>116.6</Longitude><Latitude>39.7</Latitude></Notify>`)
	require.NoError(t, disabledHistory.Process(context.Background(), device, Notification{Body: third}))
	require.NoError(t, db.Where("device_id = ? AND source_code = ?", device.ID, "C").First(&latest).Error)
	require.Equal(t, 116.6, latest.Longitude)
	require.NoError(t, db.Model(&gbmodels.GbMobilePositionHistory{}).Count(&count).Error)
	require.EqualValues(t, 2, count)

	zero := []byte(`<Notify><CmdType>MobilePosition</CmdType><DeviceID>C</DeviceID><Time>2026-07-19T16:01:00</Time><Longitude>0</Longitude><Latitude>39.8</Latitude></Notify>`)
	require.Error(t, p.Process(context.Background(), device, Notification{Body: zero}))
}

func TestPrunePositionHistory_RemovesOnlyExpiredRecords(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbMobilePositionHistory{}))
	now := time.Date(2026, 7, 19, 17, 0, 0, 0, time.Local)
	require.NoError(t, db.Create(&gbmodels.GbMobilePositionHistory{DeviceID: 1, SourceCode: "old", EventTime: now.AddDate(0, 0, -8), ReceivedAt: now.AddDate(0, 0, -8), Latitude: 39.9, Longitude: 116.4}).Error)
	require.NoError(t, db.Create(&gbmodels.GbMobilePositionHistory{DeviceID: 1, SourceCode: "new", EventTime: now.AddDate(0, 0, -7), ReceivedAt: now.AddDate(0, 0, -7), Latitude: 39.9, Longitude: 116.4}).Error)

	deleted, err := PrunePositionHistory(context.Background(), db, now, 7)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbMobilePositionHistory{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

// newPositionTestDB 起一个只装位置相关表的内存库 + 一台设备。
func newPositionTestDB(t *testing.T) (*gorm.DB, *gbmodels.GbDevice) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&gbmodels.GbDevice{}, &gbmodels.GbChannel{},
		&gbmodels.GbMobilePositionLatest{}, &gbmodels.GbMobilePositionHistory{},
	))
	device := &gbmodels.GbDevice{DeviceID: "D"}
	require.NoError(t, db.Create(device).Error)
	return db, device
}

// 2022 列表形态一包可带**多台设备**的位置：逐条独立落库，
// event_time 取各自的 Item/CaptureTime（不是根 Time —— 根 Time 是上报通知时间）。
func TestPositionProcessor_2022ListFormLandsEveryItem(t *testing.T) {
	db, device := newPositionTestDB(t)
	p := NewPositionProcessor(db, func() time.Time {
		return time.Date(2026, 9, 17, 22, 30, 0, 0, time.Local)
	}, func() bool { return false })

	body := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<Notify>
<CmdType>MobilePosition</CmdType>
<SN>7</SN>
<DeviceID>D</DeviceID>
<Time>2026-09-17T22:00:00</Time>
<SumNum>2</SumNum>
<DeviceList Num="2">
<Item><DeviceID>C1</DeviceID><CaptureTime>2026-09-17T21:59:00</CaptureTime><Longitude>116.4</Longitude><Latitude>39.9</Latitude><Speed>36.0</Speed></Item>
<Item><DeviceID>C2</DeviceID><CaptureTime>2026-09-17T21:59:30</CaptureTime><Longitude>116.5</Longitude><Latitude>39.8</Latitude></Item>
</DeviceList>
</Notify>`)
	require.NoError(t, p.Process(context.Background(), device, Notification{Body: body}))

	var latest []gbmodels.GbMobilePositionLatest
	require.NoError(t, db.Order("source_code").Find(&latest).Error)
	require.Len(t, latest, 2)

	require.Equal(t, "C1", latest[0].SourceCode)
	require.Equal(t, 116.4, latest[0].Longitude)
	require.Equal(t, 39.9, latest[0].Latitude)
	require.Equal(t, "2026-09-17T21:59:00", latest[0].EventTime.Format("2006-01-02T15:04:05"))

	require.Equal(t, "C2", latest[1].SourceCode)
	require.Equal(t, 116.5, latest[1].Longitude)
	require.Equal(t, "2026-09-17T21:59:30", latest[1].EventTime.Format("2006-01-02T15:04:05"))

	// 根 Time（22:00:00，上报通知时间）不得被当成采集时间写进去。
	require.NotEqual(t, "2026-09-17T22:00:00", latest[0].EventTime.Format("2006-01-02T15:04:05"))
}

// 一包里某条脏数据（尚未定位 → 坐标为 0）只跳过该条，**不连坐**同包其它设备的合法位置；
// 只要有条目落地，整体就不算失败。
func TestPositionProcessor_SkipsBadItemAndLandsTheRest(t *testing.T) {
	db, device := newPositionTestDB(t)
	p := NewPositionProcessor(db, time.Now, func() bool { return false })

	body := []byte(`<Notify>
<CmdType>MobilePosition</CmdType>
<SN>8</SN>
<DeviceID>D</DeviceID>
<Time>2026-09-17T22:00:00</Time>
<SumNum>2</SumNum>
<DeviceList Num="2">
<Item><DeviceID>NOFIX</DeviceID><CaptureTime>2026-09-17T21:59:00</CaptureTime><Longitude>0</Longitude><Latitude>0</Latitude></Item>
<Item><DeviceID>OK</DeviceID><CaptureTime>2026-09-17T21:59:01</CaptureTime><Longitude>116.4</Longitude><Latitude>39.9</Latitude></Item>
</DeviceList>
</Notify>`)
	require.NoError(t, p.Process(context.Background(), device, Notification{Body: body}))

	var latest []gbmodels.GbMobilePositionLatest
	require.NoError(t, db.Find(&latest).Error)
	require.Len(t, latest, 1)
	require.Equal(t, "OK", latest[0].SourceCode)
}

// 整包全废时仍要返回错误 —— 保持改动前「零坐标必须报错」的契约
// （单设备场景恰好只有 1 条，与改动前行为一致）。
func TestPositionProcessor_AllItemsBadStillReturnsError(t *testing.T) {
	db, device := newPositionTestDB(t)
	p := NewPositionProcessor(db, time.Now, func() bool { return false })

	body := []byte(`<Notify><CmdType>MobilePosition</CmdType><SN>9</SN><DeviceID>D</DeviceID><Time>2026-09-17T22:00:00</Time><SumNum>1</SumNum><DeviceList Num="1"><Item><DeviceID>NOFIX</DeviceID><CaptureTime>2026-09-17T21:59:00</CaptureTime><Longitude>0</Longitude><Latitude>0</Latitude></Item></DeviceList></Notify>`)
	err := p.Process(context.Background(), device, Notification{Body: body})
	require.Error(t, err)
	require.Contains(t, err.Error(), "位置坐标不能为 0")
}

// 2022 空列表（SumNum=0，本次没有位置）是合法 no-op，必须返回 nil。
// 返回错误会让 notify.go 连 last_notify_at 都不更新，把一次「无位置」记成订阅异常。
func TestPositionProcessor_Empty2022ListIsNoOp(t *testing.T) {
	db, device := newPositionTestDB(t)
	p := NewPositionProcessor(db, time.Now, func() bool { return false })

	body := []byte(`<Notify><CmdType>MobilePosition</CmdType><SN>10</SN><DeviceID>D</DeviceID><Time>2026-09-17T22:00:00</Time><SumNum>0</SumNum><DeviceList Num="0"></DeviceList></Notify>`)
	require.NoError(t, p.Process(context.Background(), device, Notification{Body: body}))

	var count int64
	require.NoError(t, db.Model(&gbmodels.GbMobilePositionLatest{}).Count(&count).Error)
	require.EqualValues(t, 0, count)
}
