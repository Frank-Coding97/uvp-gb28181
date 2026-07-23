package subscribe

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

func TestAlarmProcessor_DeduplicatesNotifyAndMessage(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbAlarmEvent{}))
	device := &gbmodels.GbDevice{DeviceID: "D"}
	require.NoError(t, db.Create(device).Error)
	p := NewAlarmProcessor(db, func() time.Time { return time.Date(2026, 7, 19, 16, 40, 0, 0, time.Local) })
	body := []byte(`<Notify><CmdType>Alarm</CmdType><SN>7</SN><DeviceID>UNKNOWN</DeviceID><AlarmTime>2026-07-19T16:39:00</AlarmTime><AlarmPriority>2</AlarmPriority><AlarmDescription>loss</AlarmDescription></Notify>`)
	notification := Notification{CallID: "call", CSeq: "9", Body: body}
	require.NoError(t, p.Process(context.Background(), device, notification))
	require.NoError(t, p.Process(context.Background(), device, notification))
	var events []gbmodels.GbAlarmEvent
	require.NoError(t, db.Find(&events).Error)
	require.Len(t, events, 1)
	require.Nil(t, events[0].ChannelID)
	require.Equal(t, "UNKNOWN", events[0].SourceCode)
}

// TestAlarmProcessor_RawSummaryIsUTF8ForGB2312Body 复现 gorm Error 1366:
// 国标设备下发的 alarm body 常用 GB2312 编码,直接 string(body) 塞进 utf8mb4 列会被 MySQL 拒收。
// 走 truncateRaw → manscdp.DecodeToUTF8 转码后 RawSummary 应是合法 UTF-8。
func TestAlarmProcessor_RawSummaryIsUTF8ForGB2312Body(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbAlarmEvent{}))
	device := &gbmodels.GbDevice{DeviceID: "D"}
	require.NoError(t, db.Create(device).Error)
	p := NewAlarmProcessor(db, func() time.Time { return time.Date(2026, 7, 23, 15, 35, 55, 0, time.Local) })

	// GB2312 字节: 视频动检
	gb := []byte{0xCA, 0xD3, 0xC6, 0xB5, 0xB6, 0xAF, 0xBC, 0xEC}
	body := append([]byte(`<?xml version="1.0" encoding="GB2312"?><Notify><CmdType>Alarm</CmdType><SN>241</SN><DeviceID>UNKNOWN</DeviceID><AlarmTime>2026-07-23T15:35:55</AlarmTime><AlarmPriority>1</AlarmPriority><AlarmDescription>`), gb...)
	body = append(body, []byte(`</AlarmDescription></Notify>`)...)

	require.NoError(t, p.Process(context.Background(), device, Notification{CallID: "c", CSeq: "342", Body: body}))

	var events []gbmodels.GbAlarmEvent
	require.NoError(t, db.Find(&events).Error)
	require.Len(t, events, 1)
	require.True(t, utf8.ValidString(events[0].RawSummary), "raw_summary 应为合法 UTF-8,实际=%q", events[0].RawSummary)
	require.True(t, strings.Contains(events[0].RawSummary, "视频动检"), "raw_summary 应含转码后的中文,实际=%q", events[0].RawSummary)
}
