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
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
	"uvplatform.com/uvp-gb28181/internal/authoritytest"
)

func TestCascadeStorageQueryAuthorizedEndToEnd(t *testing.T) {
	for _, test := range []struct {
		name, payload string
		wantError     bool
	}{
		{"one card", `<SumNum>1</SumNum><SDCardStatusInfo Num="1"><Item><ID>1</ID><HddName>录像卡</HddName><Status>ok</Status><Capacity>100</Capacity><FreeSpace>90</FreeSpace><Password>secret</Password></Item></SDCardStatusInfo>`, false},
		{"no cards", `<SumNum>0</SumNum>`, false},
		{"incomplete", `<SumNum>2</SumNum><SDCardStatusInfo Num="0"/>`, true},
		{"source epoch changed", `<SumNum>0</SumNum>`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			downstream := make(chan []byte, 2)
			sender := cascadeRecordSenderFunc(func(_ context.Context, device, address, transport string, body []byte) (uac.TrackedMessageResult, error) {
				downstream <- append([]byte(nil), body...)
				return uac.TrackedMessageResult{CallID: "storage-downstream", CSeq: "1", StatusCode: 200, Attempted: true}, nil
			})
			db, platform, store, service := cascadeAuthorizedControlFixture(t, "2022", sender)
			require.NoError(t, db.AutoMigrate(&gbmodels.GbDeviceStorageCard{}))
			require.NoError(t, db.Create(&gbmodels.GbDeviceStorageCard{DeviceID: 10, TargetCode: "34020000001320000002", CardID: 7, HddName: "old-cache", Status: gbmodels.StorageCardStateOK}).Error)
			// Reading a published resource does not grant or require PTZ movement permission.
			require.NoError(t, db.Exec("UPDATE gb_cascade_platform SET ptz_enabled=0").Error)
			require.NoError(t, db.Exec("UPDATE gb_cascade_channel_projection SET ptz_allowed=0").Error)
			scheduler := ptz.NewScheduler(service, ptz.WithSchedulerInterval(5*time.Millisecond))
			scheduler.Start(t.Context())
			t.Cleanup(scheduler.Stop)
			upstream := &cascadeStatusTransport{requests: make(chan *sip.Request, 3)}
			handler := newCascadeMessageHandler(store, newCascadePlatformClientFactory(upstream, nil, time.Second))
			req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, []byte(`<Query><CmdType>SDCardStatus</CmdType><SN>97</SN><DeviceID>34020000001320000012</DeviceID></Query>`))
			tx := siptest.NewServerTxRecorder(req)
			defer tx.Terminate()
			require.True(t, handler(req, tx), "storage query must reach the cascade responder")
			require.Equal(t, 200, tx.Result()[0].StatusCode)
			var query []byte
			select {
			case query = <-downstream:
			case <-time.After(2 * time.Second):
				t.Fatal("no downstream storage query")
			}
			head, err := manscdp.ParseHead(query)
			require.NoError(t, err)
			require.Equal(t, manscdp.CmdSDCardStatus, head.CmdType)
			require.Equal(t, "34020000001320000002", head.DeviceID)
			require.NotEqual(t, "97", head.SN)
			var operation gbmodels.GbPTZOperation
			require.Eventually(t, func() bool {
				return db.Where("sn=?", head.SN).Take(&operation).Error == nil && operation.Status == gbmodels.PTZOperationSent
			}, time.Second, 5*time.Millisecond)
			require.NotNil(t, operation.DeviceEpoch)
			require.NotNil(t, operation.DeviceIntentID)
			// Neither a previous cache nor an uncorrelated response may answer this query.
			wrong := []byte(`<Response><CmdType>SDCardStatus</CmdType><SN>999999</SN><DeviceID>34020000001320000002</DeviceID><SumNum>0</SumNum></Response>`)
			require.NoError(t, service.OnPTZMessage(t.Context(), "34020000001320000001", "wrong-response", "2", wrong))
			select {
			case early := <-upstream.requests:
				t.Fatalf("unrelated or cached storage state was forwarded: %s", early.Body())
			case <-time.After(40 * time.Millisecond):
			}
			if test.name == "source epoch changed" {
				require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=2 WHERE id=10").Error)
			}
			response := []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><Response><CmdType>SDCardStatus</CmdType><SN>%s</SN><DeviceID>%s</DeviceID>%s<PrivateDeviceID>private-source</PrivateDeviceID></Response>`, head.SN, head.DeviceID, test.payload))
			require.NoError(t, service.OnPTZMessage(t.Context(), "34020000001320000001", "storage-response", "2", response))
			for attempt := 0; attempt < 2; attempt++ {
				if attempt > 0 {
					replay := siptest.NewServerTxRecorder(req)
					defer replay.Terminate()
					require.True(t, handler(req, replay))
					require.Equal(t, 200, replay.Result()[0].StatusCode)
				}
				up := awaitCascadeStatusResponse(t, upstream.requests)
				head, err := manscdp.ParseHead(up.Body())
				require.NoError(t, err)
				require.Equal(t, "97", head.SN)
				require.Equal(t, "34020000001320000012", head.DeviceID)
				require.NotContains(t, string(up.Body()), "private-source")
				require.NotContains(t, string(up.Body()), "secret")
				require.NotContains(t, string(up.Body()), "34020000001320000002")
				require.NotContains(t, string(up.Body()), "old-cache")
				if test.wantError {
					require.Contains(t, string(up.Body()), "<Result>ERROR</Result>")
					require.NotContains(t, string(up.Body()), "<SumNum>0</SumNum>")
				} else {
					parsed, err := manscdp.ParseSDCardStatusResponse(up.Body())
					require.NoError(t, err)
					if test.name == "one card" {
						require.Equal(t, "录像卡", parsed.Items[0].HddName)
					}
					require.Contains(t, string(up.Body()), string(protocol.CharsetGB18030))
				}
			}
			var cards []gbmodels.GbDeviceStorageCard
			require.NoError(t, db.Find(&cards).Error)
			if test.name == "no cards" {
				require.Empty(t, cards, "an explicit complete empty response removes stale cards")
			}
			if test.name == "incomplete" {
				require.Len(t, cards, 1)
				require.Equal(t, "old-cache", cards[0].HddName, "invalid data must not overwrite the previous observation")
			}
			require.Empty(t, downstream, "same transaction must not query the device twice")
		})
	}
}

func TestCascadeStorageQueryRejectsUnpublishedTargets(t *testing.T) {
	for _, test := range []struct {
		name, target, statement string
		mismatch, unknownSource bool
		status                  int
	}{
		{name: "hidden parent", target: "34020000001320000011", status: 403},
		{name: "unpublished channel", target: "34020000001320000099", status: 403},
		{name: "platform has no card target", target: "platform", status: 403},
		{name: "disabled platform", statement: "UPDATE gb_cascade_platform SET enabled=0", status: 403},
		{name: "SIP target mismatch", mismatch: true, status: 403},
		{name: "unknown upstream", unknownSource: true},
		{name: "invalid ID", target: "invalid", status: 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, platform, store := cascadeDeviceStatusFixture(t, "2022")
			if test.statement != "" {
				require.NoError(t, db.Exec(test.statement).Error)
			}
			target := test.target
			if target == "" {
				target = "34020000001320000012"
			}
			if target == "platform" {
				target = platform.LocalDeviceID
			}
			req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, []byte(fmt.Sprintf(`<Query><CmdType>SDCardStatus</CmdType><SN>97</SN><DeviceID>%s</DeviceID></Query>`, target)))
			if test.mismatch {
				req.Recipient.User = "34020000001320000099"
			}
			if test.unknownSource {
				req.SetSource("192.0.2.99:5060")
			}
			executed := make(chan struct{}, 1)
			handler := newCascadeStorageQueryHandlerWithQuery(store, newCascadePlatformClientFactory(&cascadeStatusTransport{requests: make(chan *sip.Request, 1)}, nil, time.Second), func(context.Context, cascadeDeviceStatusTarget, string) ([]byte, error) {
				executed <- struct{}{}
				return nil, fmt.Errorf("must not query")
			})
			tx := siptest.NewServerTxRecorder(req)
			defer tx.Terminate()
			require.Equal(t, test.status != 0, handler(req, tx))
			if test.status != 0 {
				require.Equal(t, test.status, tx.Result()[0].StatusCode)
			}
			require.Empty(t, executed)
		})
	}
}

func TestCascadeStorageQueryFailureAndRevocation(t *testing.T) {
	for _, test := range []string{"timeout", "revoked", "oversized UDP"} {
		t.Run(test, func(t *testing.T) {
			db, platform, store := cascadeDeviceStatusFixture(t, "2022")
			upstream := &cascadeStatusTransport{requests: make(chan *sip.Request, 1)}
			finished := make(chan struct{})
			handler := newCascadeStorageQueryHandlerWithQuery(store, newCascadePlatformClientFactory(upstream, nil, time.Second), func(context.Context, cascadeDeviceStatusTarget, string) ([]byte, error) {
				defer close(finished)
				if test == "revoked" {
					require.NoError(t, db.Model(&platform).Update("projection_revision", platform.ProjectionRevision+1).Error)
				}
				if test == "oversized UDP" {
					return manscdp.BuildSDCardStatusResponseWithProfile(protocol.ProfileFor(protocol.Version2022), "34020000001320000002", 5, manscdp.SDCardStatus{SumNum: 1, Items: []manscdp.SDCardItem{{ID: 1, HddName: strings.Repeat("long", 500), Status: manscdp.SDCardStateOK, Capacity: 100, FreeSpace: 90}}})
				}
				return nil, context.DeadlineExceeded
			})
			req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, []byte(`<Query><CmdType>SDCardStatus</CmdType><SN>97</SN><DeviceID>34020000001320000012</DeviceID></Query>`))
			tx := siptest.NewServerTxRecorder(req)
			defer tx.Terminate()
			require.True(t, handler(req, tx))
			require.Equal(t, 200, tx.Result()[0].StatusCode)
			<-finished
			if test == "revoked" {
				select {
				case <-upstream.requests:
					t.Fatal("revoked publication was sent")
				case <-time.After(100 * time.Millisecond):
				}
				return
			}
			response := awaitCascadeStatusResponse(t, upstream.requests)
			require.Contains(t, string(response.Body()), "<Result>ERROR</Result>")
			require.NotContains(t, string(response.Body()), "<SumNum>0</SumNum>")
		})
	}
}

func TestCascadeStorageQueryRejectsUnauthorizedSource(t *testing.T) {
	for _, test := range []struct{ name, statement string }{
		{"cleanup pending", "UPDATE gb_device SET access_epoch=2 WHERE id=10"},
		{"missing epoch", "UPDATE gb_device SET access_epoch=NULL WHERE id=10"},
		{"owner mismatch", "UPDATE gb_channel SET owner_dept_id=1 WHERE id=20"},
		{"deleted source", "UPDATE gb_device SET deleted_at=CURRENT_TIMESTAMP WHERE id=10"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			dispatched := make(chan struct{}, 1)
			sender := cascadeRecordSenderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
				dispatched <- struct{}{}
				return uac.TrackedMessageResult{StatusCode: 200, Attempted: true}, nil
			})
			db, _, _, _ := cascadeAuthorizedControlFixture(t, "2022", sender)
			require.NoError(t, db.Exec(test.statement).Error)
			_, err := queryCascadeStorage(t.Context(), cascadeDeviceStatusTarget{SourceDeviceID: 10, SourceChannelID: 20}, "denied-storage")
			require.Error(t, err)
			var count int64
			require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Count(&count).Error)
			require.Zero(t, count)
			require.Empty(t, dispatched)
		})
	}
}

func TestCascadeStorageQueryReturnsAllEightCardsOverTCP(t *testing.T) {
	db, platform, store := cascadeDeviceStatusFixture(t, "2022")
	platform.Transport = "TCP"
	require.NoError(t, db.Model(&platform).Update("transport", platform.Transport).Error)
	items := make([]manscdp.SDCardItem, 8)
	for i := range items {
		items[i] = manscdp.SDCardItem{ID: i + 1, HddName: fmt.Sprintf("录像卡 %d", i+1), Status: manscdp.SDCardStateOK, Capacity: 100, FreeSpace: 90}
	}
	source, err := manscdp.BuildSDCardStatusResponseWithProfile(protocol.ProfileFor(protocol.Version2022), "34020000001320000002", 5, manscdp.SDCardStatus{SumNum: 8, Items: items})
	require.NoError(t, err)
	upstream := &cascadeStatusTransport{requests: make(chan *sip.Request, 1)}
	handler := newCascadeStorageQueryHandlerWithQuery(store, newCascadePlatformClientFactory(upstream, nil, time.Second), func(context.Context, cascadeDeviceStatusTarget, string) ([]byte, error) { return source, nil })
	req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, []byte(`<Query><CmdType>SDCardStatus</CmdType><SN>97</SN><DeviceID>34020000001320000012</DeviceID></Query>`))
	req.SetTransport("TCP")
	req.Via().Transport = "TCP"
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()
	require.True(t, handler(req, tx))
	require.Equal(t, 200, tx.Result()[0].StatusCode)
	response := awaitCascadeStatusResponse(t, upstream.requests)
	require.Equal(t, "TCP", response.Transport())
	status, err := manscdp.ParseSDCardStatusResponseFor(response.Body(), manscdp.SDCardStatusExpectation{SN: 97, DeviceID: "34020000001320000012"})
	require.NoError(t, err)
	require.Equal(t, 8, status.SumNum)
	require.Equal(t, items, status.Items)
}
