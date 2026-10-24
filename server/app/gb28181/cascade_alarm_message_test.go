package gb28181

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/repository"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/sipclient"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/subscribe"
)

const cascadeAlarmMessageBody = `<Notify><CmdType>Alarm</CmdType><SN>7</SN><DeviceID>source-only-channel</DeviceID><AlarmPriority>1</AlarmPriority><AlarmMethod>2</AlarmMethod><AlarmTime>2026-10-22T10:00:00</AlarmTime><AlarmDescription>门禁告警</AlarmDescription><Password>private-secret</Password><PrivateDeviceID>private-id</PrivateDeviceID></Notify>`

func TestCascadeAlarmMessageWithoutSubscription(t *testing.T) {
	for _, version := range []string{"2016", "2022"} {
		t.Run(version, func(t *testing.T) {
			_, platform, store := cascadeAlarmFixture(t, version)
			transport := &subscriptionTransport{requests: make(chan *sip.Request, 10), status: 200}
			manager := newCascadeEventSubscriptions(store, newCascadePlatformClientFactory(transport, nil, time.Second), time.Hour)
			defer manager.Shutdown(context.Background())
			require.NoError(t, manager.BroadcastAlarm(context.Background(), "source-only-device", "source-call", "4 MESSAGE", []byte(cascadeAlarmMessageBody)))
			require.Len(t, transport.requests, 1, "a published alarm must reach an upstream without a subscription")
			request := <-transport.requests
			require.Equal(t, sip.MESSAGE, request.Method)
			require.Equal(t, platform.UpstreamServerID, request.To().Address.User)
			require.Equal(t, platform.LocalDeviceID, request.From().Address.User)
			require.NotEqual(t, "source-call", request.CallID().Value(), "do not reuse a device's transaction identity across upstreams")
			alarm, err := manscdp.ParseAlarmNotify(request.Body())
			require.NoError(t, err)
			require.Equal(t, "34020000001320000012", alarm.DeviceID)
			require.Equal(t, "7", alarm.SN)
			require.Equal(t, "门禁告警", alarm.Description)
			for _, private := range []string{"source-only", "private-secret", "PrivateDeviceID", "Password"} {
				require.NotContains(t, string(request.Body()), private)
			}
		})
	}
}

func TestCascadeAlarmDeliveryResumesAfterRuntimeRecreation(t *testing.T) {
	db, platform, store := cascadeAlarmFixture(t, "2022")
	body, err := buildCascadeEventBody(cascadeWireProfile(platform), cascadeEventAlarm, "34020000001320000012", 77, &manscdp.AlarmNotify{
		SN: "77", DeviceID: "source-only-channel", Priority: 1, Method: 2, Description: "door alarm",
	})
	require.NoError(t, err)
	item := model.GbCascadeAlarmDelivery{
		DeliveryKey: "durable-alarm-upstream-1", PlatformID: platform.ID, SourceDeviceCode: "source-only-device",
		SourceResourceCode: "source-only-channel", PublishedCode: "34020000001320000012", ProfileVersion: "2022",
		Payload: body, NextAttemptAt: time.Now().Add(-time.Second),
	}
	bindingRuntime := &cascadeEventSubscriptions{store: store}
	item.BindingFingerprint, err = bindingRuntime.alarmBinding(t.Context(), platform, item.SourceDeviceCode, item.SourceResourceCode, item.PublishedCode)
	require.NoError(t, err)
	require.NoError(t, db.Create(&item).Error)
	transport := &subscriptionTransport{requests: make(chan *sip.Request, 2), status: 200}
	manager := newCascadeEventSubscriptions(store, newCascadePlatformClientFactory(transport, nil, time.Second), time.Hour)
	defer manager.Shutdown(context.Background())
	select {
	case request := <-transport.requests:
		require.Equal(t, sip.MESSAGE, request.Method)
		notify, err := manscdp.ParseAlarmNotify(request.Body())
		require.NoError(t, err)
		require.Equal(t, "34020000001320000012", notify.DeviceID)
	case <-time.After(3 * time.Second):
		t.Fatal("runtime did not resume the persisted alarm delivery")
	}
	require.Eventually(t, func() bool {
		var got model.GbCascadeAlarmDelivery
		if err := db.First(&got, item.ID).Error; err != nil {
			return false
		}
		return got.State == model.CascadeAlarmDeliverySent && got.SentAt != nil
	}, time.Second, 10*time.Millisecond)
}

