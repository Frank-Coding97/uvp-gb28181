package gb28181

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/sipclient"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
)

func TestCascadeCatalogSubscriptionLifecycle(t *testing.T) {
	for _, version := range []string{"2016", "2022"} {
		t.Run(version, func(t *testing.T) {
			db, p, store := cascadeDeviceStatusFixture(t, version)
			require.NoError(t, db.Table("gb_channel").Where("id = ?", 20).Update("status", 0).Error)
			transport := &cascadeStatusTransport{requests: make(chan *sip.Request, 20)}
			manager := newCascadeCatalogSubscriptions(store, newCascadePlatformClientFactory(transport, nil, time.Second), 10*time.Millisecond)
			t.Cleanup(func() { require.NoError(t, manager.Shutdown(context.Background())) })
			req := newCascadeStatusRequest(p, p.LocalDeviceID, p.LocalDeviceID, []byte("<Query><CmdType>Catalog</CmdType><SN>71</SN><DeviceID>"+p.LocalDeviceID+"</DeviceID></Query>"))
			req.Method = sip.SUBSCRIBE
			req.CSeq().MethodName = sip.SUBSCRIBE
			req.From().Params = sip.NewParams()
			req.From().Params.Add("tag", "remote")
			req.To().Params = sip.NewParams()
			req.AppendHeader(sip.NewHeader("Event", "Catalog;id=7"))
			req.AppendHeader(sip.NewHeader("Expires", "600"))
			req.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: p.UpstreamServerID, Host: p.Host, Port: p.Port}})
			tx := siptest.NewServerTxRecorder(req)
			defer tx.Terminate()
			require.True(t, manager.Handle(req, tx))
			require.Equal(t, 200, tx.Result()[0].StatusCode)
			notify := awaitCascadeStatusResponse(t, transport.requests)
			require.Equal(t, sip.NOTIFY, notify.Method)
			require.Equal(t, req.CallID().Value(), notify.CallID().Value())
			require.Equal(t, "Catalog;id=7", notify.GetHeader("Event").Value())
			parsed, err := manscdp.ParseCatalogNotify(notify.Body())
			require.NoError(t, err)
			require.Equal(t, 71, parsed.SN)
			require.Len(t, parsed.DeviceList.Items, 1)
			require.Equal(t, "OFF", parsed.DeviceList.Items[0].Event)
			require.NotContains(t, string(notify.Body()), "source-only")
			require.NoError(t, db.Table("gb_channel").Where("id = ?", 20).Update("status", 1).Error)
			online := awaitCascadeStatusResponse(t, transport.requests)
			parsed, err = manscdp.ParseCatalogNotify(online.Body())
			require.NoError(t, err)
			require.Equal(t, "ON", parsed.DeviceList.Items[0].Event)
			require.Greater(t, online.CSeq().SeqNo, notify.CSeq().SeqNo)
			localTag, _ := tx.Result()[0].To().Params.Get("tag")
			refresh := req.Clone()
			refresh.To().Params.Add("tag", localTag)
			refresh.CSeq().SeqNo = 2
			refreshTx := siptest.NewServerTxRecorder(refresh)
			defer refreshTx.Terminate()
			require.True(t, manager.Handle(refresh, refreshTx))
			require.Equal(t, 200, refreshTx.Result()[0].StatusCode)
			active := awaitCascadeStatusResponse(t, transport.requests)
			require.Contains(t, active.GetHeader("Subscription-State").Value(), "active")
			cancel := refresh.Clone()
			cancel.CSeq().SeqNo = 3
			cancel.RemoveHeader("Expires")
			cancel.AppendHeader(sip.NewHeader("Expires", "0"))
			cancelTx := siptest.NewServerTxRecorder(cancel)
			defer cancelTx.Terminate()
			require.True(t, manager.Handle(cancel, cancelTx))
			require.Equal(t, 200, cancelTx.Result()[0].StatusCode)
			terminated := awaitCascadeStatusResponse(t, transport.requests)
			require.Equal(t, "terminated;reason=timeout", terminated.GetHeader("Subscription-State").Value())
			require.Empty(t, terminated.Body())
			require.Eventually(t, func() bool { return manager.count() == 0 }, time.Second, time.Millisecond)
		})
	}
}

