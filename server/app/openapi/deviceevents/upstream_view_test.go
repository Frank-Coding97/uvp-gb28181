package deviceevents

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func TestSubscriptionUpstreamReportsSharedStateWithoutChangingClientContract(t *testing.T) {
	service, db, scope, rootID, otherID, now := newDeviceEventsTestService(t, &subscriptionStub{})
	expires, subscribed, notified := now.Add(time.Hour), now.Add(-time.Minute), now.Add(-time.Second)
	shared := gbmodels.GbDeviceSubscription{DeviceID: rootID, Kind: gbmodels.SubscriptionKindMobilePosition, Enabled: true, Status: gbmodels.SubscriptionStatusDegraded, ExpiresSeconds: 3600, IntervalSeconds: 30, ExpiresAt: &expires, LastSubscribeAt: &subscribed, LastNotifyAt: &notified, RetryCount: 2, LastStatusCode: 503, LastError: "设备暂未确认订阅"}
	require.NoError(t, db.Create(&shared).Error)
	require.NoError(t, db.Create(&gbmodels.GbDeviceSubscription{DeviceID: otherID, Kind: gbmodels.SubscriptionKindMobilePosition, Enabled: true, Status: gbmodels.SubscriptionStatusActive, LastError: "private-other-device"}).Error)
	request := Request{OpenAPIClientID: 71, ResourceScope: scope, DeviceID: deviceEventsRootDevice, Kind: EventMobilePosition, ExpiresSeconds: 600, IntervalSeconds: 5}
	created, err := service.CreateSubscription(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, "enabled", created.Status)
	require.Equal(t, 600, created.ExpiresSeconds)
	require.NotNil(t, created.Upstream)
	require.Equal(t, "degraded", created.Upstream.Status)
	require.Equal(t, 3600, created.Upstream.ExpiresSeconds)
	require.Equal(t, 30, created.Upstream.IntervalSeconds)
	require.Equal(t, 2, created.Upstream.RetryCount)
	require.Equal(t, 503, created.Upstream.LastStatusCode)
	require.Equal(t, "设备暂未确认订阅", created.Upstream.LastError)
	require.True(t, created.Upstream.LastNotifyAt.Equal(notified))
	request.SubscriptionID = created.ID
	cancelled, err := service.CancelSubscription(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, "cancelled", cancelled.Status)
	require.True(t, cancelled.Upstream.Enabled)
	require.Equal(t, "degraded", cancelled.Upstream.Status)
	list, err := service.ListSubscriptions(context.Background(), request)
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	require.Equal(t, 30, list.Items[0].Upstream.IntervalSeconds)
}

func TestSubscriptionUpstreamMissingIsNullAndDatabaseFailuresAreVisible(t *testing.T) {
	service, db, scope, _, _, _ := newDeviceEventsTestService(t, &subscriptionStub{})
	request := Request{OpenAPIClientID: 71, ResourceScope: scope, DeviceID: deviceEventsRootDevice, Kind: EventAlarm, ExpiresSeconds: 600}
	created, err := service.CreateSubscription(context.Background(), request)
	require.NoError(t, err)
	require.Nil(t, created.Upstream)
	request.SubscriptionID = created.ID
	require.NoError(t, db.Migrator().DropTable(&gbmodels.GbDeviceSubscription{}))
	_, err = service.GetSubscription(context.Background(), request)
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = service.ListSubscriptions(context.Background(), request)
	require.ErrorIs(t, err, ErrUnavailable)
	request.Kind = EventDeviceStatus
	request.SubscriptionID = ""
	status, err := service.CreateSubscription(context.Background(), request)
	require.NoError(t, err)
	require.Nil(t, status.Upstream)
}
