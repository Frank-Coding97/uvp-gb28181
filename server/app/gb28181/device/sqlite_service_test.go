package device

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/gormhelper"
	"uvplatform.cn/uvp-gb28181/internal/sqlitebootstrap"
)

type realSQLiteServiceConfig struct{}

func (realSQLiteServiceConfig) ConfigFileChangeListen(...func()) {}
func (realSQLiteServiceConfig) Get(string) interface{}           { return nil }
func (realSQLiteServiceConfig) GetString(key string) string {
	if key == "gormv2.usedbtype" {
		return "sqlite"
	}
	return ""
}
func (realSQLiteServiceConfig) GetBool(string) bool { return false }
func (realSQLiteServiceConfig) GetInt(key string) int {
	if key == "gb28181.device.default_owner_dept_id" {
		return 1
	}
	return 0
}
func (realSQLiteServiceConfig) GetInt32(string) int32            { return 0 }
func (realSQLiteServiceConfig) GetInt64(string) int64            { return 0 }
func (realSQLiteServiceConfig) GetFloat64(string) float64        { return 0 }
func (realSQLiteServiceConfig) GetDuration(string) time.Duration { return 0 }
func (realSQLiteServiceConfig) GetStringSlice(string) []string   { return nil }
func (realSQLiteServiceConfig) GetUintSlice(string) []uint       { return nil }
func (realSQLiteServiceConfig) Set(string, interface{})          {}
func (realSQLiteServiceConfig) SaveConfig() error                { return nil }

func newRealSQLiteServiceDB(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "设备 # register.db")
	db, err := gormhelper.NewSQLiteClient(path)
	require.NoError(t, err)
	raw, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	result, err := sqlitebootstrap.Initialize(context.Background(), db)
	require.NoError(t, err)
	require.True(t, result.Created)

	oldDB, oldConfig, oldLog := app.GormDbSQLite, app.ConfigYml, app.ZapLog
	app.GormDbSQLite = db
	app.ConfigYml = realSQLiteServiceConfig{}
	app.ZapLog = zap.NewNop()
	t.Cleanup(func() {
		app.GormDbSQLite, app.ConfigYml, app.ZapLog = oldDB, oldConfig, oldLog
	})
	return db
}

func TestHandleRegister_RealSQLiteSingleConnectionTransactionCompletes(t *testing.T) {
	db := newRealSQLiteServiceDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	first, err := HandleRegister(ctx, RegisterInfo{
		DeviceID:  "34020000002000100004",
		Transport: "UDP",
		IP:        "127.0.0.1",
		Port:      5060,
		Expires:   3600,
	}, 60)
	require.NoError(t, err)
	require.True(t, first)

	var device gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", "34020000002000100004").First(&device).Error)
	require.Equal(t, gbmodels.DeviceStatusOnline, device.Status)
	raw, err := db.DB()
	require.NoError(t, err)
	require.Equal(t, 0, raw.Stats().InUse, "transaction must release the sole SQLite connection")
}

func TestHandleRegister_RealSQLitePropagatesCanceledTransactionAndReleasesConnection(t *testing.T) {
	db := newRealSQLiteServiceDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := HandleRegister(ctx, RegisterInfo{DeviceID: "34020000002000100008", Expires: 3600}, 60)
	require.ErrorIs(t, err, context.Canceled)

	raw, err := db.DB()
	require.NoError(t, err)
	require.Equal(t, 0, raw.Stats().InUse, "canceled transaction must release the sole SQLite connection")
	validCtx, validCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer validCancel()
	_, err = HandleRegister(validCtx, RegisterInfo{DeviceID: "34020000002000100008", Expires: 3600}, 60)
	require.NoError(t, err, "a canceled transaction must not poison the SQLite connection")
}

func TestMarkOfflineIfStale_RechecksFreshRegisterOnRealSQLite(t *testing.T) {
	db := newRealSQLiteServiceDB(t)
	const deviceID = "34020000002000100005"
	old := time.Now().Add(-10 * time.Minute)
	require.NoError(t, db.Create(&gbmodels.GbDevice{
		DeviceID: deviceID, Status: gbmodels.DeviceStatusOnline,
		KeepaliveTime: &old, KeepaliveInterval: 60, OwnerDeptID: 1,
	}).Error)

	stale, err := gbmodels.ListStaleOnline(context.Background(), 3, 15)
	require.NoError(t, err)
	require.Len(t, stale, 1)

	_, err = HandleRegister(context.Background(), RegisterInfo{DeviceID: deviceID, Expires: 3600}, 60)
	require.NoError(t, err)
	changed, err := gbmodels.MarkOfflineIfStale(context.Background(), deviceID, 3, 15)
	require.NoError(t, err)
	require.False(t, changed, "the fresh registration must make the stale candidate a no-op")

	var device gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", deviceID).First(&device).Error)
	require.Equal(t, gbmodels.DeviceStatusOnline, device.Status)
}