func addCascadeAlarmTestSession(t *testing.T, manager *cascadeEventSubscriptions, p model.GbCascadePlatform) *cascadeEventSubscription {
	t.Helper()
	client, err := manager.clients.NewClient(p)
	require.NoError(t, err)
	from := sip.FromHeader{Address: sip.Uri{Scheme: "sip", User: p.LocalDeviceID, Host: p.LocalDomain}, Params: sip.NewParams()}
	from.Params.Add("tag", "local")
	to := sip.ToHeader{Address: sip.Uri{Scheme: "sip", User: p.UpstreamServerID, Host: p.UpstreamDomain}, Params: sip.NewParams()}
	to.Params.Add("tag", "remote")
	s := &cascadeEventSubscription{key: "alarm-sub", kind: cascadeEventAlarm, platform: p,
		client: client.(*cascadePlatformClient).transactions,
		dialog: sipclient.NotifyDialog{CallID: "alarm-sub", From: from, To: to, RemoteTarget: sip.Uri{Scheme: "sip", User: p.UpstreamServerID, Host: p.Host, Port: p.Port}, Event: "Alarm"},
		target: p.LocalDeviceID, sn: 90, expires: time.Now().Add(time.Minute)}
	manager.mu.Lock()
	manager.sessions[s.key] = s
	manager.mu.Unlock()
	return s
}

func TestCascadeAlarmMessagePrefersMatchingSubscriptionEvenOnFailure(t *testing.T) {
	for _, status := range []int{200, 500, 481} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			_, p, store := cascadeAlarmFixture(t, "2022")
			tr := &subscriptionTransport{requests: make(chan *sip.Request, 10), status: status}
			m := newCascadeEventSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), time.Hour)
			defer m.Shutdown(context.Background())
			addCascadeAlarmTestSession(t, m, p)
			err := m.BroadcastAlarm(context.Background(), "source-only-device", "source-call", "4 MESSAGE", []byte(cascadeAlarmMessageBody))
			if status == 200 {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, fmt.Sprint(status))
			}
			require.Len(t, tr.requests, 1, "a failed NOTIFY must not trigger a second delivery via MESSAGE")
			req := <-tr.requests
			require.Equal(t, sip.NOTIFY, req.Method)
			alarm, err := manscdp.ParseAlarmNotify(req.Body())
			require.NoError(t, err)
			require.Equal(t, "90", alarm.SN)
			require.Equal(t, "34020000001320000012", alarm.DeviceID)
		})
	}
}

func TestCascadeAlarmMessageIgnoresNonmatchingSubscriptions(t *testing.T) {
	for _, scenario := range []string{"expired", "cancelled", "other target", "old config", "old profile", "platform unpublished"} {
		t.Run(scenario, func(t *testing.T) {
			db, p, store := cascadeAlarmFixture(t, "2022")
			tr := &subscriptionTransport{requests: make(chan *sip.Request, 10), status: 200}
			m := newCascadeEventSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), time.Hour)
			defer m.Shutdown(context.Background())
			s := addCascadeAlarmTestSession(t, m, p)
			switch scenario {
			case "expired":
				s.expires = time.Now().Add(-time.Second)
			case "cancelled":
				s.reason = "deactivated"
			case "other target":
				s.target = "34020000001320000999"
			case "old config":
				s.platform.ConfigRevision++
			case "old profile":
				s.platform.EffectiveVersion = "2016"
			case "platform unpublished":
				require.NoError(t, db.Model(&p).Update("publish_platform", false).Error)
			}
			require.NoError(t, m.BroadcastAlarm(context.Background(), "source-only-device", "source-call", "4 MESSAGE", []byte(cascadeAlarmMessageBody)))
			require.Len(t, tr.requests, 1)
			require.Equal(t, sip.MESSAGE, (<-tr.requests).Method)
		})
	}
}

