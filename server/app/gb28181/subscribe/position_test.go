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
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbChannel{}, &gbmodels.GbMobilePositionLatest{}))
	device := &gbmodels.GbDevice{DeviceID: "D"}
	channel := &gbmodels.GbChannel{DeviceID: "D", ChannelID: "C"}
	require.NoError(t, db.Create(device).Error)
	require.NoError(t, db.Create(channel).Error)
	p := NewPositionProcessor(db, func() time.Time { return time.Date(2026, 7, 19, 16, 30, 0, 0, time.Local) })
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

	zero := []byte(`<Notify><CmdType>MobilePosition</CmdType><DeviceID>C</DeviceID><Time>2026-07-19T16:01:00</Time><Longitude>0</Longitude><Latitude>39.8</Latitude></Notify>`)
	require.Error(t, p.Process(context.Background(), device, Notification{Body: zero}))
}
