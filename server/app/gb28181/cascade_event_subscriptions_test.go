package gb28181

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/sipclient"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

func TestCascadeSubscriptionEventDispatchAndScope(t *testing.T) {
	db, platform, store := cascadeAlarmFixture(t, "2022")
	manager := newCascadeEventSubscriptions(store, nil, time.Hour)
	defer manager.Shutdown(context.Background())
	platform.PublishPlatform = false
	allowed, err := manager.targetAllowed(context.Background(), platform, cascadeEventAlarm, platform.LocalDeviceID)
	require.NoError(t, err)
	require.False(t, allowed, "platform-level event subscriptions require platform publication")
	platform.PublishPlatform = true

	req := newCatalogSubscribeRequest(t, platform, platform.LocalDeviceID)
	req.RemoveHeader("Event")
	req.AppendHeader(sip.NewHeader("Event", "presence"))
	req.SetBody([]byte(`<Query><CmdType>Alarm</CmdType><SN>4</SN><DeviceID>34020000001320000012</DeviceID></Query>`))
	kind, _, ok := cascadeSubscriptionEventKind(req)
	require.True(t, ok)
	require.Equal(t, cascadeEventAlarm, kind)

	req.SetBody([]byte(`<Query><CmdType>MobilePosition</CmdType><SN>5</SN><DeviceID>34020000001320000012</DeviceID></Query>`))
	_, _, ok = cascadeSubscriptionEventKind(req)
	require.False(t, ok, "MobilePosition stays with its own subscription manager")

	req.RemoveHeader("Event")
	req.AppendHeader(sip.NewHeader("Event", "PTZPosition"))
	req.SetBody([]byte(`<Query><CmdType>PTZPosition</CmdType><SN>6</SN><DeviceID>34020000001320000012</DeviceID></Query>`))
	kind, _, ok = cascadeSubscriptionEventKind(req)
	require.True(t, ok)
	require.Equal(t, cascadeEventPTZ, kind)

	allowed, err = manager.targetAllowed(context.Background(), platform, cascadeEventPTZ, "34020000001320000012")
	require.NoError(t, err)
	require.False(t, allowed, "PTZ must be shared explicitly")
	require.NoError(t, db.Table("gb_cascade_channel_projection").Where("platform_id = ?", platform.ID).Update("ptz_allowed", true).Error)
	allowed, err = manager.targetAllowed(context.Background(), platform, cascadeEventPTZ, "34020000001320000011")
	require.NoError(t, err)
	require.True(t, allowed, "device-level PTZ scope requires at least one PTZ-enabled child")
}

func TestCascadeAlarmPresenceRefreshDispatchesByDialog(t *testing.T) {
	_, platform, store := cascadeAlarmFixture(t, "2022")
	manager := newCascadeEventSubscriptions(store, nil, time.Hour)
	defer manager.Shutdown(context.Background())
	req := newCatalogSubscribeRequest(t, platform, platform.LocalDeviceID)
	req.RemoveHeader("Event")
	req.AppendHeader(sip.NewHeader("Event", "presence"))
	req.SetBody(nil)
	manager.sessions["alarm"] = &cascadeEventSubscription{
		kind: cascadeEventAlarm,
		dialog: sipclient.NotifyDialog{
			CallID: req.CallID().Value(), Event: "presence",
			To: sip.ToHeader{Address: req.From().Address, Params: req.From().Params},
		},
	}

	kind, event, ok := manager.subscriptionEventKind(req)
	require.True(t, ok)
	require.Equal(t, cascadeEventAlarm, kind)
	require.Equal(t, "presence", event)

	req.RemoveHeader("Call-ID")
	req.AppendHeader(sip.NewHeader("Call-ID", "mobile-position-refresh"))
	_, _, ok = manager.subscriptionEventKind(req)
	require.False(t, ok, "presence refresh without an alarm dialog must fall through to MobilePosition")
}