func newCatalogSubscribeRequest(t *testing.T, p model.GbCascadePlatform, target string) *sip.Request {
	t.Helper()
	body, err := manscdp.BuildCatalogQueryWithProfile(cascadeWireProfile(p), target, 81)
	require.NoError(t, err)
	r := newCascadeStatusRequest(p, p.LocalDeviceID, p.LocalDeviceID, body)
	r.Method = sip.SUBSCRIBE
	r.CSeq().MethodName = sip.SUBSCRIBE
	r.From().Params = sip.NewParams()
	r.From().Params.Add("tag", "remote-tag")
	r.To().Params = sip.NewParams()
	r.AppendHeader(sip.NewHeader("Event", "Catalog;id=1"))
	r.AppendHeader(sip.NewHeader("Expires", "600"))
	r.AppendHeader(&sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: p.UpstreamServerID, Host: p.Host, Port: p.Port}})
	return r
}

func subscribeCatalog(t *testing.T, m *cascadeCatalogSubscriptions, r *sip.Request) *sip.Response {
	t.Helper()
	tx := siptest.NewServerTxRecorder(r)
	defer tx.Terminate()
	require.True(t, m.Handle(r, tx))
	require.Len(t, tx.Result(), 1)
	return tx.Result()[0]
}

func TestCascadeCatalogSubscriptionRejectsInvalidRequests(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*sip.Request)
		code   int
	}{
		{"wrong source", func(r *sip.Request) { r.SetSource("192.0.2.99:5060") }, 403},
		{"wrong From", func(r *sip.Request) { r.From().Address.User = "34020000002000000999" }, 403},
		{"wrong To", func(r *sip.Request) { r.To().Address.User = "34020000001320000999" }, 403},
		{"wrong URI", func(r *sip.Request) { r.Recipient.User = "34020000001320000999" }, 403},
		{"missing tag", func(r *sip.Request) { r.From().Params = sip.NewParams() }, 400},
		{"unsupported Event", func(r *sip.Request) { r.RemoveHeader("Event"); r.AppendHeader(sip.NewHeader("Event", "Alarm;id=1")) }, 489},
		{"bad Event id", func(r *sip.Request) {
			r.RemoveHeader("Event")
			r.AppendHeader(sip.NewHeader("Event", "Catalog;id=bad"))
		}, 489},
		{"negative expiry", func(r *sip.Request) { r.RemoveHeader("Expires"); r.AppendHeader(sip.NewHeader("Expires", "-1")) }, 400},
		{"ambiguous expiry", func(r *sip.Request) { r.AppendHeader(sip.NewHeader("Expire", "600")) }, 400},
		{"unknown dialog", func(r *sip.Request) { r.To().Params.Add("tag", "unknown") }, 481},
		{"initial cancel", func(r *sip.Request) { r.RemoveHeader("Expires"); r.AppendHeader(sip.NewHeader("Expires", "0")) }, 481},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			_, p, store := cascadeDeviceStatusFixture(t, "2022")
			transport := &cascadeStatusTransport{requests: make(chan *sip.Request, 5)}
			m := newCascadeCatalogSubscriptions(store, newCascadePlatformClientFactory(transport, nil, time.Second), time.Hour)
			defer m.Shutdown(context.Background())
			req := newCatalogSubscribeRequest(t, p, p.LocalDeviceID)
			scenario.change(req)
			require.Equal(t, scenario.code, subscribeCatalog(t, m, req).StatusCode)
			require.Zero(t, m.count())
			require.Empty(t, transport.requests)
		})
	}
}

type subscriptionTransport struct {
	requests chan *sip.Request
	status   int
	before   func(context.Context, *sip.Request) error
}

func (f *subscriptionTransport) Do(ctx context.Context, req *sip.Request) (*sip.Response, error) {
	f.requests <- req.Clone()
	if f.before != nil {
		if err := f.before(ctx, req); err != nil {
			return nil, err
		}
	}
	return sip.NewResponseFromRequest(req, f.status, "result", nil), nil
}
func (f *subscriptionTransport) DoDigestAuth(ctx context.Context, r *sip.Request, _ *sip.Response, _ sipclient.Credentials) (*sip.Response, error) {
	return f.Do(ctx, r)
}