func TestOfflineScanner_RealSQLiteTransitionsStaleDeviceAndReleasesConnection(t *testing.T) {
	db := newRealSQLiteServiceDB(t)
	const deviceID = "34020000002000100007"
	old := time.Now().Add(-10 * time.Minute)
	require.NoError(t, db.Create(&gbmodels.GbDevice{
		DeviceID: deviceID, Status: gbmodels.DeviceStatusOnline,
		KeepaliveTime: &old, KeepaliveInterval: 60, OwnerDeptID: 1,
	}).Error)

	observer := &capturedStatusObserver{}
	SetStatusObserver(observer)
	t.Cleanup(func() { SetStatusObserver(nil) })
	NewOfflineScanner(1, 3, 15).ScanOnceForTest()
	require.Equal(t, []string{"HEARTBEAT_TIMEOUT"}, observer.reasons)

	var device gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", deviceID).First(&device).Error)
	require.Equal(t, gbmodels.DeviceStatusOffline, device.Status)
	raw, err := db.DB()
	require.NoError(t, err)
	require.Equal(t, 0, raw.Stats().InUse, "scanner must close all result rows before returning")
}

func TestOfflineScanner_StaleCandidateRefreshedBeforeRecheckDoesNotNotifyOffline(t *testing.T) {
	db := newRealSQLiteServiceDB(t)
	const deviceID = "34020000002000100009"
	old := time.Now().Add(-10 * time.Minute)
	require.NoError(t, db.Create(&gbmodels.GbDevice{
		DeviceID: deviceID, Status: gbmodels.DeviceStatusOnline,
		KeepaliveTime: &old, KeepaliveInterval: 60, OwnerDeptID: 1,
	}).Error)

	stale, err := gbmodels.ListStaleOnline(context.Background(), 3, 15)
	require.NoError(t, err)
	require.Len(t, stale, 1)
	_, err = HandleRegister(context.Background(), RegisterInfo{DeviceID: deviceID, Expires: 3600}, 60)
	require.NoError(t, err)

	observer := &capturedStatusObserver{}
	SetStatusObserver(observer)
	t.Cleanup(func() { SetStatusObserver(nil) })
	NewOfflineScanner(1, 3, 15).processStale(context.Background(), stale[0])
	require.Empty(t, observer.reasons, "a refreshed stale candidate must not publish offline")

	var device gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", deviceID).First(&device).Error)
	require.Equal(t, gbmodels.DeviceStatusOnline, device.Status)
}

func TestRegisterKeepaliveAndOfflineRaceKeepsNewestStateOnRealSQLite(t *testing.T) {
	db := newRealSQLiteServiceDB(t)
	const deviceID = "34020000002000100006"
	old := time.Now().Add(-10 * time.Minute)
	require.NoError(t, db.Create(&gbmodels.GbDevice{
		DeviceID: deviceID, Status: gbmodels.DeviceStatusOnline,
		KeepaliveTime: &old, KeepaliveInterval: 60, OwnerDeptID: 1,
	}).Error)

	for attempt := 0; attempt < 8; attempt++ {
		old = time.Now().Add(-10 * time.Minute)
		require.NoError(t, db.Model(&gbmodels.GbDevice{}).Where("device_id = ?", deviceID).Updates(map[string]interface{}{
			"status":         gbmodels.DeviceStatusOnline,
			"keepalive_time": old,
			"offline_at":     nil,
		}).Error)

		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		start := make(chan struct{})
		results := make(chan error, 3)
		go func() {
			<-start
			_, err := HandleRegister(ctx, RegisterInfo{DeviceID: deviceID, Expires: 3600}, 60)
			results <- err
		}()
		go func() {
			<-start
			_, err := Keepalive(ctx, deviceID)
			results <- err
		}()
		go func() {
			<-start
			_, err := gbmodels.MarkOfflineIfStale(ctx, deviceID, 3, 15)
			results <- err
		}()
		close(start)
		for i := 0; i < 3; i++ {
			require.NoError(t, <-results)
		}
		cancel()

		var device gbmodels.GbDevice
		require.NoError(t, db.Where("device_id = ?", deviceID).First(&device).Error)
		require.Equal(t, gbmodels.DeviceStatusOnline, device.Status, "attempt %d lost the newest online state", attempt)
		require.NotNil(t, device.KeepaliveTime, "attempt %d lost the heartbeat fact", attempt)
	}
}
