package subscribe

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

type schedulerTestConfig struct {
	values map[string]interface{}
}

func (c *schedulerTestConfig) ConfigFileChangeListen(...func()) {}
func (c *schedulerTestConfig) Get(key string) interface{}       { return c.values[key] }
func (c *schedulerTestConfig) GetString(key string) string {
	value, _ := c.values[key].(string)
	return value
}
func (c *schedulerTestConfig) GetBool(key string) bool {
	value, _ := c.values[key].(bool)
	return value
}
func (c *schedulerTestConfig) GetInt(string) int                { return 0 }
func (c *schedulerTestConfig) GetInt32(string) int32            { return 0 }
func (c *schedulerTestConfig) GetInt64(string) int64            { return 0 }
func (c *schedulerTestConfig) GetFloat64(string) float64        { return 0 }
func (c *schedulerTestConfig) GetDuration(string) time.Duration { return 0 }
func (c *schedulerTestConfig) GetStringSlice(key string) []string {
	value, _ := c.values[key].([]string)
	return value
}
func (c *schedulerTestConfig) GetUintSlice(string) []uint        { return nil }
func (c *schedulerTestConfig) Set(key string, value interface{}) { c.values[key] = value }
func (c *schedulerTestConfig) SaveConfig() error                 { return nil }

func TestRunDue_RenewsOnlineAndPausesOffline(t *testing.T) {
	sender := &fakeSender{response: uac.SubscriptionResponse{StatusCode: 200, Expires: 3600, CallID: "call", CSeq: 1}}
	svc, db, online := newServiceTest(t, sender)
	_, err := svc.Enable(context.Background(), online, gbmodels.SubscriptionKindCatalog)
	require.NoError(t, err)

	offline := &gbmodels.GbDevice{DeviceID: "34020000001320000002", IP: "192.168.1.11", Port: 5060, Status: gbmodels.DeviceStatusOffline}
	require.NoError(t, db.Create(offline).Error)
	now := svc.now()
	require.NoError(t, db.Create(&gbmodels.GbDeviceSubscription{DeviceID: offline.ID, Kind: gbmodels.SubscriptionKindAlarm, Enabled: true, Status: gbmodels.SubscriptionStatusActive, Event: "presence", NextActionAt: &now}).Error)
	require.NoError(t, db.Model(&gbmodels.GbDeviceSubscription{}).Where("device_id = ?", online.ID).Update("next_action_at", now).Error)

	require.NoError(t, svc.RunDue(context.Background()))
	require.Len(t, sender.calls, 2, "only online due subscription is renewed")
	var sub gbmodels.GbDeviceSubscription
	require.NoError(t, db.Where("device_id = ? AND kind = ?", offline.ID, gbmodels.SubscriptionKindAlarm).First(&sub).Error)
	require.Equal(t, gbmodels.SubscriptionStatusExpired, sub.Status)
	require.Nil(t, sub.NextActionAt)
}

func TestWakeDevice_MarksEnabledRowsDue(t *testing.T) {
	sender := &fakeSender{}
	svc, db, device := newServiceTest(t, sender)
	future := svc.now().Add(time.Hour)
	require.NoError(t, db.Create(&gbmodels.GbDeviceSubscription{DeviceID: device.ID, Kind: gbmodels.SubscriptionKindAlarm, Enabled: true, Status: gbmodels.SubscriptionStatusExpired, Event: "presence", NextActionAt: &future}).Error)
	require.NoError(t, svc.WakeDevice(context.Background(), device.ID))
	var sub gbmodels.GbDeviceSubscription
	require.NoError(t, db.Where("device_id = ? AND kind = ?", device.ID, gbmodels.SubscriptionKindAlarm).First(&sub).Error)
	require.WithinDuration(t, svc.now(), *sub.NextActionAt, time.Second)
}

func TestWakeDeviceByCode_OnlyMarksEnabledRowsDue(t *testing.T) {
	sender := &fakeSender{}
	svc, db, device := newServiceTest(t, sender)
	future := svc.now().Add(time.Hour)
	require.NoError(t, db.Create(&gbmodels.GbDeviceSubscription{DeviceID: device.ID, Kind: gbmodels.SubscriptionKindCatalog, Enabled: true, Status: gbmodels.SubscriptionStatusExpired, Event: "Catalog", NextActionAt: &future}).Error)
	require.NoError(t, db.Create(&gbmodels.GbDeviceSubscription{DeviceID: device.ID, Kind: gbmodels.SubscriptionKindAlarm, Enabled: false, Status: gbmodels.SubscriptionStatusDisabled, Event: "presence"}).Error)

	require.NoError(t, svc.WakeDeviceByCode(context.Background(), device.DeviceID))

	var enabled, disabled gbmodels.GbDeviceSubscription
	require.NoError(t, db.Where("device_id = ? AND kind = ?", device.ID, gbmodels.SubscriptionKindCatalog).First(&enabled).Error)
	require.NoError(t, db.Where("device_id = ? AND kind = ?", device.ID, gbmodels.SubscriptionKindAlarm).First(&disabled).Error)
	require.WithinDuration(t, svc.now(), *enabled.NextActionAt, time.Second)
	require.Nil(t, disabled.NextActionAt)
}

func TestWakeDevice_AppliesGlobalDefaultsWithoutOverridingDeviceSettings(t *testing.T) {
	previous := app.ConfigYml
	t.Cleanup(func() { app.ConfigYml = previous })
	app.ConfigYml = &schedulerTestConfig{values: map[string]interface{}{
		gbconfig.GlobalSubscriptionItemsConfigKey: []string{"catalog", "alarm", "ptz_precise_position"},
	}}

	svc, db, device := newServiceTest(t, &fakeSender{})
	require.NoError(t, db.Create(&gbmodels.GbDeviceSubscription{
		DeviceID: device.ID, Kind: gbmodels.SubscriptionKindAlarm, Enabled: false,
		Status: gbmodels.SubscriptionStatusDisabled, Event: "presence",
	}).Error)

	require.NoError(t, svc.WakeDevice(context.Background(), device.ID))

	var catalog, alarm, ptz gbmodels.GbDeviceSubscription
	require.NoError(t, db.Where("device_id = ? AND kind = ?", device.ID, gbmodels.SubscriptionKindCatalog).First(&catalog).Error)
	require.True(t, catalog.Enabled)
	require.Equal(t, gbmodels.SubscriptionStatusPending, catalog.Status)
	require.NotNil(t, catalog.NextActionAt)
	require.NoError(t, db.Where("device_id = ? AND kind = ?", device.ID, gbmodels.SubscriptionKindAlarm).First(&alarm).Error)
	require.False(t, alarm.Enabled, "设备级关闭配置不能被全局默认值覆盖")
	require.Nil(t, alarm.NextActionAt)
	require.NoError(t, db.Where("device_id = ? AND kind = ?", device.ID, gbmodels.SubscriptionKindPTZPrecisePosition).First(&ptz).Error)
	require.True(t, ptz.Enabled)
	require.Equal(t, "PTZPosition", ptz.Event)
}