func TestCascadeSubscriptionQueryRequiresMatchingEventPackage(t *testing.T) {
	for _, tc := range []struct {
		event string
		cmd   string
		want  bool
	}{
		{event: "PTZPosition", cmd: manscdp.CmdPTZPosition, want: true},
		{event: "PTZPosition", cmd: manscdp.CmdPTZPrecisePosition},
		{event: "PTZPrecisePosition", cmd: manscdp.CmdPTZPrecisePosition, want: true},
		{event: "PTZPrecisePosition", cmd: manscdp.CmdPTZPosition},
		{event: "Alarm", cmd: manscdp.CmdAlarm, want: true},
		{event: "presence", cmd: manscdp.CmdAlarm, want: true},
	} {
		t.Run(tc.event+"/"+tc.cmd, func(t *testing.T) {
			require.Equal(t, tc.want, cascadeSubscriptionQueryMatches(cascadeEventPTZ, tc.cmd, tc.event) || cascadeSubscriptionQueryMatches(cascadeEventAlarm, tc.cmd, tc.event))
		})
	}
}

func TestCascadeEventRefreshRequiresSameEventPackageAndUpdatesSN(t *testing.T) {
	_, platform, store := cascadeAlarmFixture(t, "2022")
	manager := newCascadeEventSubscriptions(store, nil, time.Hour)
	defer manager.Shutdown(context.Background())
	req := newCatalogSubscribeRequest(t, platform, platform.LocalDeviceID)
	req.RemoveHeader("Event")
	req.AppendHeader(sip.NewHeader("Event", "PTZPosition"))
	req.To().Params.Add("tag", "local-tag")
	req.CSeq().SeqNo = 2
	req.SetBody([]byte(`<Query><CmdType>PTZPosition</CmdType><SN>99</SN><DeviceID>` + platform.LocalDeviceID + `</DeviceID></Query>`))

	remoteTag := sip.NewParams()
	remoteTag.Add("tag", "remote-tag")
	localTag := sip.NewParams()
	localTag.Add("tag", "local-tag")
	state := &cascadeEventSubscription{
		kind: cascadeEventPTZ, platform: platform, target: platform.LocalDeviceID, sn: 3,
		expires: time.Now().Add(time.Minute), remoteSeq: 1,
		dialog: sipclient.NotifyDialog{
			From:  sip.FromHeader{Address: req.To().Address, Params: localTag},
			To:    sip.ToHeader{Address: req.From().Address, Params: remoteTag},
			Event: "PTZPosition",
		},
	}
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()
	require.True(t, manager.refresh(context.Background(), req, tx, state, platform, 600, "PTZPosition"))
	require.Equal(t, 200, tx.Result()[0].StatusCode)
	require.Equal(t, 99, state.sn)

	changed := req.Clone()
	changed.RemoveHeader("Event")
	changed.AppendHeader(sip.NewHeader("Event", "PTZPrecisePosition"))
	changed.To().Params.Add("tag", "local-tag")
	changed.CSeq().SeqNo = 3
	changed.SetBody([]byte(`<Query><CmdType>PTZPrecisePosition</CmdType><SN>100</SN><DeviceID>` + platform.LocalDeviceID + `</DeviceID></Query>`))
	changedTx := siptest.NewServerTxRecorder(changed)
	defer changedTx.Terminate()
	require.True(t, manager.refresh(context.Background(), changed, changedTx, state, platform, 600, "PTZPrecisePosition"))
	require.Equal(t, 481, changedTx.Result()[0].StatusCode)
	require.Equal(t, "PTZPosition", state.dialog.Event)
	require.Equal(t, 99, state.sn)
}

