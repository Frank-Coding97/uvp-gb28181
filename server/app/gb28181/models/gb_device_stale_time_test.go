package models

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/global/app"
)

func newReliabilityDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&GbDevice{}, &GbDeviceStatusEvent{}))
	oldDB, oldConfig := app.GormDbMysql, app.ConfigYml
	app.GormDbMysql, app.ConfigYml = db, listPagedTestConfig{}
	t.Cleanup(func() { app.GormDbMysql, app.ConfigYml = oldDB, oldConfig })
	return db
}

func TestListStaleOnlineTimezones(t *testing.T) {
	db := newReliabilityDB(t)
	now := time.Now().UTC()
	for _, tc := range []struct {
		name   string
		offset int
		stale  bool
	}{
		{"Beijing nanoseconds expired", 8 * 3600, true},
		{"UTC expired", 0, true},
		{"West expired", -7 * 3600, true},
		{"Beijing nanoseconds fresh", 8 * 3600, false},
		{"UTC fresh", 0, false},
		{"West fresh", -7 * 3600, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, db.Exec("DELETE FROM gb_device").Error)
			heartbeat := now.Add(-30 * time.Second)
			if tc.stale {
				heartbeat = now.Add(-5 * time.Minute)
			}
			d := GbDevice{DeviceID: "34020000002000000052", Status: DeviceStatusOnline, KeepaliveInterval: 60}
			require.NoError(t, db.Create(&d).Error)
			stored := heartbeat.In(time.FixedZone("offset", tc.offset)).Format("2006-01-02 15:04:05.999999999Z07:00")
			require.NoError(t, db.Model(&GbDevice{}).Where("id = ?", d.ID).Update("keepalive_time", stored).Error)
			rows, err := ListStaleOnline(context.Background(), 3, 15)
			require.NoError(t, err)
			if tc.stale {
				require.Len(t, rows, 1, stored)
			} else {
				require.Empty(t, rows, stored)
			}
		})
	}
}

func TestKeepaliveSourceDoesNotRollBackNewerFacts(t *testing.T) {
	db := newReliabilityDB(t)
	now := time.Now()
	registered := now.Add(-time.Minute)
	expires := now.Add(time.Hour)
	heartbeat := now.Add(-time.Second)
	d := GbDevice{DeviceID: "34020000002000000052", IP: "112.25.55.9", Port: 51174, Transport: "UDP", Status: DeviceStatusOnline, RegisterTime: &registered, RegisterExpireAt: &expires, KeepaliveTime: &heartbeat}
	require.NoError(t, db.Create(&d).Error)
	source := KeepaliveSource{SenderDeviceID: d.DeviceID, Address: "112.25.55.9:12230", Transport: "UDP", ReceivedAt: now}
	restored, err := TouchKeepaliveFrom(context.Background(), d.DeviceID, true, source)
	require.NoError(t, err)
	require.False(t, restored)
	source.ReceivedAt = now.Add(-500 * time.Millisecond)
	source.Address = "112.25.55.9:51174"
	restored, err = TouchKeepaliveFrom(context.Background(), d.DeviceID, true, source)
	require.NoError(t, err)
	require.False(t, restored)
	got, err := FindByDeviceID(context.Background(), d.DeviceID)
	require.NoError(t, err)
	require.Equal(t, 12230, got.Port)
	require.True(t, got.KeepaliveTime.Equal(now))
	// A successful new REGISTER wins over a heartbeat already waiting in the handler.
	registered = now.Add(time.Millisecond)
	require.NoError(t, db.Model(&GbDevice{}).Where("id = ?", d.ID).Updates(map[string]interface{}{"register_time": registered, "port": 63404, "keepalive_time": registered}).Error)
	source.ReceivedAt = now
	source.Address = "112.25.55.9:12230"
	_, err = TouchKeepaliveFrom(context.Background(), d.DeviceID, true, source)
	require.ErrorIs(t, err, ErrKeepaliveSourceRejected)
	got, err = FindByDeviceID(context.Background(), d.DeviceID)
	require.NoError(t, err)
	require.Equal(t, 63404, got.Port)
}

func TestKeepaliveSourceMatchesPersistedTimestampFormats(t *testing.T) {
	db := newReliabilityDB(t)
	now := time.Now()
	for _, layout := range []string{"2006-01-02 15:04:05.999999999Z07:00", "2006-01-02 15:04:05.000000000-07:00"} {
		t.Run(layout, func(t *testing.T) {
			require.NoError(t, db.Exec("DELETE FROM gb_device").Error)
			zone := time.FixedZone("CST", 8*3600)
			registered := now.Add(-time.Minute).In(zone).Format(layout)
			expires := now.Add(time.Hour).In(zone).Format(layout)
			heartbeat := now.Add(-time.Second).In(zone).Format(layout)
			d := GbDevice{DeviceID: "34020000002000000052", IP: "112.25.55.9", Port: 51174, Transport: "UDP", Status: DeviceStatusOnline}
			require.NoError(t, db.Create(&d).Error)
			require.NoError(t, db.Model(&GbDevice{}).Where("id = ?", d.ID).Updates(map[string]interface{}{"register_time": registered, "register_expire_at": expires, "keepalive_time": heartbeat}).Error)
			_, err := TouchKeepaliveFrom(context.Background(), d.DeviceID, true, KeepaliveSource{SenderDeviceID: d.DeviceID, Address: "112.25.55.9:12230", Transport: "UDP", ReceivedAt: now})
			require.NoError(t, err)
			got, err := FindByDeviceID(context.Background(), d.DeviceID)
			require.NoError(t, err)
			require.Equal(t, 12230, got.Port)
		})
	}
}