func TestCascadeAlarmSourceRejectsRevokedOrMismatchedResource(t *testing.T) {
	for _, statement := range []string{
		"UPDATE gb_device SET deleted_at=CURRENT_TIMESTAMP WHERE id=10",
		"UPDATE gb_channel SET deleted_at=CURRENT_TIMESTAMP WHERE id=20",
		"UPDATE gb_channel SET owner_dept_id=1 WHERE id=20",
		"UPDATE gb_channel SET device_id='different-device' WHERE id=20",
		"UPDATE gb_cascade_device_projection SET active=0",
		"UPDATE gb_cascade_channel_projection SET active=0",
		"UPDATE gb_cascade_platform SET enabled=0",
		"UPDATE gb_cascade_platform SET config_revision=config_revision+1",
		"UPDATE gb_cascade_platform SET projection_revision=projection_revision+1",
	} {
		t.Run(statement, func(t *testing.T) {
			db, p, store := cascadeAlarmFixture(t, "2022")
			require.NoError(t, db.Exec(statement).Error)
			m := newCascadeEventSubscriptions(store, nil, time.Hour)
			defer m.Shutdown(context.Background())
			for _, kind := range []cascadeEventSubscriptionKind{cascadeEventAlarm, cascadeEventPTZ} {
				_, _, allowed, err := m.resolveSource(context.Background(), p, kind, "source-only-device", "source-only-channel")
				require.NoError(t, err)
				require.False(t, allowed, "revoked or mismatched source is never publishable")
			}
		})
	}
}

func TestCascadeAlarmMessageReportsSIPFailure(t *testing.T) {
	_, _, store := cascadeAlarmFixture(t, "2022")
	tr := &subscriptionTransport{requests: make(chan *sip.Request, 10), status: 500}
	m := newCascadeEventSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), time.Hour)
	defer m.Shutdown(context.Background())
	err := m.BroadcastAlarm(context.Background(), "source-only-device", "source-call", "4 MESSAGE", []byte(cascadeAlarmMessageBody))
	require.ErrorContains(t, err, "500")
	require.Len(t, tr.requests, 1)
}

func TestCascadeAlarmMessageDeliveryScope(t *testing.T) {
	for _, scenario := range []struct {
		name, sql, source, target string
		sends                     int
	}{
		{name: "device event", source: "source-only-device", target: "source-only-device", sends: 1},
		{name: "offline event", sql: "UPDATE gb_device SET status=0", source: "source-only-device", target: "source-only-channel", sends: 1},
		{name: "platform disabled", sql: "UPDATE gb_cascade_platform SET enabled=0", source: "source-only-device", target: "source-only-channel"},
		{name: "device removed", sql: "UPDATE gb_device SET deleted_at=CURRENT_TIMESTAMP", source: "source-only-device", target: "source-only-device"},
		{name: "channel removed", sql: "UPDATE gb_channel SET deleted_at=CURRENT_TIMESTAMP", source: "source-only-device", target: "source-only-channel"},
		{name: "channel owner changed", sql: "UPDATE gb_channel SET owner_dept_id=1", source: "source-only-device", target: "source-only-channel"},
		{name: "hidden channel", sql: "UPDATE gb_cascade_channel_projection SET active=0", source: "source-only-device", target: "source-only-channel"},
		{name: "hidden device", sql: "UPDATE gb_cascade_device_projection SET active=0", source: "source-only-device", target: "source-only-channel"},
		{name: "unknown source", source: "other-device", target: "source-only-channel"},
		{name: "unknown channel", source: "source-only-device", target: "unknown-channel"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			db, _, store := cascadeAlarmFixture(t, "2022")
			if scenario.sql != "" {
				require.NoError(t, db.Exec(scenario.sql).Error)
			}
			tr := &subscriptionTransport{requests: make(chan *sip.Request, 10), status: 200}
			m := newCascadeEventSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), time.Hour)
			defer m.Shutdown(context.Background())
			body := []byte(fmt.Sprintf(`<Notify><CmdType>Alarm</CmdType><SN>7</SN><DeviceID>%s</DeviceID><AlarmPriority>1</AlarmPriority><AlarmMethod>2</AlarmMethod></Notify>`, scenario.target))
			require.NoError(t, m.BroadcastAlarm(context.Background(), scenario.source, "source-call", "4 MESSAGE", body))
			require.Len(t, tr.requests, scenario.sends)
			if scenario.name == "device event" {
				alarm, err := manscdp.ParseAlarmNotify((<-tr.requests).Body())
				require.NoError(t, err)
				require.Equal(t, "34020000001320000011", alarm.DeviceID)
			}
		})
	}
}

