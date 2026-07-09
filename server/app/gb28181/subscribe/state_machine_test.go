package subscribe_test

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/subscribe"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}))
	return db
}

func seedDevice(t *testing.T, db *gorm.DB, deviceID string, cap gbmodels.SubscribeCapability) {
	t.Helper()
	d := &gbmodels.GbDevice{DeviceID: deviceID, Name: "test", SubscribeCapability: cap}
	require.NoError(t, db.Create(d).Error)
}

// TestOnRegister_Unknown_ToFallback unknown → fallback(Phase 1 模拟 SUBSCRIBE 失败)
func TestOnRegister_Unknown_ToFallback(t *testing.T) {
	db := newTestDB(t)
	seedDevice(t, db, "34020000002000000001", gbmodels.SubscribeUnknown)
	sm := subscribe.NewStateMachine(db, zap.NewNop())

	sm.OnRegister(context.Background(), "34020000002000000001")

	var dev gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", "34020000002000000001").First(&dev).Error)
	assert.Equal(t, gbmodels.SubscribeFallback, dev.SubscribeCapability)
	assert.NotNil(t, dev.SubscribeLastTest)
}

// TestOnNotify_Upgrade fallback → subscribed(收到 NOTIFY 确认设备支持)
func TestOnNotify_Upgrade(t *testing.T) {
	db := newTestDB(t)
	seedDevice(t, db, "34020000002000000001", gbmodels.SubscribeFallback)
	sm := subscribe.NewStateMachine(db, zap.NewNop())

	sm.OnNotify(context.Background(), "34020000002000000001")

	var dev gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", "34020000002000000001").First(&dev).Error)
	assert.Equal(t, gbmodels.SubscribeSubscribed, dev.SubscribeCapability)
	assert.NotNil(t, dev.SubscribeExpiresAt)
}

// TestOnNotify_Subscribed_Refresh 已 subscribed 收到 NOTIFY → 刷新 expires
func TestOnNotify_Subscribed_Refresh(t *testing.T) {
	db := newTestDB(t)
	d := &gbmodels.GbDevice{DeviceID: "34020000002000000001", Name: "test", SubscribeCapability: gbmodels.SubscribeSubscribed}
	require.NoError(t, db.Create(d).Error)
	sm := subscribe.NewStateMachine(db, zap.NewNop())

	sm.OnNotify(context.Background(), "34020000002000000001")

	var dev gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", "34020000002000000001").First(&dev).Error)
	assert.Equal(t, gbmodels.SubscribeSubscribed, dev.SubscribeCapability)
	// expires 应在 ~30min 后
	require.NotNil(t, dev.SubscribeExpiresAt)
	assert.WithinDuration(t, time.Now().Add(30*time.Minute), *dev.SubscribeExpiresAt, 2*time.Second)
}

// TestDegradeToFallback subscribed → fallback
func TestDegradeToFallback(t *testing.T) {
	db := newTestDB(t)
	seedDevice(t, db, "34020000002000000001", gbmodels.SubscribeSubscribed)
	sm := subscribe.NewStateMachine(db, zap.NewNop())

	sm.DegradeToFallback(context.Background(), "34020000002000000001", "30min no NOTIFY")

	var dev gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", "34020000002000000001").First(&dev).Error)
	assert.Equal(t, gbmodels.SubscribeFallback, dev.SubscribeCapability)
}

// TestOnRegister_Fallback_NoRetryWithin24h 24h 内不重复 retry
func TestOnRegister_Fallback_NoRetryWithin24h(t *testing.T) {
	db := newTestDB(t)
	recent := time.Now().Add(-1 * time.Hour)
	d := &gbmodels.GbDevice{DeviceID: "34020000002000000001", Name: "test",
		SubscribeCapability: gbmodels.SubscribeFallback, SubscribeLastTest: &recent}
	require.NoError(t, db.Create(d).Error)
	sm := subscribe.NewStateMachine(db, zap.NewNop())

	sm.OnRegister(context.Background(), "34020000002000000001")

	var dev gbmodels.GbDevice
	require.NoError(t, db.Where("device_id = ?", "34020000002000000001").First(&dev).Error)
	// last_test 没变(没重试)
	assert.WithinDuration(t, recent, *dev.SubscribeLastTest, time.Second)
}