func TestCascadeCatalogSubscriptionRetriesBoundedlyAndCleansUp(t *testing.T) {
	for _, status := range []int{500, 481} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			_, p, store := cascadeDeviceStatusFixture(t, "2022")
			tr := &subscriptionTransport{requests: make(chan *sip.Request, 10), status: status}
			m := newCascadeCatalogSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), 5*time.Millisecond)
			defer m.Shutdown(context.Background())
			require.Equal(t, 200, subscribeCatalog(t, m, newCatalogSubscribeRequest(t, p, p.LocalDeviceID)).StatusCode)
			require.Eventually(t, func() bool { return m.count() == 0 }, time.Second, time.Millisecond)
			expected := 3
			if status == 481 {
				expected = 1
			}
			require.Len(t, tr.requests, expected)
		})
	}
}

func TestCascadeCatalogSubscriptionRemovesSharedChannelWithoutOldMetadata(t *testing.T) {
	db, p, store := cascadeDeviceStatusFixture(t, "2022")
	tr := &cascadeStatusTransport{requests: make(chan *sip.Request, 10)}
	m := newCascadeCatalogSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), 5*time.Millisecond)
	defer m.Shutdown(context.Background())
	target := "34020000001320000012"
	require.Equal(t, 200, subscribeCatalog(t, m, newCatalogSubscribeRequest(t, p, target)).StatusCode)
	awaitCascadeStatusResponse(t, tr.requests)
	require.NoError(t, db.Exec("UPDATE gb_cascade_channel_projection SET active=0 WHERE platform_id=?", p.ID).Error)
	require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET projection_revision=projection_revision+1 WHERE id=?", p.ID).Error)
	notification := awaitCascadeStatusResponse(t, tr.requests)
	parsed, err := manscdp.ParseCatalogNotify(notification.Body())
	require.NoError(t, err)
	require.Len(t, parsed.DeviceList.Items, 1)
	require.Equal(t, "DEL", parsed.DeviceList.Items[0].Event)
	require.Equal(t, target, parsed.DeviceList.Items[0].DeviceID)
	require.NotContains(t, string(notification.Body()), "<Name>")
	terminated := awaitCascadeStatusResponse(t, tr.requests)
	require.Equal(t, "terminated;reason=noresource", terminated.GetHeader("Subscription-State").Value())
}

func TestCascadeCatalogSubscriptionRejectsOldOrChangedDialog(t *testing.T) {
	_, p, store := cascadeDeviceStatusFixture(t, "2022")
	tr := &cascadeStatusTransport{requests: make(chan *sip.Request, 10)}
	m := newCascadeCatalogSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), time.Hour)
	defer m.Shutdown(context.Background())
	req := newCatalogSubscribeRequest(t, p, p.LocalDeviceID)
	response := subscribeCatalog(t, m, req)
	require.Equal(t, 200, response.StatusCode)
	awaitCascadeStatusResponse(t, tr.requests)
	tag, _ := response.To().Params.Get("tag")
	for _, scenario := range []struct {
		name   string
		change func(*sip.Request)
		code   int
	}{
		{"old CSeq", func(r *sip.Request) { r.CSeq().SeqNo = 1 }, 500},
		{"wrong local tag", func(r *sip.Request) { r.To().Params.Add("tag", "bad-tag") }, 481},
		{"wrong Event id", func(r *sip.Request) { r.RemoveHeader("Event"); r.AppendHeader(sip.NewHeader("Event", "Catalog;id=2")) }, 481},
		{"changed target", func(r *sip.Request) {
			r.SetBody([]byte("<Query><CmdType>Catalog</CmdType><SN>81</SN><DeviceID>34020000001320000012</DeviceID></Query>"))
		}, 403},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			r := req.Clone()
			r.To().Params.Add("tag", tag)
			r.CSeq().SeqNo = 2
			scenario.change(r)
			require.Equal(t, scenario.code, subscribeCatalog(t, m, r).StatusCode)
		})
	}
	require.Empty(t, tr.requests)
}