func TestCascadeAlarmMessageUpstreamsAreIndependent(t *testing.T) {
	for _, revokeSecond := range []bool{false, true} {
		t.Run(fmt.Sprint(revokeSecond), func(t *testing.T) {
			db, p, store := cascadeAlarmFixture(t, "2022")
			projection, err := store.ProjectionSnapshot(context.Background(), p.ID)
			require.NoError(t, err)
			second := p
			second.ID, second.Name, second.Host, second.UpstreamServerID = 0, "second alarm upstream", "192.0.2.11", "34020000002000000003"
			require.NoError(t, db.Create(&second).Error)
			device := projection.Devices[0]
			device.ID, device.PlatformID, device.PublishedDeviceID = 0, second.ID, "34020000001320000021"
			require.NoError(t, db.Create(&device).Error)
			channel := projection.Channels[0]
			channel.ID, channel.PlatformID, channel.DeviceProjectionID, channel.PublishedChannelID = 0, second.ID, device.ID, "34020000001320000022"
			require.NoError(t, db.Create(&channel).Error)
			tr := &subscriptionTransport{requests: make(chan *sip.Request, 10), status: 200}
			tr.before = func(_ context.Context, req *sip.Request) error {
				if req.To().Address.User == p.UpstreamServerID {
					if revokeSecond {
						require.NoError(t, db.Model(&second).Update("enabled", false).Error)
					}
					return fmt.Errorf("first upstream is unavailable")
				}
				return nil
			}
			m := newCascadeEventSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), time.Hour)
			defer m.Shutdown(context.Background())
			require.ErrorContains(t, m.BroadcastAlarm(context.Background(), "source-only-device", "source-call", "4 MESSAGE", []byte(cascadeAlarmMessageBody)), "first upstream is unavailable")
			first := <-tr.requests
			firstAlarm, err := manscdp.ParseAlarmNotify(first.Body())
			require.NoError(t, err)
			require.Equal(t, "34020000001320000012", firstAlarm.DeviceID)
			if revokeSecond {
				require.Empty(t, tr.requests)
				return
			}
			require.Len(t, tr.requests, 1, "one upstream failing must not prevent the next delivery")
			other := <-tr.requests
			alarm, err := manscdp.ParseAlarmNotify(other.Body())
			require.NoError(t, err)
			require.Equal(t, second.UpstreamServerID, other.To().Address.User)
			require.Equal(t, "34020000001320000022", alarm.DeviceID)
			require.NotEqual(t, first.CallID().Value(), other.CallID().Value())
		})
	}
}

func TestCascadeAlarmMessageRevalidatesBeforeEveryNotify(t *testing.T) {
	db, p, store := cascadeAlarmFixture(t, "2022")
	tr := &subscriptionTransport{requests: make(chan *sip.Request, 10), status: 200}
	tr.before = func(context.Context, *sip.Request) error {
		return db.Exec("UPDATE gb_channel SET deleted_at=CURRENT_TIMESTAMP WHERE id=20").Error
	}
	m := newCascadeEventSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), time.Hour)
	defer m.Shutdown(context.Background())
	first := addCascadeAlarmTestSession(t, m, p)
	first.key = "first"
	m.sessions[first.key] = first
	second := addCascadeAlarmTestSession(t, m, p)
	second.key = "second"
	m.sessions[second.key] = second
	delete(m.sessions, "alarm-sub")
	require.ErrorIs(t, m.BroadcastAlarm(context.Background(), "source-only-device", "source-call", "4 MESSAGE", []byte(cascadeAlarmMessageBody)), errCascadeAlarmBindingRevoked)
	require.Len(t, tr.requests, 1, "a channel revoked after the first delivery cannot be sent to another dialog")
	require.Equal(t, sip.NOTIFY, (<-tr.requests).Method)
}

