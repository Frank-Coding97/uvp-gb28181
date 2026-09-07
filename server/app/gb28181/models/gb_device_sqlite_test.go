package models

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

type realSQLiteDeviceConfig struct{}

func (realSQLiteDeviceConfig) ConfigFileChangeListen(...func()) {}
func (realSQLiteDeviceConfig) Get(string) interface{}           { return nil }
func (realSQLiteDeviceConfig) GetString(key string) string {
	if key == "gormv2.usedbtype" {
		return "sqlite"
	}
	return ""
}
func (realSQLiteDeviceConfig) GetBool(string) bool              { return false }
func (realSQLiteDeviceConfig) GetInt(string) int                { return 0 }
func (realSQLiteDeviceConfig) GetInt32(string) int32            { return 0 }
func (realSQLiteDeviceConfig) GetInt64(string) int64            { return 0 }
func (realSQLiteDeviceConfig) GetFloat64(string) float64        { return 0 }
func (realSQLiteDeviceConfig) GetDuration(string) time.Duration { return 0 }
func (realSQLiteDeviceConfig) GetStringSlice(string) []string   { return nil }
func (realSQLiteDeviceConfig) GetUintSlice(string) []uint       { return nil }
func (realSQLiteDeviceConfig) Set(string, interface{})          {}
func (realSQLiteDeviceConfig) SaveConfig() error                { return nil }

func newRealSQLiteDeviceDB(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "设备 # heartbeat.db")
	db, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	result, err := sqlitebootstrap.Initialize(context.Background(), db)
	require.NoError(t, err)
	require.True(t, result.Created)

	oldDB, oldConfig := app.GormDbSQLite, app.ConfigYml
	app.GormDbSQLite = db
	app.ConfigYml = realSQLiteDeviceConfig{}
	t.Cleanup(func() {
		app.GormDbSQLite = oldDB
		app.ConfigYml = oldConfig
	})
	return db
}

func TestListStaleOnline_RealSQLiteUsesPortableTimeBoundaries(t *testing.T) {
	db := newRealSQLiteDeviceDB(t)
	const (
		timeoutCount = 3
		graceSeconds = 15
	)
	now := time.Now().UTC()
	threshold := time.Duration(timeoutCount*60+graceSeconds) * time.Second
	old := now.Add(-(threshold + time.Second)).In(time.FixedZone("CST", 8*60*60))
	fresh := now.Add(-(threshold - time.Second))
	devices := []GbDevice{
		{DeviceID: "34020000002000100001", Status: DeviceStatusOnline, KeepaliveTime: &old, KeepaliveInterval: 60},
		{DeviceID: "34020000002000100002", Status: DeviceStatusOnline, KeepaliveTime: &fresh, KeepaliveInterval: 60},
	}
	for i := range devices {
		require.NoError(t, db.Create(&devices[i]).Error)
	}

	list, err := ListStaleOnline(context.Background(), timeoutCount, graceSeconds)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, devices[0].DeviceID, list[0].DeviceID)

	equal := now.Add(-threshold)
	early := now.Add(-(threshold - time.Second))
	late := now.Add(-(threshold + time.Second))
	require.False(t, heartbeatExpiredAt(now, &equal, 60, timeoutCount, graceSeconds), "threshold equality must remain online")
	require.False(t, heartbeatExpiredAt(now, &early, 60, timeoutCount, graceSeconds))
	require.True(t, heartbeatExpiredAt(now, &late, 60, timeoutCount, graceSeconds))
	require.False(t, (&GbDevice{}).IsOnlineByFact(timeoutCount, graceSeconds), "a device without a heartbeat is offline by fact")
}

func TestLockGBDeviceForMaintenance_RealSQLiteSerializesTransactions(t *testing.T) {
	db := newRealSQLiteDeviceDB(t)
	device := GbDevice{DeviceID: "34020000002000100003", Status: DeviceStatusOnline}
	require.NoError(t, db.Create(&device).Error)

	firstLocked := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- db.Transaction(func(tx *gorm.DB) error {
			_, err := LockGBDeviceForMaintenance(tx, device.ID, device.DeviceID)
			if err != nil {
				return err
			}
			close(firstLocked)
			<-releaseFirst
			return nil
		})
	}()
	select {
	case <-firstLocked:
	case <-time.After(2 * time.Second):
		t.Fatal("first maintenance transaction did not acquire the device lock")
	}

	secondEntered := make(chan error, 1)
	go func() {
		secondEntered <- db.Transaction(func(tx *gorm.DB) error {
			_, err := LockGBDeviceForMaintenance(tx, device.ID, device.DeviceID)
			return err
		})
	}()
	select {
	case err := <-secondEntered:
		t.Fatalf("second maintenance transaction entered while first was held: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	close(releaseFirst)
	require.NoError(t, <-firstDone)
	require.NoError(t, <-secondEntered)
}
