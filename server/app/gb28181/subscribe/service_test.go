package subscribe

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
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

func TestService_PTZPrecisePositionFailureWaitsForNextOnline(t *testing.T) {
	sender := &fakeSender{err: errors.New("not supported")}
	svc, db, device := newServiceTest(t, sender)

	sub, err := svc.Enable(context.Background(), device, gbmodels.SubscriptionKindPTZPrecisePosition)
	require.ErrorContains(t, err, "not supported")
	require.Equal(t, gbmodels.SubscriptionStatusDegraded, sub.Status)
	require.True(t, sub.Enabled)
	require.Nil(t, sub.NextActionAt)

	require.NoError(t, svc.WakeDevice(context.Background(), device.ID))
	require.NoError(t, db.Where("device_id = ? AND kind = ?", device.ID, gbmodels.SubscriptionKindPTZPrecisePosition).First(&sub).Error)
	require.WithinDuration(t, svc.now(), *sub.NextActionAt, time.Second)
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

func TestService_ConfigurePersistsPolicyAndResubscribes(t *testing.T) {
	sender := &fakeSender{response: uac.SubscriptionResponse{StatusCode: 200, Expires: 3600, CallID: "call", CSeq: 1}}
	svc, _, device := newServiceTest(t, sender)
	initial, err := svc.Enable(context.Background(), device, gbmodels.SubscriptionKindMobilePosition)
	require.NoError(t, err)
	require.Equal(t, 30, initial.IntervalSeconds)

	expires, interval := 7200, 15
	sub, err := svc.Configure(context.Background(), device, gbmodels.SubscriptionKindMobilePosition, nil, &expires, &interval)
	require.NoError(t, err)
	require.Equal(t, 7200, sub.ExpiresSeconds)
	require.Equal(t, 15, sub.IntervalSeconds)
	require.Len(t, sender.calls, 2)
	require.Equal(t, 7200, sender.calls[1].Expires)
	require.Contains(t, string(sender.calls[1].Body), "<Interval>15</Interval>")
}

// 目录订阅必须带 `Event: Catalog;id=N`，且续订要复用同一个 id。
//
// 现场形态（2026-10 级联实测）：上一级用本服务向下一级订阅目录，事件头只有
// `Catalog`，下一级回 `489 Bad Event`，订阅永远起不来。id 一旦在续订时变了，
// 下一级会把续订当成新的订阅（482/481），所以 id 必须从订阅行主键稳定推导。
func TestService_CatalogSubscribeCarriesStableEventID(t *testing.T) {
	sender := &fakeSender{response: uac.SubscriptionResponse{StatusCode: 200, Expires: 3600, CallID: "call", LocalTag: "local", RemoteTag: "remote", CSeq: 1}}
	svc, _, device := newServiceTest(t, sender)

	sub, err := svc.Enable(context.Background(), device, gbmodels.SubscriptionKindCatalog)
	require.NoError(t, err)
	require.Len(t, sender.calls, 1)
	require.Equal(t, fmt.Sprintf("Catalog;id=%d", sub.ID), sender.calls[0].Event)

	_, err = svc.Renew(context.Background(), device, gbmodels.SubscriptionKindCatalog)
	require.NoError(t, err)
	require.Len(t, sender.calls, 2)
	require.Equal(t, sender.calls[0].Event, sender.calls[1].Event, "续订换 id 会让下级把它当成另一次订阅")

	// 其余三类订阅的 Event 头不能被顺手加上 id。
	for _, kind := range []gbmodels.SubscriptionKind{
		gbmodels.SubscriptionKindMobilePosition, gbmodels.SubscriptionKindAlarm, gbmodels.SubscriptionKindPTZPrecisePosition,
	} {
		sender.calls = nil
		_, err := svc.Enable(context.Background(), device, kind)
		require.NoError(t, err)
		require.Len(t, sender.calls, 1)
		require.NotContains(t, sender.calls[0].Event, ";id=", kind)
	}
}

// 收到 481 必须清掉对话凭据，下一次按首包重新订阅。
//
// 现场形态（2026-10 级联实测）：对端（下一级）重启后订阅表清空，我方仍拿旧的
// Call-ID + 两个 tag 续订，对端每次回 481；退避按 1 分钟 → 5 分钟 → 30 分钟 →
// 24 小时递增，页面上就表现为「订阅一直异常、等半天也不恢复」。只有首包（不带
// To-tag）对端才会新建订阅，所以 481 之后必须把凭据清干净、退避重置成 1 分钟。
func TestService_RenewClearsStaleDialogOn481(t *testing.T) {
	sender := &fakeSender{response: uac.SubscriptionResponse{StatusCode: 200, Expires: 3600, CallID: "call-1", LocalTag: "l1", RemoteTag: "r1", CSeq: 1}}
	svc, db, device := newServiceTest(t, sender)

	sub, err := svc.Enable(context.Background(), device, gbmodels.SubscriptionKindCatalog)
	require.NoError(t, err)
	require.Equal(t, "call-1", sub.CallID)

	sender.response = uac.SubscriptionResponse{StatusCode: 481, CallID: "call-1", LocalTag: "l1", RemoteTag: "r1", CSeq: 2}
	sender.err = errors.New("SUBSCRIBE 应答非2xx: 481 Subscription Does Not Exist")
	backoff, err := svc.Renew(context.Background(), device, gbmodels.SubscriptionKindCatalog)
	require.ErrorContains(t, err, "481")
	require.Equal(t, gbmodels.SubscriptionStatusDegraded, backoff.Status)
	require.EqualValues(t, 481, backoff.LastStatusCode)

	var stored gbmodels.GbDeviceSubscription
	require.NoError(t, db.First(&stored, sub.ID).Error)
	require.Empty(t, stored.CallID, "留着重启前的 Call-ID 只会一直 481")
	require.Empty(t, stored.LocalTag)
	require.Empty(t, stored.RemoteTag)
	require.Zero(t, stored.CSeq)
	require.Zero(t, stored.RetryCount, "清凭据后要立刻重试，不能继续按指数退避等下去")
	require.NotNil(t, stored.NextActionAt)
	require.WithinDuration(t, svc.now().Add(time.Minute), *stored.NextActionAt, time.Second)

	// 下一次必须是首包：不带任何对话凭据，对端才会新建订阅。
	sender.err = nil
	sender.response = uac.SubscriptionResponse{StatusCode: 200, Expires: 3600, CallID: "call-2", LocalTag: "l2", RemoteTag: "r2", CSeq: 1}
	_, err = svc.Renew(context.Background(), device, gbmodels.SubscriptionKindCatalog)
	require.NoError(t, err)
	last := sender.calls[len(sender.calls)-1]
	require.Empty(t, last.CallID)
	require.Empty(t, last.LocalTag)
	require.Empty(t, last.RemoteTag)
	require.Equal(t, "Catalog;id="+strconv.FormatUint(uint64(sub.ID), 10), last.Event)
}