func TestCascadeAlarmMessageTransportBudget(t *testing.T) {
	for _, transport := range []string{"UDP", "TCP"} {
		t.Run(transport, func(t *testing.T) {
			db, p, store := cascadeAlarmFixture(t, "2022")
			require.NoError(t, db.Model(&p).Update("transport", transport).Error)
			tr := &subscriptionTransport{requests: make(chan *sip.Request, 10), status: 200}
			m := newCascadeEventSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), time.Hour)
			defer m.Shutdown(context.Background())
			description := strings.Repeat("告警详情", 1200)
			body := []byte(strings.Replace(cascadeAlarmMessageBody, "门禁告警", description, 1))
			err := m.BroadcastAlarm(context.Background(), "source-only-device", "source-call", "4 MESSAGE", body)
			if transport == "UDP" {
				require.ErrorContains(t, err, "UDP")
				require.Empty(t, tr.requests)
			} else {
				require.NoError(t, err)
				require.Len(t, tr.requests, 1)
				alarm, err := manscdp.ParseAlarmNotify((<-tr.requests).Body())
				require.NoError(t, err)
				require.Equal(t, description, alarm.Description, "do not truncate an alarm to fit a packet")
			}
		})
	}
}

func TestCascadeAlarmMessageCancellation(t *testing.T) {
	_, _, store := cascadeAlarmFixture(t, "2022")
	tr := &subscriptionTransport{requests: make(chan *sip.Request, 10), status: 200}
	m := newCascadeEventSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), time.Hour)
	defer m.Shutdown(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, m.BroadcastAlarm(ctx, "source-only-device", "source-call", "4 MESSAGE", []byte(cascadeAlarmMessageBody)), context.Canceled)
	require.Empty(t, tr.requests)
	sent := make(chan struct{})
	tr.before = func(ctx context.Context, _ *sip.Request) error {
		close(sent)
		<-ctx.Done()
		return ctx.Err()
	}
	result := make(chan error, 1)
	go func() {
		result <- m.BroadcastAlarm(context.Background(), "source-only-device", "source-call", "4 MESSAGE", []byte(cascadeAlarmMessageBody))
	}()
	select {
	case <-sent:
	case <-time.After(2 * time.Second):
		t.Fatal("MESSAGE did not reach transport")
	}
	shutdownCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	require.NoError(t, m.Shutdown(shutdownCtx), "shutdown must cancel and join an in-flight MESSAGE")
	require.ErrorIs(t, <-result, context.Canceled)
	require.Len(t, tr.requests, 1)
	require.NoError(t, m.BroadcastAlarm(context.Background(), "source-only-device", "source-call", "4 MESSAGE", []byte(cascadeAlarmMessageBody)))
	require.Len(t, tr.requests, 1, "a closed manager cannot send another event")
}

func TestCascadeAlarmMessageFromDeviceProcessor(t *testing.T) {
	for _, subscribed := range []bool{false, true} {
		t.Run(fmt.Sprint(subscribed), func(t *testing.T) {
			db, p, store := cascadeAlarmFixture(t, "2022")
			tr := &subscriptionTransport{requests: make(chan *sip.Request, 10), status: 200}
			m := newCascadeEventSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), time.Hour)
			defer m.Shutdown(context.Background())
			if subscribed {
				addCascadeAlarmTestSession(t, m, p)
			}
			oldRuntime := cascadeRuntimeManager
			cascadeRuntimeManager = &cascadeSubscriptionRuntime{events: m}
			t.Cleanup(func() { cascadeRuntimeManager = oldRuntime })
			processor := subscribe.NewAlarmProcessor(db, time.Now)
			processor.SetRelay(cascadeAlarmNotificationBridge{})
			var device gbmodels.GbDevice
			require.NoError(t, db.First(&device, 10).Error)
			// The fixture retains the default disabled alarm-history setting: forwarding
			// remains available independently of storing the device's alarm locally.
			require.NoError(t, processor.Process(context.Background(), &device, subscribe.Notification{
				DeviceCode: device.DeviceID, CallID: "device-alarm-call", CSeq: "4 MESSAGE", Body: []byte(cascadeAlarmMessageBody),
			}))
			require.NoError(t, m.dispatchAlarmDelivery(t.Context()))
			require.Len(t, tr.requests, 1)
			req := <-tr.requests
			expectedMethod := sip.MESSAGE
			expectedSN := "7"
			if subscribed {
				expectedMethod, expectedSN = sip.NOTIFY, "90"
			}
			require.Equal(t, expectedMethod, req.Method)
			alarm, err := manscdp.ParseAlarmNotify(req.Body())
			require.NoError(t, err)
			require.Equal(t, expectedSN, alarm.SN)
			require.Equal(t, "34020000001320000012", alarm.DeviceID)
		})
	}
}