func TestMarkOfflineRechecksHeartbeatTimeout(t *testing.T) {
	db := newReliabilityDB(t)
	require.NoError(t, db.AutoMigrate(&GbChannel{}))
	old := time.Now().Add(-5 * time.Minute)
	d := GbDevice{DeviceID: "34020000002000000052", Status: DeviceStatusOnline, KeepaliveInterval: 60, KeepaliveTime: &old}
	require.NoError(t, db.Create(&d).Error)
	stale, err := ListStaleOnline(context.Background(), 3, 15)
	require.NoError(t, err)
	require.Len(t, stale, 1)
	// The scan result is stale by the time its worker handles it.
	fresh := time.Now()
	require.NoError(t, db.Model(&d).Update("keepalive_time", fresh).Error)
	changed, err := MarkOfflineIfStale(context.Background(), d.DeviceID, 3, 15)
	require.NoError(t, err)
	require.False(t, changed)
	got, err := FindByDeviceID(context.Background(), d.DeviceID)
	require.NoError(t, err)
	require.Equal(t, DeviceStatusOnline, got.Status)
	var count int64
	require.NoError(t, db.Model(&GbDeviceStatusEvent{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestStaleOnlineThresholdAndLegacyUTC(t *testing.T) {
	db := newReliabilityDB(t)
	for _, tc := range []struct {
		name, layout string
		age          time.Duration
		interval     int
		status       int8
		stale        bool
	}{
		{"just expired", "2006-01-02 15:04:05.999999999Z07:00", 198 * time.Second, 60, DeviceStatusOnline, true},
		{"just fresh", "2006-01-02 15:04:05.999999999Z07:00", 192 * time.Second, 60, DeviceStatusOnline, false},
		{"legacy UTC expired", "2006-01-02 15:04:05", 5 * time.Minute, 60, DeviceStatusOnline, true},
		{"legacy UTC fresh", "2006-01-02 15:04:05", 30 * time.Second, 60, DeviceStatusOnline, false},
		{"zero interval uses default", "2006-01-02 15:04:05", 30 * time.Second, 0, DeviceStatusOnline, false},
		{"negative interval uses default", "2006-01-02 15:04:05", 30 * time.Second, -1, DeviceStatusOnline, false},
		{"offline omitted", "2006-01-02 15:04:05", 5 * time.Minute, 60, DeviceStatusOffline, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, db.Exec("DELETE FROM gb_device").Error)
			d := GbDevice{DeviceID: "34020000002000000052", Status: tc.status, KeepaliveInterval: tc.interval}
			require.NoError(t, db.Create(&d).Error)
			require.NoError(t, db.Model(&d).Updates(map[string]interface{}{"keepalive_interval": tc.interval, "keepalive_time": time.Now().UTC().Add(-tc.age).Format(tc.layout)}).Error)
			rows, err := ListStaleOnline(context.Background(), 3, 15)
			require.NoError(t, err)
			if tc.stale {
				require.Len(t, rows, 1)
			} else {
				require.Empty(t, rows)
			}
		})
	}
}

func TestConcurrentKeepalivesKeepNewestEndpointAndSingleRecovery(t *testing.T) {
	db := newReliabilityDB(t)
	now := time.Now()
	registered, expires := now.Add(-time.Minute), now.Add(time.Hour)
	d := GbDevice{DeviceID: "34020000002000000052", IP: "112.25.55.9", Port: 51174, Transport: "UDP", RegisterTime: &registered, RegisterExpireAt: &expires}
	require.NoError(t, db.Create(&d).Error)
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func(i int) {
			_, err := TouchKeepaliveFrom(context.Background(), d.DeviceID, true, KeepaliveSource{SenderDeviceID: d.DeviceID, Address: fmt.Sprintf("112.25.55.9:%d", 12000+i), Transport: "UDP", ReceivedAt: now.Add(time.Duration(i) * time.Nanosecond)})
			errs <- err
		}(i)
	}
	for i := 0; i < 20; i++ {
		require.NoError(t, <-errs)
	}
	got, err := FindByDeviceID(context.Background(), d.DeviceID)
	require.NoError(t, err)
	require.Equal(t, 12019, got.Port)
	var count int64
	require.NoError(t, db.Model(&GbDeviceStatusEvent{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}