func TestCascadeCatalogSubscriptionShutdownCancelsInflight(t *testing.T) {
	_, p, store := cascadeDeviceStatusFixture(t, "2022")
	tr := &subscriptionTransport{requests: make(chan *sip.Request, 2), status: 200, before: func(ctx context.Context, _ *sip.Request) error { <-ctx.Done(); return ctx.Err() }}
	m := newCascadeCatalogSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Hour), time.Hour)
	require.Equal(t, 200, subscribeCatalog(t, m, newCatalogSubscribeRequest(t, p, p.LocalDeviceID)).StatusCode)
	awaitCascadeStatusResponse(t, tr.requests)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, m.Shutdown(ctx))
	require.Zero(t, m.count())
	require.Equal(t, 503, subscribeCatalog(t, m, newCatalogSubscribeRequest(t, p, p.LocalDeviceID)).StatusCode)
}

func TestCascadeCatalogSubscriptionProjectionChangeBetweenBatchesPreservesAcknowledgedIDs(t *testing.T) {
	db, p, store := cascadeDeviceStatusFixture(t, "2022")
	require.NoError(t, db.Exec("UPDATE gb_channel SET status=0 WHERE id=20").Error)
	require.NoError(t, db.Exec("INSERT INTO gb_channel(id,device_id,channel_id,status,owner_dept_id) VALUES(21,'source-only-device','source-two',0,0)").Error)
	var device model.GbCascadeDeviceProjection
	require.NoError(t, db.Where("platform_id=?", p.ID).First(&device).Error)
	require.NoError(t, db.Create(&model.GbCascadeChannelProjection{PlatformID: p.ID, DeviceProjectionID: device.ID, SourceChannelID: 21, PublishedChannelID: "34020000001320000013", Active: true}).Error)
	calls := 0
	tr := &subscriptionTransport{requests: make(chan *sip.Request, 10), status: 200, before: func(_ context.Context, _ *sip.Request) error {
		calls++
		if calls == 1 {
			if err := db.Exec("UPDATE gb_cascade_channel_projection SET active=0 WHERE published_channel_id='34020000001320000012'").Error; err != nil {
				return err
			}
			return db.Exec("UPDATE gb_cascade_platform SET projection_revision=projection_revision+1 WHERE id=?", p.ID).Error
		}
		return nil
	}}
	m := newCascadeCatalogSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), 5*time.Millisecond)
	defer m.Shutdown(context.Background())
	require.Equal(t, 200, subscribeCatalog(t, m, newCatalogSubscribeRequest(t, p, p.LocalDeviceID)).StatusCode)
	first := awaitCascadeStatusResponse(t, tr.requests)
	parsed, err := manscdp.ParseCatalogNotify(first.Body())
	require.NoError(t, err)
	require.Equal(t, "OFF", parsed.DeviceList.Items[0].Event)
	next := awaitCascadeStatusResponse(t, tr.requests)
	parsed, err = manscdp.ParseCatalogNotify(next.Body())
	require.NoError(t, err)
	require.Equal(t, "DEL", parsed.DeviceList.Items[0].Event, "an acknowledged item removed between batches must be deleted upstream")
}

func TestCascadeCatalogSubscriptionExpiryTerminates(t *testing.T) {
	_, p, store := cascadeDeviceStatusFixture(t, "2022")
	tr := &cascadeStatusTransport{requests: make(chan *sip.Request, 10)}
	m := newCascadeCatalogSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), time.Hour)
	defer m.Shutdown(context.Background())
	req := newCatalogSubscribeRequest(t, p, p.LocalDeviceID)
	req.RemoveHeader("Expires")
	req.AppendHeader(sip.NewHeader("Expires", "1"))
	require.Equal(t, 200, subscribeCatalog(t, m, req).StatusCode)
	awaitCascadeStatusResponse(t, tr.requests)
	end := awaitCascadeStatusResponse(t, tr.requests)
	require.Equal(t, "terminated;reason=timeout", end.GetHeader("Subscription-State").Value())
	require.Eventually(t, func() bool { return m.count() == 0 }, time.Second, time.Millisecond)
}

