package gb28181

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/sipclient"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

func TestCascadeMobilePositionHandlerLeavesCatalogForCatalogManager(t *testing.T) {
	_, platform, store := cascadeDeviceStatusFixture(t, "2022")
	positionManager := newCascadeMobilePositionSubscriptions(store, nil, time.Hour)
	defer positionManager.Shutdown(context.Background())
	transport := &cascadeStatusTransport{requests: make(chan *sip.Request, 1)}
	catalogManager := newCascadeCatalogSubscriptions(store, newCascadePlatformClientFactory(transport, nil, time.Second), time.Hour)
	defer catalogManager.Shutdown(context.Background())
	req := newCatalogSubscribeRequest(t, platform, platform.LocalDeviceID)
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()

	require.False(t, positionManager.Handle(req, tx))
	require.Empty(t, tx.Result(), "Catalog SUBSCRIBE must reach the next manager without a response")
	require.True(t, catalogManager.Handle(req, tx))
	require.Equal(t, 200, tx.Result()[0].StatusCode)
	require.Equal(t, sip.NOTIFY, awaitCascadeStatusResponse(t, transport.requests).Method)
}

func TestCascadeMobilePositionSubscriptionHonorsRequestedInterval(t *testing.T) {
	db, platform, store := cascadeDeviceStatusFixture(t, "2022")
	require.NoError(t, db.AutoMigrate(&gbmodels.GbMobilePositionLatest{}))
	channelID := uint(20)
	now := time.Now()
	require.NoError(t, db.Create(&gbmodels.GbMobilePositionLatest{
		DeviceID: 10, SourceCode: "source-only-channel", ChannelID: &channelID,
		EventTime: now, ReceivedAt: now, Longitude: 116.4, Latitude: 39.9,
	}).Error)
	transport := &cascadeStatusTransport{requests: make(chan *sip.Request, 10)}
	manager := newCascadeMobilePositionSubscriptions(store, newCascadePlatformClientFactory(transport, nil, time.Second), 10*time.Millisecond)
	defer manager.Shutdown(context.Background())
	req := newCatalogSubscribeRequest(t, platform, platform.LocalDeviceID)
	req.RemoveHeader("Event")
	req.AppendHeader(sip.NewHeader("Event", "presence"))
	req.SetBody([]byte(`<Query><CmdType>MobilePosition</CmdType><SN>5</SN><DeviceID>` + platform.LocalDeviceID + `</DeviceID><Interval>1</Interval></Query>`))
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()

	require.True(t, manager.Handle(req, tx))
	require.Equal(t, 200, tx.Result()[0].StatusCode)
	first := awaitCascadeStatusResponse(t, transport.requests)
	started := time.Now()
	select {
	case next := <-transport.requests:
		t.Fatalf("position was sent before requested interval: %s", next.Body())
	case <-time.After(300 * time.Millisecond):
	}
	select {
	case next := <-transport.requests:
		require.GreaterOrEqual(t, time.Since(started), 900*time.Millisecond)
		require.Greater(t, next.CSeq().SeqNo, first.CSeq().SeqNo)
	case <-time.After(1800 * time.Millisecond):
		t.Fatal("unchanged location was not reported at the requested interval")
	}
}

func TestCascadeMobilePositionSubscriptionRejectsIntervalOverflow(t *testing.T) {
	query := manscdp.MobilePositionSubscriptionQuery{
		SN: 5, DeviceID: "device", Interval: int(^uint(0) >> 1), IntervalPresent: true,
	}
	_, err := cascadeMobilePositionInterval(query)
	require.Error(t, err)
}

func TestCascadeMobilePositionRefreshKeepsStateWhenResponseFails(t *testing.T) {
	_, platform, store := cascadeDeviceStatusFixture(t, "2022")
	manager := newCascadeMobilePositionSubscriptions(store, nil, time.Hour)
	defer manager.Shutdown(context.Background())

	req := newCatalogSubscribeRequest(t, platform, platform.LocalDeviceID)
	req.RemoveHeader("Event")
	req.AppendHeader(sip.NewHeader("Event", "presence"))
	req.To().Params.Add("tag", "local-tag")
	req.CSeq().SeqNo = 2
	req.SetBody([]byte(`<Query><CmdType>MobilePosition</CmdType><SN>9</SN><DeviceID>` + platform.LocalDeviceID + `</DeviceID><Interval>45</Interval></Query>`))

	remoteTag := sip.NewParams()
	remoteTag.Add("tag", "remote-tag")
	localTag := sip.NewParams()
	localTag.Add("tag", "local-tag")
	expires := time.Now().Add(time.Minute)
	state := &cascadeMobilePositionSubscription{
		platform: platform, target: platform.LocalDeviceID, sn: 3, interval: 15 * time.Second,
		expires: expires, remoteSeq: 1,
		dialog: sipclient.NotifyDialog{
			From: sip.FromHeader{Address: req.To().Address, Params: localTag},
			To:   sip.ToHeader{Address: req.From().Address, Params: remoteTag},
		},
	}
	tx := &cascadeCatalogAckTx{ServerTxRecorder: siptest.NewServerTxRecorder(req), err: errors.New("response transport failed")}
	defer tx.Terminate()

	require.True(t, manager.refresh(context.Background(), req, tx, state, platform, 600))
	require.Equal(t, 3, state.sn)
	require.Equal(t, 15*time.Second, state.interval)
	require.Equal(t, uint32(1), state.remoteSeq)
	require.Equal(t, expires, state.expires)
}

