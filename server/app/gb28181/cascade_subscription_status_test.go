package gb28181

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	cascadeservice "uvplatform.com/uvp-gb28181/app/gb28181/cascade/service"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/sipclient"
)

func TestCascadeSubscriptionSnapshotLifecycle(t *testing.T) {
	_, p, store := cascadeDeviceStatusFixture(t, "2022")
	transport := &cascadeStatusTransport{requests: make(chan *sip.Request, 20)}
	m := newCascadeCatalogSubscriptions(store, newCascadePlatformClientFactory(transport, nil, time.Second), time.Hour)
	defer m.Shutdown(context.Background())
	require.Equal(t, "inactive", m.Subscriptions(p).Catalog.State)
	// Exercise readers while NOTIFY and renewal update the session concurrently.
	stop := make(chan struct{})
	var readers sync.WaitGroup
	for i := 0; i < 3; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
					m.Subscriptions(p)
					time.Sleep(time.Millisecond)
				}
			}
		}()
	}
	defer func() { close(stop); readers.Wait() }()

	req := newCatalogSubscribeRequest(t, p, p.LocalDeviceID)
	response := subscribeCatalog(t, m, req)
	require.Equal(t, 200, response.StatusCode)
	awaitCascadeStatusResponse(t, transport.requests)
	require.Eventually(t, func() bool {
		v := m.Subscriptions(p)
		return len(v.Catalog.Sessions) == 1 && v.Catalog.Sessions[0].LastNotifyStatus == "success"
	}, time.Second, time.Millisecond)
	v := m.Subscriptions(p)
	require.Equal(t, "active", v.Catalog.State)
	s := v.Catalog.Sessions[0]
	require.Equal(t, p.LocalDeviceID, s.Target)
	require.Equal(t, "Catalog;id=1", s.Event)
	require.False(t, s.SubscribedAt.IsZero())
	require.Equal(t, s.SubscribedAt, s.RenewedAt)
	require.NotNil(t, s.LastNotifyAt)
	require.Equal(t, 200, s.LastNotifyCode)
	require.Equal(t, "unsupported", v.MobilePosition.State)
	require.Equal(t, "unsupported", v.PTZPosition.State)
	require.Equal(t, "unsupported", v.Alarm.State)
	// Returned snapshots must not alias mutable session state.
	v.Catalog.Sessions[0].Target = "changed"
	require.Equal(t, p.LocalDeviceID, m.Subscriptions(p).Catalog.Sessions[0].Target)

	other := p
	other.ID++
	require.Empty(t, m.Subscriptions(other).Catalog.Sessions)
	stale := p
	stale.ConfigRevision++
	require.Empty(t, m.Subscriptions(stale).Catalog.Sessions)
	disabled := p
	disabled.Enabled = false
	require.Empty(t, m.Subscriptions(disabled).Catalog.Sessions)

	tag, _ := response.To().Params.Get("tag")
	refresh := req.Clone()
	refresh.To().Params.Add("tag", tag)
	refresh.CSeq().SeqNo++
	require.Equal(t, 200, subscribeCatalog(t, m, refresh).StatusCode)
	require.True(t, m.Subscriptions(p).Catalog.Sessions[0].RenewedAt.After(s.RenewedAt))
	require.Equal(t, s.SubscribedAt, m.Subscriptions(p).Catalog.Sessions[0].SubscribedAt)
	awaitCascadeStatusResponse(t, transport.requests)
	// Cancellation is immediately invisible even before the worker removes it.
	refresh.CSeq().SeqNo++
	refresh.RemoveHeader("Expires")
	refresh.AppendHeader(sip.NewHeader("Expires", "0"))
	require.Equal(t, 200, subscribeCatalog(t, m, refresh).StatusCode)
	require.Equal(t, "inactive", m.Subscriptions(p).Catalog.State)
	require.NoError(t, m.Shutdown(context.Background()))
	require.Equal(t, "unavailable", m.Subscriptions(p).Catalog.State)
}

func TestCascadeSubscriptionSnapshotFiltersExpiredAndMultipleDialogs(t *testing.T) {
	_, p, store := cascadeDeviceStatusFixture(t, "2022")
	m := newCascadeCatalogSubscriptions(store, nil, time.Hour)
	defer m.Shutdown(context.Background())
	now := time.Now()
	for key, expiry := range map[string]time.Time{"one": now.Add(time.Minute), "two": now.Add(time.Hour), "expired": now.Add(-time.Second)} {
		m.sessions[key] = &cascadeCatalogSubscription{key: key, platform: p, target: p.LocalDeviceID,
			expires: expiry, subscribedAt: now, renewedAt: now, dialog: subscriptionSnapshotDialog(key)}
	}
	require.Len(t, m.Subscriptions(p).Catalog.Sessions, 2)
	runtime := &cascadeSubscriptionRuntime{subscriptions: m}
	// The media management wrapper must preserve the optional snapshot interface.
	var provider cascadeservice.SubscriptionReader = cascadeVideoManagementRuntime{runtime}
	require.Len(t, provider.Subscriptions(p).Catalog.Sessions, 2)
}