func TestCascadeCatalogSubscriptionStopsOnPlatformRevocation(t *testing.T) {
	for _, change := range []string{"enabled=0", "config_revision=config_revision+1", "deleted_at=CURRENT_TIMESTAMP"} {
		t.Run(change, func(t *testing.T) {
			db, p, store := cascadeDeviceStatusFixture(t, "2022")
			tr := &cascadeStatusTransport{requests: make(chan *sip.Request, 10)}
			m := newCascadeCatalogSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), time.Hour)
			defer m.Shutdown(context.Background())
			require.Equal(t, 200, subscribeCatalog(t, m, newCatalogSubscribeRequest(t, p, p.LocalDeviceID)).StatusCode)
			awaitCascadeStatusResponse(t, tr.requests)
			require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET "+change+" WHERE id=?", p.ID).Error)
			require.NoError(t, m.Reload(context.Background()))
			require.Eventually(t, func() bool { return m.count() == 0 }, time.Second, time.Millisecond)
			require.Empty(t, tr.requests)
		})
	}
}

func TestCascadeCatalogSubscriptionRequiresEventID(t *testing.T) {
	_, p, store := cascadeDeviceStatusFixture(t, "2022")
	tr := &cascadeStatusTransport{requests: make(chan *sip.Request, 10)}
	m := newCascadeCatalogSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), time.Hour)
	defer m.Shutdown(context.Background())
	req := newCatalogSubscribeRequest(t, p, p.LocalDeviceID)
	req.RemoveHeader("Event")
	req.AppendHeader(sip.NewHeader("Event", "Catalog"))
	require.Equal(t, 489, subscribeCatalog(t, m, req).StatusCode)
}

func TestCascadeCatalogSubscriptionMultipleUpstreamsKeepPublishedIDsSeparate(t *testing.T) {
	db, a, store := cascadeDeviceStatusFixture(t, "2022")
	b := a
	b.ID = 0
	b.Name = "other upstream"
	b.UpstreamServerID = "44010000002000000001"
	b.LocalDeviceID = "44010000002000000002"
	b.Host = "192.0.2.11"
	require.NoError(t, db.Create(&b).Error)
	device := model.GbCascadeDeviceProjection{PlatformID: b.ID, SourceDeviceID: 10, PublishedDeviceID: "44010000001320000011", Active: true}
	require.NoError(t, db.Create(&device).Error)
	require.NoError(t, db.Create(&model.GbCascadeChannelProjection{PlatformID: b.ID, DeviceProjectionID: device.ID, SourceChannelID: 20, PublishedChannelID: "44010000001320000012", Active: true}).Error)
	tr := &cascadeStatusTransport{requests: make(chan *sip.Request, 10)}
	m := newCascadeCatalogSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), 5*time.Millisecond)
	defer m.Shutdown(context.Background())
	for _, p := range []model.GbCascadePlatform{a, b} {
		r := newCatalogSubscribeRequest(t, p, p.LocalDeviceID)
		r.SetSource(net.JoinHostPort(p.Host, strconv.Itoa(p.Port)))
		require.Equal(t, 200, subscribeCatalog(t, m, r).StatusCode)
		awaitCascadeStatusResponse(t, tr.requests)
	}
	require.NoError(t, db.Exec("UPDATE gb_channel SET status=0 WHERE id=20").Error)
	seen := map[string]bool{}
	for range 2 {
		n := awaitCascadeStatusResponse(t, tr.requests)
		parsed, err := manscdp.ParseCatalogNotify(n.Body())
		require.NoError(t, err)
		require.Len(t, parsed.DeviceList.Items, 1)
		require.Equal(t, "OFF", parsed.DeviceList.Items[0].Event)
		expected := "34020000001320000012"
		if n.To().Address.User == b.UpstreamServerID {
			expected = "44010000001320000012"
		}
		require.Equal(t, expected, parsed.DeviceList.Items[0].DeviceID)
		seen[expected] = true
	}
	require.Len(t, seen, 2)
}