func TestCascadeEventBodiesMapTargetSequenceAndProfile(t *testing.T) {
	pan, tilt := 120.0, -15.0
	ptzNotify := &manscdp.PTZPrecisePositionNotify{CmdType: manscdp.CmdPTZPrecisePosition, SN: 7, DeviceID: "source-channel", Pan: &pan, Tilt: &tilt}
	body, err := buildCascadeEventBody(protocol.ProfileFor(protocol.Version2022), cascadeEventPTZ, "published-channel", 81, ptzNotify)
	require.NoError(t, err)
	var ptz struct {
		CmdType  string  `xml:"CmdType"`
		SN       int     `xml:"SN"`
		DeviceID string  `xml:"DeviceID"`
		Pan      float64 `xml:"Pan"`
	}
	require.NoError(t, manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2022), body, &ptz))
	require.Equal(t, manscdp.CmdPTZPosition, ptz.CmdType)
	require.Equal(t, 81, ptz.SN)
	require.Equal(t, "published-channel", ptz.DeviceID)
	require.Equal(t, 120.0, ptz.Pan)

	eventType := 3
	alarm := &manscdp.AlarmNotify{CmdType: manscdp.CmdAlarm, SN: "8", DeviceID: "source-channel", Priority: 2, Method: 5, Description: "motion", Info: struct {
		AlarmType      *int `xml:"AlarmType"`
		AlarmTypeParam struct {
			EventType *int `xml:"EventType"`
		} `xml:"AlarmTypeParam"`
	}{AlarmType: &eventType}}
	alarm.Info.AlarmTypeParam.EventType = &eventType
	body, err = buildCascadeEventBody(protocol.ProfileFor(protocol.Version2022), cascadeEventAlarm, "published-channel", 82, alarm)
	require.NoError(t, err)
	decoded, err := manscdp.ParseAlarmNotify(body)
	require.NoError(t, err)
	require.Equal(t, "82", decoded.SN)
	require.Equal(t, "published-channel", decoded.DeviceID)
	require.NotNil(t, decoded.Info.AlarmType)
	require.Equal(t, 3, *decoded.Info.AlarmType)
	require.Equal(t, "EventType=3", decoded.AlarmTypeParam)
}

func TestCascadeEventBroadcastsOnlyToActiveAuthorizedDialog(t *testing.T) {
	db, platform, store := cascadeAlarmFixture(t, "2022")
	require.NoError(t, db.Table("gb_cascade_channel_projection").Where("platform_id = ?", platform.ID).Update("ptz_allowed", true).Error)
	transport := &cascadeStatusTransport{requests: make(chan *sip.Request, 10)}
	manager := newCascadeEventSubscriptions(store, newCascadePlatformClientFactory(transport, nil, time.Second), time.Hour)
	defer manager.Shutdown(context.Background())
	client, err := manager.clients.NewClient(platform)
	require.NoError(t, err)
	transactions := client.(*cascadePlatformClient).transactions
	from := sip.FromHeader{Address: sip.Uri{Scheme: "sip", User: platform.LocalDeviceID, Host: platform.LocalDomain}, Params: sip.NewParams()}
	from.Params.Add("tag", "local-tag")
	to := sip.ToHeader{Address: sip.Uri{Scheme: "sip", User: platform.UpstreamServerID, Host: platform.UpstreamDomain}, Params: sip.NewParams()}
	to.Params.Add("tag", "remote-tag")
	manager.sessions["ptz"] = &cascadeEventSubscription{
		key: "ptz", kind: cascadeEventPTZ, platform: platform, client: transactions,
		dialog: sipclient.NotifyDialog{CallID: "ptz-sub", From: from, To: to, RemoteTarget: sip.Uri{Scheme: "sip", User: platform.UpstreamServerID, Host: platform.Host, Port: platform.Port}, Event: "PTZPosition"},
		target: "34020000001320000011", sn: 90, expires: time.Now().Add(time.Minute),
	}
	manager.sessions["stale"] = &cascadeEventSubscription{key: "stale", kind: cascadeEventPTZ, platform: platform, target: "34020000001320000012", expires: time.Now().Add(-time.Second)}

	body := []byte(`<Notify><CmdType>PTZPosition</CmdType><SN>7</SN><DeviceID>source-only-channel</DeviceID><Pan>12</Pan></Notify>`)
	require.NoError(t, manager.BroadcastPTZ(context.Background(), "source-only-device", "device-call", "4 NOTIFY", body))
	request := awaitCascadeStatusResponse(t, transport.requests)
	require.Equal(t, sip.NOTIFY, request.Method)
	require.Equal(t, "ptz-sub", request.CallID().Value())
	decoded, err := manscdp.ParsePTZPrecisePositionNotify(request.Body())
	require.NoError(t, err)
	require.Equal(t, 90, decoded.SN)
	require.Equal(t, "34020000001320000012", decoded.DeviceID)
	require.Empty(t, transport.requests, "expired session receives no notification")
	require.Equal(t, "success", manager.Subscriptions(platform).PTZPosition.Sessions[0].LastNotifyStatus)
}

