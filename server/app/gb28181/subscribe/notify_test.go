package subscribe

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

func TestOnNotify_MatchesDialogAndDispatches(t *testing.T) {
	sender := &fakeSender{response: uac.SubscriptionResponse{StatusCode: 200, Expires: 3600, CallID: "dialog-call", CSeq: 1}}
	svc, db, device := newServiceTest(t, sender)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbChannel{}))
	_, err := svc.Enable(context.Background(), device, gbmodels.SubscriptionKindCatalog)
	require.NoError(t, err)
	called := false
	svc.SetProcessor(gbmodels.SubscriptionKindCatalog, ProcessorFunc(func(_ context.Context, got *gbmodels.GbDevice, _ Notification) error {
		called = got.ID == device.ID
		return nil
	}))
	require.NoError(t, svc.OnNotify(context.Background(), Notification{Kind: gbmodels.SubscriptionKindCatalog, DeviceCode: device.DeviceID, CallID: "dialog-call"}))
	require.True(t, called)
	var sub gbmodels.GbDeviceSubscription
	require.NoError(t, db.Where("device_id = ? AND kind = ?", device.ID, gbmodels.SubscriptionKindCatalog).First(&sub).Error)
	require.NotNil(t, sub.LastNotifyAt)
}

func TestOnNotify_TerminatedExpiresWithoutDispatch(t *testing.T) {
	sender := &fakeSender{response: uac.SubscriptionResponse{StatusCode: 200, Expires: 3600, CallID: "dialog-call", CSeq: 1}}
	svc, db, device := newServiceTest(t, sender)
	_, err := svc.Enable(context.Background(), device, gbmodels.SubscriptionKindAlarm)
	require.NoError(t, err)
	require.NoError(t, svc.OnNotify(context.Background(), Notification{Kind: gbmodels.SubscriptionKindAlarm, DeviceCode: device.DeviceID, CallID: "dialog-call", SubscriptionState: "terminated", Expires: 0}))
	var sub gbmodels.GbDeviceSubscription
	require.NoError(t, db.Where("device_id = ? AND kind = ?", device.ID, gbmodels.SubscriptionKindAlarm).First(&sub).Error)
	require.Equal(t, gbmodels.SubscriptionStatusExpired, sub.Status)
	require.Nil(t, sub.NextActionAt)
}