func TestCascadeCatalogSubscriptionBodylessRefreshAndDefaultExpiry(t *testing.T) {
	_, p, store := cascadeDeviceStatusFixture(t, "2022")
	tr := &cascadeStatusTransport{requests: make(chan *sip.Request, 10)}
	m := newCascadeCatalogSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), time.Hour)
	defer m.Shutdown(context.Background())
	req := newCatalogSubscribeRequest(t, p, p.LocalDeviceID)
	req.RemoveHeader("Expires")
	response := subscribeCatalog(t, m, req)
	require.Equal(t, 200, response.StatusCode)
	require.Equal(t, "600", response.GetHeader("Expires").Value())
	awaitCascadeStatusResponse(t, tr.requests)
	tag, _ := response.To().Params.Get("tag")
	req.To().Params.Add("tag", tag)
	req.CSeq().SeqNo++
	req.SetBody(nil)
	req.AppendHeader(sip.NewHeader("Expire", "300"))
	response = subscribeCatalog(t, m, req)
	require.Equal(t, 200, response.StatusCode)
	require.Equal(t, "300", response.GetHeader("Expires").Value())
	n := awaitCascadeStatusResponse(t, tr.requests)
	parsed, err := manscdp.ParseCatalogNotify(n.Body())
	require.NoError(t, err)
	require.Equal(t, 81, parsed.SN)
}

func TestCascadeCatalogSubscriptionRemovedEmptyScopeTerminates(t *testing.T) {
	db, p, store := cascadeDeviceStatusFixture(t, "2022")
	require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET publish_civil=1 WHERE id=?", p.ID).Error)
	require.NoError(t, db.Exec("UPDATE gb_channel SET civil_code='340203' WHERE id=20").Error)
	p.PublishCivil = true
	tr := &cascadeStatusTransport{requests: make(chan *sip.Request, 10)}
	m := newCascadeCatalogSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), 5*time.Millisecond)
	defer m.Shutdown(context.Background())
	require.Equal(t, 200, subscribeCatalog(t, m, newCatalogSubscribeRequest(t, p, "340203")).StatusCode)
	awaitCascadeStatusResponse(t, tr.requests)
	// Remove the published region and its only channel together.
	require.NoError(t, db.Exec("UPDATE gb_cascade_channel_projection SET active=0 WHERE platform_id=?", p.ID).Error)
	require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET projection_revision=projection_revision+1 WHERE id=?", p.ID).Error)
	for {
		n := awaitCascadeStatusResponse(t, tr.requests)
		if strings.HasPrefix(n.GetHeader("Subscription-State").Value(), "terminated") {
			break
		}
	}
	require.Eventually(t, func() bool { return m.count() == 0 }, time.Second, time.Millisecond)
}

func TestCascadeCatalogSubscriptionRefreshPreservesDialogURIs(t *testing.T) {
	_, p, store := cascadeDeviceStatusFixture(t, "2022")
	tr := &cascadeStatusTransport{requests: make(chan *sip.Request, 10)}
	m := newCascadeCatalogSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), time.Hour)
	defer m.Shutdown(context.Background())
	req := newCatalogSubscribeRequest(t, p, "34020000001320000012")
	response := subscribeCatalog(t, m, req)
	require.Equal(t, 200, response.StatusCode)
	awaitCascadeStatusResponse(t, tr.requests)
	tag, _ := response.To().Params.Get("tag")
	for _, change := range []func(*sip.Request){
		func(r *sip.Request) { r.From().Address.Host = "changed.example" },
		func(r *sip.Request) { r.To().Address.User = "34020000001320000012" },
	} {
		r := req.Clone()
		r.To().Params.Add("tag", tag)
		r.CSeq().SeqNo = 2
		change(r)
		require.Equal(t, 481, subscribeCatalog(t, m, r).StatusCode)
	}
}

type catalogSubscriptionRespondHook struct {
	sip.ServerTransaction
	after func()
}

func (tx catalogSubscriptionRespondHook) Respond(response *sip.Response) error {
	err := tx.ServerTransaction.Respond(response)
	if err == nil && response.StatusCode == 200 {
		tx.after()
	}
	return err
}

func TestCascadeCatalogSubscriptionChangeImmediatelyAfterAcceptanceIsNotInitialBaseline(t *testing.T) {
	db, p, store := cascadeDeviceStatusFixture(t, "2022")
	tr := &cascadeStatusTransport{requests: make(chan *sip.Request, 10)}
	m := newCascadeCatalogSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), time.Hour)
	defer m.Shutdown(context.Background())
	req := newCatalogSubscribeRequest(t, p, p.LocalDeviceID)
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()
	wrapper := catalogSubscriptionRespondHook{ServerTransaction: tx, after: func() {
		require.NoError(t, db.Exec("UPDATE gb_cascade_channel_projection SET name='renamed' WHERE platform_id=?", p.ID).Error)
		require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET projection_revision=projection_revision+1 WHERE id=?", p.ID).Error)
	}}
	require.True(t, m.Handle(req, wrapper))
	n := awaitCascadeStatusResponse(t, tr.requests)
	parsed, err := manscdp.ParseCatalogNotify(n.Body())
	require.NoError(t, err)
	require.Len(t, parsed.DeviceList.Items, 1)
	require.Equal(t, "UPDATE", parsed.DeviceList.Items[0].Event)
	require.Equal(t, "renamed", parsed.DeviceList.Items[0].Name)
}