func TestCascadeEventSubscriptionLifecycle(t *testing.T) {
	for _, event := range []string{"PTZPosition", "presence"} {
		t.Run(event, func(t *testing.T) {
			db, platform, store := cascadeAlarmFixture(t, "2022")
			require.NoError(t, db.Table("gb_cascade_channel_projection").Where("platform_id = ?", platform.ID).Update("ptz_allowed", true).Error)
			transport := &cascadeStatusTransport{requests: make(chan *sip.Request, 10)}
			manager := newCascadeEventSubscriptions(store, newCascadePlatformClientFactory(transport, nil, time.Second), time.Hour)
			defer manager.Shutdown(context.Background())
			cmd := manscdp.CmdPTZPosition
			if event == "presence" {
				cmd = manscdp.CmdAlarm
			}
			req := newCatalogSubscribeRequest(t, platform, platform.LocalDeviceID)
			req.RemoveHeader("Event")
			req.AppendHeader(sip.NewHeader("Event", event))
			req.SetBody([]byte(fmt.Sprintf(`<Query><CmdType>%s</CmdType><SN>7</SN><DeviceID>%s</DeviceID></Query>`, cmd, platform.LocalDeviceID)))
			tx := siptest.NewServerTxRecorder(req)
			defer tx.Terminate()
			require.True(t, manager.Handle(req, tx))
			require.Equal(t, 200, tx.Result()[0].StatusCode)
			initial := awaitCascadeStatusResponse(t, transport.requests)
			require.Equal(t, event, initial.GetHeader("Event").Value())
			require.Contains(t, initial.GetHeader("Subscription-State").Value(), "active")
			status := manager.Subscriptions(platform)
			if cmd == manscdp.CmdAlarm {
				require.Equal(t, "active", status.Alarm.State)
			} else {
				require.Equal(t, "active", status.PTZPosition.State)
			}

			broadcast := func(sn int) {
				t.Helper()
				if cmd == manscdp.CmdAlarm {
					require.NoError(t, manager.BroadcastAlarm(context.Background(), "source-only-device", "alarm-call", fmt.Sprintf("%d NOTIFY", sn), []byte(`<Notify><CmdType>Alarm</CmdType><SN>1</SN><DeviceID>source-only-channel</DeviceID><AlarmPriority>1</AlarmPriority><AlarmMethod>2</AlarmMethod><AlarmTime>2026-10-22T10:00:00</AlarmTime></Notify>`)))
				} else {
					require.NoError(t, manager.BroadcastPTZ(context.Background(), "source-only-device", "ptz-call", "1 NOTIFY", []byte(`<Notify><CmdType>PTZPosition</CmdType><SN>1</SN><DeviceID>source-only-channel</DeviceID><Pan>12</Pan></Notify>`)))
				}
				notify := awaitCascadeStatusResponse(t, transport.requests)
				query, err := manscdp.ParseSubscriptionQuery(notify.Body())
				require.NoError(t, err)
				require.Equal(t, sn, query.SN)
				require.Equal(t, "34020000001320000012", query.DeviceID)
			}
			broadcast(7)
			refresh := req.Clone()
			tag, _ := tx.Result()[0].To().Params.Get("tag")
			refresh.To().Params.Add("tag", tag)
			refresh.CSeq().SeqNo = 2
			refresh.SetBody([]byte(fmt.Sprintf(`<Query><CmdType>%s</CmdType><SN>8</SN><DeviceID>%s</DeviceID></Query>`, cmd, platform.LocalDeviceID)))
			refreshTx := siptest.NewServerTxRecorder(refresh)
			defer refreshTx.Terminate()
			require.True(t, manager.Handle(refresh, refreshTx))
			require.Equal(t, 200, refreshTx.Result()[0].StatusCode)
			renewed := awaitCascadeStatusResponse(t, transport.requests)
			require.Contains(t, renewed.GetHeader("Subscription-State").Value(), "active")
			require.Greater(t, renewed.CSeq().SeqNo, initial.CSeq().SeqNo)
			broadcast(8)
			cancel := refresh.Clone()
			cancel.CSeq().SeqNo = 3
			cancel.RemoveHeader("Expires")
			cancel.AppendHeader(sip.NewHeader("Expires", "0"))
			cancel.SetBody(nil)
			cancelTx := siptest.NewServerTxRecorder(cancel)
			defer cancelTx.Terminate()
			require.True(t, manager.Handle(cancel, cancelTx))
			require.Equal(t, 200, cancelTx.Result()[0].StatusCode)
			terminated := awaitCascadeStatusResponse(t, transport.requests)
			require.Contains(t, terminated.GetHeader("Subscription-State").Value(), "terminated")
			require.Greater(t, terminated.CSeq().SeqNo, uint32(1))
			require.Eventually(t, func() bool {
				status := manager.Subscriptions(platform)
				return len(status.PTZPosition.Sessions) == 0 && len(status.Alarm.Sessions) == 0
			}, time.Second, time.Millisecond)
		})
	}
}

