package subscribe

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/uac"
)

type fakeSender struct {
	response uac.SubscriptionResponse
	err      error
	calls    []uac.SubscriptionRequest
}

func (f *fakeSender) SendSubscribe(_ context.Context, req uac.SubscriptionRequest) (uac.SubscriptionResponse, error) {
	f.calls = append(f.calls, req)
	return f.response, f.err
}

func newServiceTest(t *testing.T, sender *fakeSender) (*Service, *gorm.DB, *gbmodels.GbDevice) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gbmodels.GbDevice{}, &gbmodels.GbDeviceSubscription{}))
	device := &gbmodels.GbDevice{DeviceID: "34020000001320000001", IP: "192.168.1.10", Port: 5060, Transport: "UDP", Status: gbmodels.DeviceStatusOnline}
	require.NoError(t, db.Create(device).Error)
	now := time.Date(2026, 7, 19, 16, 0, 0, 0, time.Local)
	return NewService(db, sender, func() time.Time { return now }), db, device
}

func TestService_EnableActivatesOneKind(t *testing.T) {
	sender := &fakeSender{response: uac.SubscriptionResponse{StatusCode: 200, Expires: 3600, CallID: "call", LocalTag: "local", RemoteTag: "remote", CSeq: 1}}
	svc, db, device := newServiceTest(t, sender)

	sub, err := svc.Enable(context.Background(), device, gbmodels.SubscriptionKindCatalog)
	require.NoError(t, err)
	require.Equal(t, gbmodels.SubscriptionStatusActive, sub.Status)
	require.True(t, sub.Enabled)
	require.Equal(t, "call", sub.CallID)
	require.Len(t, sender.calls, 1)

	var count int64
	require.NoError(t, db.Model(&gbmodels.GbDeviceSubscription{}).Where("device_id = ?", device.ID).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestService_EnableRejectsOfflineAndDegradesOnFailure(t *testing.T) {
	sender := &fakeSender{err: errors.New("timeout")}
	svc, _, device := newServiceTest(t, sender)

	device.Status = gbmodels.DeviceStatusOffline
	_, err := svc.Enable(context.Background(), device, gbmodels.SubscriptionKindAlarm)
	require.ErrorContains(t, err, "设备离线")
	require.Empty(t, sender.calls)

	device.Status = gbmodels.DeviceStatusOnline
	sub, err := svc.Enable(context.Background(), device, gbmodels.SubscriptionKindAlarm)
	require.ErrorContains(t, err, "timeout")
	require.Equal(t, gbmodels.SubscriptionStatusDegraded, sub.Status)
	require.Equal(t, 1, sub.RetryCount)
	require.NotNil(t, sub.NextActionAt)
}

func TestService_DisableStopsLocallyWhenRemoteCancelFails(t *testing.T) {
	sender := &fakeSender{response: uac.SubscriptionResponse{StatusCode: 200, Expires: 3600, CallID: "call", CSeq: 1}}
	svc, _, device := newServiceTest(t, sender)
	_, err := svc.Enable(context.Background(), device, gbmodels.SubscriptionKindMobilePosition)
	require.NoError(t, err)

	sender.err = errors.New("network down")
	sub, err := svc.Disable(context.Background(), device, gbmodels.SubscriptionKindMobilePosition)
	require.ErrorContains(t, err, "network down")
	require.False(t, sub.Enabled)
	require.Equal(t, gbmodels.SubscriptionStatusDisabled, sub.Status)
	require.Nil(t, sub.NextActionAt)
	require.Len(t, sender.calls, 2)
	require.Equal(t, 0, sender.calls[1].Expires)
}