func TestCascadeManagementSubscriptionSnapshotCombinesAllTypes(t *testing.T) {
	_, p, store := cascadeDeviceStatusFixture(t, "2022")
	catalogs := newCascadeCatalogSubscriptions(store, nil, time.Hour)
	positions := newCascadeMobilePositionSubscriptions(store, nil, time.Hour)
	events := newCascadeEventSubscriptions(store, nil, time.Hour)
	now := time.Now()
	expires := now.Add(time.Minute)
	catalogs.sessions["catalog"] = &cascadeCatalogSubscription{
		key: "catalog", platform: p, target: p.LocalDeviceID, expires: expires,
		subscribedAt: now, renewedAt: now, dialog: subscriptionSnapshotDialog("catalog"),
	}
	positions.sessions["position"] = &cascadeMobilePositionSubscription{
		key: "position", platform: p, target: p.LocalDeviceID, expires: expires,
		subscribedAt: now, renewedAt: now, dialog: sipclient.NotifyDialog{CallID: "position", Event: "presence"},
	}
	events.sessions["ptz"] = &cascadeEventSubscription{
		key: "ptz", kind: cascadeEventPTZ, platform: p, target: "34020000001320000012", expires: expires,
		subscribedAt: now, renewedAt: now, dialog: sipclient.NotifyDialog{CallID: "ptz", Event: "PTZPosition"},
	}
	events.sessions["alarm"] = &cascadeEventSubscription{
		key: "alarm", kind: cascadeEventAlarm, platform: p, target: p.LocalDeviceID, expires: expires,
		subscribedAt: now, renewedAt: now, dialog: sipclient.NotifyDialog{CallID: "alarm", Event: "presence"},
	}
	runtime := &cascadeSubscriptionRuntime{subscriptions: catalogs, positions: positions, events: events}
	var reader cascadeservice.SubscriptionReader = cascadeVideoManagementRuntime{runtime}
	status := reader.Subscriptions(p)
	require.Equal(t, "active", status.Catalog.State)
	require.Equal(t, "active", status.MobilePosition.State)
	require.Equal(t, "active", status.PTZPosition.State)
	require.Equal(t, "active", status.Alarm.State)
	require.Len(t, status.Catalog.Sessions, 1)
	require.Len(t, status.MobilePosition.Sessions, 1)
	require.Len(t, status.PTZPosition.Sessions, 1)
	require.Len(t, status.Alarm.Sessions, 1)
	require.Equal(t, "34020000001320000012", status.PTZPosition.Sessions[0].Target)
	require.Equal(t, "PTZPosition", status.PTZPosition.Sessions[0].Event)
	require.NoError(t, catalogs.Shutdown(context.Background()))
	require.NoError(t, positions.Shutdown(context.Background()))
	require.NoError(t, events.Shutdown(context.Background()))
}

func subscriptionSnapshotDialog(id string) sipclient.NotifyDialog {
	return sipclient.NotifyDialog{CallID: id, Event: "Catalog;id=1"}
}

func TestCascadeSubscriptionSnapshotShowsPendingAndFailedNotify(t *testing.T) {
	_, p, store := cascadeDeviceStatusFixture(t, "2022")
	release := make(chan struct{})
	tr := &subscriptionTransport{requests: make(chan *sip.Request, 10), status: 500, before: func(ctx context.Context, _ *sip.Request) error {
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	m := newCascadeCatalogSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), time.Hour)
	defer m.Shutdown(context.Background())
	require.Equal(t, 200, subscribeCatalog(t, m, newCatalogSubscribeRequest(t, p, p.LocalDeviceID)).StatusCode)
	awaitCascadeStatusResponse(t, tr.requests)
	pending := m.Subscriptions(p).Catalog
	require.Equal(t, "active", pending.State)
	require.Len(t, pending.Sessions, 1)
	require.Equal(t, "pending", pending.Sessions[0].LastNotifyStatus)
	require.Nil(t, pending.Sessions[0].LastNotifyAt)
	close(release)
	require.Eventually(t, func() bool {
		status := m.Subscriptions(p).Catalog
		return len(status.Sessions) == 1 && status.Sessions[0].LastNotifyStatus == "failed"
	}, time.Second, time.Millisecond)
	status := m.Subscriptions(p).Catalog
	require.Equal(t, 500, status.Sessions[0].LastNotifyCode)
	require.NotNil(t, status.Sessions[0].LastNotifyAt)
	timestamp := *status.Sessions[0].LastNotifyAt
	*status.Sessions[0].LastNotifyAt = time.Time{}
	require.Equal(t, timestamp, *m.Subscriptions(p).Catalog.Sessions[0].LastNotifyAt)
}
