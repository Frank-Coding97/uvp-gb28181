package subscribe

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

func newSubscribeSQLiteBaselineDB(t *testing.T) (*gorm.DB, *gbmodels.GbDevice, *gbmodels.GbChannel) {
	t.Helper()
	db, err := gormhelper.NewSQLiteClient(filepath.Join(t.TempDir(), "subscribe.db"))
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	_, err = sqlitebootstrap.Initialize(context.Background(), db)
	require.NoError(t, err)

	device := &gbmodels.GbDevice{
		DeviceID: "34020000001320001234", Name: "T12 SQLite device", IP: "192.0.2.10", Port: 5060,
		Transport: "UDP", Status: gbmodels.DeviceStatusOnline,
	}
	require.NoError(t, db.Create(device).Error)
	channel := &gbmodels.GbChannel{
		DeviceID: device.DeviceID, ChannelID: "34020000001310001234", Name: "T12 channel",
		Status: gbmodels.ChannelStatusOnline,
	}
	require.NoError(t, db.Create(channel).Error)
	return db, device, channel
}

func TestT12SubscribeSQLiteBaselinePersistsAndValidates(t *testing.T) {
	db, device, _ := newSubscribeSQLiteBaselineDB(t)
	sender := &fakeSender{response: uac.SubscriptionResponse{
		StatusCode: 200, Expires: 3600, CallID: "t12-subscribe-call", LocalTag: "local", RemoteTag: "remote", CSeq: 1,
	}}
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	service := NewService(db, sender, func() time.Time { return now })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sub, err := service.Enable(ctx, device, gbmodels.SubscriptionKindCatalog)
	require.NoError(t, err)
	require.Equal(t, gbmodels.SubscriptionStatusActive, sub.Status)
	require.True(t, sub.Enabled)
	require.Equal(t, "t12-subscribe-call", sub.CallID)
	require.Len(t, sender.calls, 1)
	require.Contains(t, string(sender.calls[0].Body), "<CmdType>Catalog</CmdType>")

	badExpires := 0
	_, err = service.Configure(ctx, device, gbmodels.SubscriptionKindCatalog, nil, &badExpires, nil)
	require.ErrorContains(t, err, "订阅有效期")

	var count int64
	require.NoError(t, db.Model(&gbmodels.GbDeviceSubscription{}).
		Where("device_id = ? AND kind = ?", device.ID, gbmodels.SubscriptionKindCatalog).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestT12AlarmSQLiteBaselineDeduplicatesAndRejectsMalformed(t *testing.T) {
	db, device, _ := newSubscribeSQLiteBaselineDB(t)
	now := time.Date(2026, 9, 7, 12, 1, 0, 0, time.UTC)
	processor := NewAlarmProcessor(db, func() time.Time { return now })
	processor.saveEnabled = func() bool { return true }
	body := []byte(`<Notify><CmdType>Alarm</CmdType><SN>12</SN><DeviceID>34020000001310001234</DeviceID><AlarmTime>2026-09-07T12:00:00Z</AlarmTime><AlarmPriority>2</AlarmPriority><AlarmDescription>sqlite alarm</AlarmDescription></Notify>`)
	notification := Notification{Kind: gbmodels.SubscriptionKindAlarm, CallID: "t12-alarm-call", CSeq: "1", Body: body}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, processor.Process(ctx, device, notification))
	require.NoError(t, processor.Process(ctx, device, notification))

	var events []gbmodels.GbAlarmEvent
	require.NoError(t, db.Where("device_id = ?", device.ID).Find(&events).Error)
	require.Len(t, events, 1)
	require.NotNil(t, events[0].ChannelID)
	require.Equal(t, "sqlite alarm", events[0].Description)
	require.Error(t, processor.Process(ctx, device, Notification{Body: []byte(`<Notify>`)}))
}

func TestT12MobilePositionSQLiteBaselineUpsertsAndRejectsZero(t *testing.T) {
	db, device, channel := newSubscribeSQLiteBaselineDB(t)
	now := time.Date(2026, 9, 7, 12, 2, 0, 0, time.UTC)
	processor := NewPositionProcessor(db, func() time.Time { return now }, func() bool { return true })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	body := []byte(`<Notify><CmdType>MobilePosition</CmdType><DeviceID>34020000001310001234</DeviceID><Time>2026-09-07T12:01:00Z</Time><Longitude>116.4</Longitude><Latitude>39.9</Latitude></Notify>`)
	require.NoError(t, processor.Process(ctx, device, Notification{Body: body}))
	updated := []byte(`<Notify><CmdType>MobilePosition</CmdType><DeviceID>34020000001310001234</DeviceID><Time>2026-09-07T12:01:30Z</Time><Longitude>116.5</Longitude><Latitude>39.8</Latitude></Notify>`)
	require.NoError(t, processor.Process(ctx, device, Notification{Body: updated}))

	var latest gbmodels.GbMobilePositionLatest
	require.NoError(t, db.Where("device_id = ? AND source_code = ?", device.ID, channel.ChannelID).First(&latest).Error)
	require.Equal(t, channel.ID, *latest.ChannelID)
	require.Equal(t, 116.5, latest.Longitude)
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbMobilePositionLatest{}).Where("device_id = ?", device.ID).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Model(&gbmodels.GbMobilePositionHistory{}).Where("device_id = ?", device.ID).Count(&count).Error)
	require.EqualValues(t, 2, count)

	zero := []byte(`<Notify><CmdType>MobilePosition</CmdType><DeviceID>34020000001310001234</DeviceID><Time>2026-09-07T12:02:00Z</Time><Longitude>0</Longitude><Latitude>39.8</Latitude></Notify>`)
	require.Error(t, processor.Process(ctx, device, Notification{Body: zero}))
}