// Authorization generations are migration-owned columns, absent from AutoMigrate.
func cascadeAlarmFixture(t *testing.T, version string) (*gorm.DB, model.GbCascadePlatform, *repository.GormRepository) {
	t.Helper()
	db, platform, store := cascadeDeviceStatusFixture(t, version)
	for _, sql := range []string{
		"ALTER TABLE gb_device ADD COLUMN access_epoch BIGINT NOT NULL DEFAULT 1",
		"ALTER TABLE gb_device ADD COLUMN cleanup_completed_epoch BIGINT NOT NULL DEFAULT 1",
	} {
		require.NoError(t, db.Exec(sql).Error)
	}
	return db, platform, store
}

func TestCascadeAlarmNotifyIsPersistedAndRetransmitKeepsOriginalRoute(t *testing.T) {
	db, p, store := cascadeAlarmFixture(t, "2022")
	tr := &subscriptionTransport{requests: make(chan *sip.Request, 10), status: 500}
	m := newCascadeEventSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), time.Hour)
	defer m.Shutdown(context.Background())
	s := addCascadeAlarmTestSession(t, m, p)
	require.ErrorContains(t, m.BroadcastAlarm(t.Context(), "source-only-device", "source-call", "4 MESSAGE", []byte(cascadeAlarmMessageBody)), "500")
	var rows []model.GbCascadeAlarmDelivery
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1, "failed NOTIFY must be durable")
	require.Equal(t, model.CascadeAlarmDeliveryPending, rows[0].State)
	s.mu.Lock()
	s.reason = "timeout"
	s.mu.Unlock()
	require.NoError(t, m.BroadcastAlarm(t.Context(), "source-only-device", "source-call", "4 MESSAGE", []byte(cascadeAlarmMessageBody)))
	require.Len(t, tr.requests, 1, "retransmission after subscription cancellation must not change to MESSAGE")
	require.NoError(t, db.Model(&rows[0]).Update("next_attempt_at", time.Now().Add(-time.Second)).Error)
	require.ErrorIs(t, m.dispatchAlarmDelivery(t.Context(), rows[0].DeliveryKey), errCascadeAlarmBindingRevoked)
	require.Len(t, tr.requests, 1, "canceled dialog cannot be retried or replaced")
	require.NoError(t, db.First(&rows[0], rows[0].ID).Error)
	require.Equal(t, model.CascadeAlarmDeliveryFailed, rows[0].State)
}

