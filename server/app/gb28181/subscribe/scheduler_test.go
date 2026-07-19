package subscribe

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

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