func TestCascadeEventSubscriptionReloadTerminatesUnpublishedTarget(t *testing.T) {
	for _, event := range []string{"PTZPosition", "Alarm"} {
		t.Run(event, func(t *testing.T) {
			db, platform, store := cascadeAlarmFixture(t, "2022")
			require.NoError(t, db.Table("gb_cascade_channel_projection").Where("platform_id = ?", platform.ID).Update("ptz_allowed", true).Error)
			transport := &cascadeStatusTransport{requests: make(chan *sip.Request, 10)}
			manager := newCascadeEventSubscriptions(store, newCascadePlatformClientFactory(transport, nil, time.Second), time.Hour)
			defer manager.Shutdown(context.Background())
			query := manscdp.CmdAlarm
			if event == "PTZPosition" {
				query = manscdp.CmdPTZPosition
			}
			req := newCatalogSubscribeRequest(t, platform, "34020000001320000012")
			req.RemoveHeader("Event")
			req.AppendHeader(sip.NewHeader("Event", event))
			req.SetBody([]byte(fmt.Sprintf(`<Query><CmdType>%s</CmdType><SN>7</SN><DeviceID>34020000001320000012</DeviceID></Query>`, query)))
			tx := siptest.NewServerTxRecorder(req)
			defer tx.Terminate()
			require.True(t, manager.Handle(req, tx))
			require.Equal(t, 200, tx.Result()[0].StatusCode)
			active := awaitCascadeStatusResponse(t, transport.requests)
			require.Contains(t, active.GetHeader("Subscription-State").Value(), "active")

			require.NoError(t, db.Table("gb_cascade_channel_projection").Where("platform_id = ?", platform.ID).Update("active", false).Error)
			require.NoError(t, manager.Reload(context.Background()))
			terminated := awaitCascadeStatusResponse(t, transport.requests)
			require.Contains(t, terminated.GetHeader("Subscription-State").Value(), "terminated")
			require.Eventually(t, func() bool {
				status := manager.Subscriptions(platform)
				return len(status.PTZPosition.Sessions) == 0 && len(status.Alarm.Sessions) == 0
			}, time.Second, time.Millisecond)
		})
	}
}

var _ = sipclient.NotifyDialog{}