func TestCascadeAlarmQueuedMessageRejectsChangedBinding(t *testing.T) {
	for _, sql := range []string{
		"UPDATE gb_device SET access_epoch=2, cleanup_completed_epoch=2 WHERE id=10",
		"UPDATE gb_cascade_platform SET config_revision=config_revision+1 WHERE id=1",
		"UPDATE gb_cascade_platform SET projection_revision=projection_revision+1 WHERE id=1",
		"UPDATE gb_channel SET owner_dept_id=1 WHERE id=20",
	} {
		t.Run(sql, func(t *testing.T) {
			db, _, store := cascadeAlarmFixture(t, "2022")
			tr := &subscriptionTransport{requests: make(chan *sip.Request, 10), status: 500}
			m := newCascadeEventSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), time.Hour)
			defer m.Shutdown(context.Background())
			require.Error(t, m.BroadcastAlarm(t.Context(), "source-only-device", "source-call", "4 MESSAGE", []byte(cascadeAlarmMessageBody)))
			var row model.GbCascadeAlarmDelivery
			require.NoError(t, db.First(&row).Error)
			require.NoError(t, db.Exec(sql).Error)
			require.NoError(t, db.Model(&row).Update("next_attempt_at", time.Now().Add(-time.Second)).Error)
			require.ErrorIs(t, m.dispatchAlarmDelivery(t.Context(), row.DeliveryKey), errCascadeAlarmBindingRevoked)
			require.Len(t, tr.requests, 1)
			require.NoError(t, db.First(&row, row.ID).Error)
			require.Equal(t, model.CascadeAlarmDeliveryFailed, row.State)
		})
	}
}

func TestCascadeAlarmNotifyRetriesOnlyUnfinishedDialogs(t *testing.T) {
	db, p, store := cascadeAlarmFixture(t, "2022")
	var attempts atomic.Int32
	tr := &subscriptionTransport{requests: make(chan *sip.Request, 10), status: 200}
	tr.before = func(_ context.Context, req *sip.Request) error {
		if req.CallID().Value() == "second-dialog" && attempts.Add(1) == 1 {
			return fmt.Errorf("temporary transport failure")
		}
		return nil
	}
	m := newCascadeEventSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), time.Hour)
	defer m.Shutdown(context.Background())
	first := addCascadeAlarmTestSession(t, m, p)
	m.mu.Lock()
	delete(m.sessions, first.key)
	first.key = "first"
	m.sessions[first.key] = first
	m.mu.Unlock()
	second := addCascadeAlarmTestSession(t, m, p)
	second.mu.Lock()
	second.dialog.CallID = "second-dialog"
	second.mu.Unlock()
	require.ErrorContains(t, m.BroadcastAlarm(t.Context(), "source-only-device", "call", "1 MESSAGE", []byte(cascadeAlarmMessageBody)), "temporary transport failure")
	require.Len(t, tr.requests, 2)
	var row model.GbCascadeAlarmDelivery
	require.NoError(t, db.First(&row).Error)
	require.Equal(t, model.CascadeAlarmDeliveryPending, row.State)
	// Renewal changes notification SN without changing the selected dialog.
	second.mu.Lock()
	second.sn = 91
	second.generation++
	second.mu.Unlock()
	require.NoError(t, db.Model(&row).Update("next_attempt_at", time.Now().Add(-time.Second)).Error)
	require.NoError(t, m.dispatchAlarmDelivery(t.Context(), row.DeliveryKey))
	require.Len(t, tr.requests, 3, "successful dialog must not be resent when another dialog retries")
	<-tr.requests
	<-tr.requests
	request := <-tr.requests
	require.Equal(t, "second-dialog", request.CallID().Value())
	alarm, err := manscdp.ParseAlarmNotify(request.Body())
	require.NoError(t, err)
	require.Equal(t, "91", alarm.SN)
	require.NoError(t, db.First(&row, row.ID).Error)
	require.Equal(t, model.CascadeAlarmDeliverySent, row.State)
}

func TestCascadeAlarmNotifyCannotResumeAnUnrestoredDialog(t *testing.T) {
	db, p, store := cascadeAlarmFixture(t, "2022")
	tr := &subscriptionTransport{requests: make(chan *sip.Request, 10), status: 500}
	m := newCascadeEventSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), time.Hour)
	addCascadeAlarmTestSession(t, m, p)
	require.Error(t, m.BroadcastAlarm(t.Context(), "source-only-device", "call", "1 MESSAGE", []byte(cascadeAlarmMessageBody)))
	require.NoError(t, m.Shutdown(t.Context()))
	var row model.GbCascadeAlarmDelivery
	require.NoError(t, db.First(&row).Error)
	require.NoError(t, db.Model(&row).Update("next_attempt_at", time.Now().Add(-time.Second)).Error)
	next := newCascadeEventSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), time.Hour)
	defer next.Shutdown(context.Background())
	require.ErrorIs(t, next.dispatchAlarmDelivery(t.Context(), row.DeliveryKey), errCascadeAlarmBindingRevoked)
	require.Len(t, tr.requests, 1, "an old NOTIFY cannot become MESSAGE after restart")
	require.NoError(t, db.First(&row, row.ID).Error)
	require.Equal(t, model.CascadeAlarmDeliveryFailed, row.State)
}

