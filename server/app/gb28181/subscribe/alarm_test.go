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