func TestCascadeCatalogSubscriptionUpdatesAndRetriesUnacknowledgedBatch(t *testing.T) {
	db, p, store := cascadeDeviceStatusFixture(t, "2022")
	failed := false
	tr := &subscriptionTransport{requests: make(chan *sip.Request, 15), status: 200}
	// A temporary failure must not advance the acknowledged catalog metadata.
	tr.before = func(_ context.Context, r *sip.Request) error {
		parsed, err := manscdp.ParseCatalogNotify(r.Body())
		if err != nil {
			return err
		}
		tr.status = 200
		if len(parsed.DeviceList.Items) > 0 && parsed.DeviceList.Items[0].Event == "UPDATE" && !failed {
			failed = true
			tr.status = 500
		}
		return nil
	}
	m := newCascadeCatalogSubscriptions(store, newCascadePlatformClientFactory(tr, nil, time.Second), 5*time.Millisecond)
	defer m.Shutdown(context.Background())
	require.Equal(t, 200, subscribeCatalog(t, m, newCatalogSubscribeRequest(t, p, p.LocalDeviceID)).StatusCode)
	awaitCascadeStatusResponse(t, tr.requests)
	require.NoError(t, db.Exec("UPDATE gb_cascade_channel_projection SET name='updated' WHERE platform_id=?", p.ID).Error)
	first := awaitCascadeStatusResponse(t, tr.requests)
	second := awaitCascadeStatusResponse(t, tr.requests)
	require.Equal(t, first.Body(), second.Body())
	require.Greater(t, second.CSeq().SeqNo, first.CSeq().SeqNo)
	parsed, err := manscdp.ParseCatalogNotify(second.Body())
	require.NoError(t, err)
	require.Equal(t, "UPDATE", parsed.DeviceList.Items[0].Event)
	require.Equal(t, "updated", parsed.DeviceList.Items[0].Name)
	require.NoError(t, db.Exec("UPDATE gb_cascade_channel_projection SET published_channel_id='34020000001320000099' WHERE platform_id=?", p.ID).Error)
	require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET projection_revision=projection_revision+1 WHERE id=?", p.ID).Error)
	removed := awaitCascadeStatusResponse(t, tr.requests)
	added := awaitCascadeStatusResponse(t, tr.requests)
	parsed, err = manscdp.ParseCatalogNotify(removed.Body())
	require.NoError(t, err)
	require.Equal(t, "DEL", parsed.DeviceList.Items[0].Event)
	parsed, err = manscdp.ParseCatalogNotify(added.Body())
	require.NoError(t, err)
	require.Equal(t, "ADD", parsed.DeviceList.Items[0].Event)
	require.Equal(t, "34020000001320000099", parsed.DeviceList.Items[0].DeviceID)
}

func TestCascadeCatalogSubscriptionTimeoutEndsDialog(t *testing.T) {
	_, p, store := cascadeDeviceStatusFixture(t, "2022")
	tr := &subscriptionTransport{requests: make(chan *sip.Request, 10), status: 200, before: func(ctx context.Context, _ *sip.Request) error { <-ctx.Done(); return ctx.Err() }}
	m := newCascadeCatalogSubscriptions(store, newCascadePlatformClientFactory(tr, nil, 10*time.Millisecond), 5*time.Millisecond)
	defer m.Shutdown(context.Background())
	require.Equal(t, 200, subscribeCatalog(t, m, newCatalogSubscribeRequest(t, p, p.LocalDeviceID)).StatusCode)
	require.Eventually(t, func() bool { return m.count() == 0 }, time.Second, time.Millisecond)
	require.Len(t, tr.requests, 1, "NOTIFY transaction timeout terminates the subscription under RFC 6665")
}