func TestCascadeAlarmEnqueueWaitsForPersistenceNotUpstreamResponse(t *testing.T) {
	db, _, store := cascadeAlarmFixture(t, "2022")
	tr := &subscriptionTransport{requests: make(chan *sip.Request, 10), status: 500}
	m := newCascadeEventSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), time.Hour)
	defer m.Shutdown(context.Background())
	require.NoError(t, m.EnqueueAlarm(t.Context(), "source-only-device", "call", "1 MESSAGE", []byte(cascadeAlarmMessageBody)))
	require.Empty(t, tr.requests, "device acceptance cannot depend on upstream response latency")
	var row model.GbCascadeAlarmDelivery
	require.NoError(t, db.First(&row).Error)
	require.Equal(t, model.CascadeAlarmDeliveryPending, row.State)
	require.ErrorContains(t, m.dispatchAlarmDelivery(t.Context(), row.DeliveryKey), "500")
	require.NoError(t, m.EnqueueAlarm(t.Context(), "source-only-device", "call", "1 MESSAGE", []byte(cascadeAlarmMessageBody)))
	var count int64
	require.NoError(t, db.Model(&row).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Migrator().DropTable(&model.GbCascadeAlarmDelivery{}))
	require.Error(t, m.EnqueueAlarm(t.Context(), "source-only-device", "new-call", "1 MESSAGE", []byte(cascadeAlarmMessageBody)))
}

func TestCascadeAlarmShutdownCancelsPendingStoreQuery(t *testing.T) {
	db, _, store := cascadeAlarmFixture(t, "2022")
	tr := &subscriptionTransport{requests: make(chan *sip.Request, 10), status: 200}
	entered, release := make(chan struct{}, 1), make(chan struct{})
	defer close(release)
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:block-platform-query", func(query *gorm.DB) {
		if query.Statement.Table != "gb_cascade_platform" {
			return
		}
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-query.Statement.Context.Done():
			query.AddError(query.Statement.Context.Err())
		case <-release:
		}
	}))
	m := newCascadeEventSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), time.Hour)
	result := make(chan error, 1)
	go func() {
		result <- m.BroadcastAlarm(context.Background(), "source-only-device", "call", "1 MESSAGE", []byte(cascadeAlarmMessageBody))
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("platform query was not reached")
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	require.NoError(t, m.Shutdown(shutdown), "shutdown must cancel database work as well as SIP sends")
	require.ErrorIs(t, <-result, context.Canceled)
	require.Empty(t, tr.requests)
}

func TestCascadeAlarmRetransmissionDoesNotMoveMessageToNewSubscription(t *testing.T) {
	db, p, store := cascadeAlarmFixture(t, "2022")
	tr := &subscriptionTransport{requests: make(chan *sip.Request, 10), status: 200}
	m := newCascadeEventSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), time.Hour)
	defer m.Shutdown(context.Background())
	require.NoError(t, m.EnqueueAlarm(t.Context(), "source-only-device", "call", "1 MESSAGE", []byte(cascadeAlarmMessageBody)))
	addCascadeAlarmTestSession(t, m, p)
	require.NoError(t, m.EnqueueAlarm(t.Context(), "source-only-device", "call", "1 MESSAGE", []byte(cascadeAlarmMessageBody)))
	var rows []model.GbCascadeAlarmDelivery
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Empty(t, rows[0].NotifyTargets)
	require.NoError(t, m.dispatchAlarmDelivery(t.Context(), rows[0].DeliveryKey))
	require.Len(t, tr.requests, 1)
	require.Equal(t, sip.MESSAGE, (<-tr.requests).Method)
}