func TestCascadeMobilePositionSubscriptionSnapshot(t *testing.T) {
	_, platform, store := cascadeDeviceStatusFixture(t, "2022")
	m := newCascadeMobilePositionSubscriptions(store, nil, time.Hour)
	now := time.Now()
	m.sessions["position"] = &cascadeMobilePositionSubscription{
		key: "position", platform: platform, target: platform.LocalDeviceID,
		expires: now.Add(time.Minute), subscribedAt: now, renewedAt: now,
		dialog:           sipclient.NotifyDialog{CallID: "position", Event: "presence"},
		lastNotifyStatus: "success", lastNotifyCode: 200,
	}
	defer m.Shutdown(context.Background())
	status := m.Subscriptions(platform)
	require.Equal(t, "active", status.MobilePosition.State)
	require.Len(t, status.MobilePosition.Sessions, 1)
	require.Equal(t, platform.LocalDeviceID, status.MobilePosition.Sessions[0].Target)
	require.Equal(t, "presence", status.MobilePosition.Sessions[0].Event)
}

func TestCascadeMobilePositionSubscriptionReloadTerminatesUnpublishedTarget(t *testing.T) {
	db, platform, store := cascadeDeviceStatusFixture(t, "2022")
	require.NoError(t, db.AutoMigrate(&gbmodels.GbMobilePositionLatest{}))
	channelID := uint(20)
	now := time.Now()
	require.NoError(t, db.Create(&gbmodels.GbMobilePositionLatest{DeviceID: 10, SourceCode: "source-only-channel", ChannelID: &channelID, EventTime: now, ReceivedAt: now, Longitude: 116.4, Latitude: 39.9}).Error)
	transport := &cascadeStatusTransport{requests: make(chan *sip.Request, 10)}
	manager := newCascadeMobilePositionSubscriptions(store, newCascadePlatformClientFactory(transport, nil, time.Second), time.Hour)
	defer manager.Shutdown(context.Background())
	req := newCatalogSubscribeRequest(t, platform, "34020000001320000012")
	req.RemoveHeader("Event")
	req.AppendHeader(sip.NewHeader("Event", "presence"))
	req.SetBody([]byte(`<Query><CmdType>MobilePosition</CmdType><SN>7</SN><DeviceID>34020000001320000012</DeviceID></Query>`))
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
		return len(manager.Subscriptions(platform).MobilePosition.Sessions) == 0
	}, time.Second, time.Millisecond)
}

func TestCascadeMobilePositionBodiesMatchProtocolProfile(t *testing.T) {
	speed := 12.5
	positions := []cascadePublishedPosition{{publishedID: "34020000001320000012", row: gbmodels.GbMobilePositionLatest{
		EventTime: time.Date(2026, 10, 22, 3, 4, 5, 0, time.UTC), Longitude: 116.4, Latitude: 39.9, Speed: &speed,
	}}}

	bodies, err := buildCascadeMobilePositionBodies(protocol.ProfileFor(protocol.Version2016), "34020000002000000002", 81, positions)
	require.NoError(t, err)
	require.Len(t, bodies, 1)
	var flat struct {
		CmdType   string  `xml:"CmdType"`
		SN        int     `xml:"SN"`
		DeviceID  string  `xml:"DeviceID"`
		Longitude float64 `xml:"Longitude"`
		Latitude  float64 `xml:"Latitude"`
	}
	require.NoError(t, manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), bodies[0], &flat))
	require.Equal(t, manscdp.CmdMobilePosition, flat.CmdType)
	require.Equal(t, 81, flat.SN)
	require.Equal(t, "34020000001320000012", flat.DeviceID)
	require.InDelta(t, 116.4, flat.Longitude, 0.000001)

	bodies, err = buildCascadeMobilePositionBodies(protocol.ProfileFor(protocol.Version2022), "34020000002000000002", 82, positions)
	require.NoError(t, err)
	require.Len(t, bodies, 1)
	var list struct {
		CmdType    string `xml:"CmdType"`
		SN         int    `xml:"SN"`
		DeviceID   string `xml:"DeviceID"`
		SumNum     int    `xml:"SumNum"`
		DeviceList struct {
			Num   int `xml:"Num,attr"`
			Items []struct {
				DeviceID  string  `xml:"DeviceID"`
				Longitude float64 `xml:"Longitude"`
			} `xml:"Item"`
		} `xml:"DeviceList"`
	}
	require.NoError(t, manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2022), bodies[0], &list))
	require.Equal(t, 82, list.SN)
	require.Equal(t, "34020000002000000002", list.DeviceID)
	require.Equal(t, 1, list.SumNum)
	require.Equal(t, 1, list.DeviceList.Num)
	require.Equal(t, "34020000001320000012", list.DeviceList.Items[0].DeviceID)
}

func TestCascadeMobilePositionScopeUsesPublishedProjection(t *testing.T) {
	db, platform, store := cascadeDeviceStatusFixture(t, "2022")
	require.NoError(t, db.Table("gb_cascade_channel_projection").Where("platform_id = ?", platform.ID).Update("ptz_allowed", true).Error)
	projection, err := store.ProjectionSnapshot(context.Background(), platform.ID)
	require.NoError(t, err)
	require.Equal(t, map[uint64]string{20: "34020000001320000012"}, cascadePositionScope(projection, platform, "34020000001320000011"))
	require.Equal(t, map[uint64]string{20: "34020000001320000012"}, cascadePositionScope(projection, platform, "34020000001320000012"))
	allowed, err := cascadePositionTargetAllowed(context.Background(), store, platform, platform.LocalDeviceID)
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, err = cascadePositionTargetAllowed(context.Background(), store, platform, "34020000001320000999")
	require.NoError(t, err)
	require.False(t, allowed)
}