func TestCascadeEventBroadcastReportsSIPFailureAndRetiresLostDialog(t *testing.T) {
	for _, status := range []int{500, 481} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			db, platform, store := cascadeAlarmFixture(t, "2022")
			require.NoError(t, db.Table("gb_cascade_channel_projection").Where("platform_id = ?", platform.ID).Update("ptz_allowed", true).Error)
			transport := &subscriptionTransport{requests: make(chan *sip.Request, 10), status: status}
			manager := newCascadeEventSubscriptions(store, newCascadePlatformClientFactory(transport, nil, time.Second), time.Hour)
			defer manager.Shutdown(context.Background())
			client, err := manager.clients.NewClient(platform)
			require.NoError(t, err)
			from := sip.FromHeader{Address: sip.Uri{Scheme: "sip", User: platform.LocalDeviceID, Host: platform.LocalDomain}, Params: sip.NewParams()}
			from.Params.Add("tag", "local")
			to := sip.ToHeader{Address: sip.Uri{Scheme: "sip", User: platform.UpstreamServerID, Host: platform.UpstreamDomain}, Params: sip.NewParams()}
			to.Params.Add("tag", "remote")
			session := &cascadeEventSubscription{key: "failing", kind: cascadeEventPTZ, platform: platform,
				client: client.(*cascadePlatformClient).transactions,
				dialog: sipclient.NotifyDialog{CallID: "ptz-sub", From: from, To: to, RemoteTarget: sip.Uri{Scheme: "sip", User: platform.UpstreamServerID, Host: platform.Host, Port: platform.Port}, Event: "PTZPosition"},
				target: platform.LocalDeviceID, sn: 90, expires: time.Now().Add(time.Minute)}
			manager.sessions[session.key] = session
			body := []byte(`<Notify><CmdType>PTZPosition</CmdType><SN>7</SN><DeviceID>source-only-channel</DeviceID><Pan>12</Pan></Notify>`)
			err = manager.BroadcastPTZ(context.Background(), "source-only-device", "device-call", "4 NOTIFY", body)
			require.Error(t, err, "a SIP error response must never report success")
			require.Contains(t, err.Error(), fmt.Sprint(status))
			view := manager.Subscriptions(platform).PTZPosition
			if status == 481 {
				require.Equal(t, "inactive", view.State, "lost dialog must not remain subscribed")
			} else {
				require.Equal(t, "active", view.State, "temporary server failure keeps a bounded retry opportunity")
				require.Equal(t, status, view.Sessions[0].LastNotifyCode)
				require.Equal(t, "failed", view.Sessions[0].LastNotifyStatus)
			}
		})
	}
}

func TestCascadeEventHiddenChannelCannotEscalateToPublishedDevice(t *testing.T) {
	_, platform, store := cascadeAlarmFixture(t, "2022")
	manager := newCascadeEventSubscriptions(store, nil, time.Hour)
	defer manager.Shutdown(context.Background())
	for _, kind := range []cascadeEventSubscriptionKind{cascadeEventPTZ, cascadeEventAlarm} {
		_, _, allowed, err := manager.resolveSource(context.Background(), platform, kind, "source-only-device", "unpublished-channel")
		require.NoError(t, err)
		require.False(t, allowed, "an unknown channel is not a device-level event")
	}
}

func TestCascadeEventInitialStatusRetriesBoundedly(t *testing.T) {
	_, platform, store := cascadeAlarmFixture(t, "2022")
	transport := &subscriptionTransport{requests: make(chan *sip.Request, 10), status: 500}
	manager := newCascadeEventSubscriptions(store, newCascadePlatformClientFactory(transport, nil, time.Second), 5*time.Millisecond)
	defer manager.Shutdown(context.Background())
	req := newCatalogSubscribeRequest(t, platform, platform.LocalDeviceID)
	req.RemoveHeader("Event")
	req.AppendHeader(sip.NewHeader("Event", "Alarm"))
	req.SetBody([]byte(`<Query><CmdType>Alarm</CmdType><SN>7</SN><DeviceID>` + platform.LocalDeviceID + `</DeviceID></Query>`))
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()
	require.True(t, manager.Handle(req, tx))
	require.Equal(t, 200, tx.Result()[0].StatusCode)
	require.Eventually(t, func() bool {
		manager.mu.Lock()
		defer manager.mu.Unlock()
		return len(manager.sessions) == 0
	}, time.Second, time.Millisecond)
	active := 0
	for len(transport.requests) > 0 {
		request := <-transport.requests
		if strings.HasPrefix(request.GetHeader("Subscription-State").Value(), "active") {
			active++
		}
	}
	require.Equal(t, 3, active, "temporary failures should be retried with a bound")
}
