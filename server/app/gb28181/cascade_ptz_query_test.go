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

func TestCascadePTZQueryAuthorizedEndToEnd(t *testing.T) {
	for _, test := range []struct {
		cmd, query string
		pages      []string
	}{
		{"PresetQuery", "", []string{`<SumNum>2</SumNum><PresetList Num="1"><Item><PresetID>1</PresetID><PresetName>大门</PresetName></Item></PresetList>`, `<SumNum>2</SumNum><PresetList Num="1"><Item><PresetID>2</PresetID><PresetName>二号</PresetName></Item></PresetList>`}},
		{"HomePositionQuery", "", []string{`<HomePosition><Enabled>1</Enabled><ResetTime>30</ResetTime><PresetIndex>2</PresetIndex></HomePosition>`}},
		{"HomePositionQuery", "", []string{``}},
		{"CruiseTrackListQuery", "", []string{`<SumNum>2</SumNum><CruiseTrackList Num="1"><CruiseTrack><Number>0</Number><Name>巡航一</Name></CruiseTrack></CruiseTrackList>`, `<SumNum>2</SumNum><CruiseTrackList Num="1"><CruiseTrack><Number>1</Number><Name>巡航二</Name></CruiseTrack></CruiseTrackList>`}},
		{"CruiseTrackQuery", "<Number>0</Number>", []string{`<Number>0</Number><Name>巡航一</Name><SumNum>1</SumNum><CruisePointList Num="1"><CruisePoint><PresetIndex>1</PresetIndex><StayTime>10</StayTime><Speed>128</Speed></CruisePoint></CruisePointList>`}},
		{"CruiseTrackQuery", "<Number>0</Number>", []string{`<Number>0</Number><Name>巡航一</Name><SumNum>2</SumNum><CruisePointList Num="1"><CruisePoint><PresetIndex>1</PresetIndex><StayTime>10</StayTime><Speed>128</Speed></CruisePoint></CruisePointList>`, `<Number>0</Number><Name>巡航一</Name><SumNum>2</SumNum><CruisePointList Num="1"><CruisePoint><PresetIndex>2</PresetIndex><StayTime>20</StayTime><Speed>256</Speed></CruisePoint></CruisePointList>`}},
		{"PTZPosition", "", []string{`<Pan>120</Pan><Tilt>30</Tilt><Zoom>2</Zoom>`}},
	} {
		t.Run(test.cmd, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			downstream := make(chan []byte, 2)
			sender := cascadeRecordSenderFunc(func(_ context.Context, device, address, transport string, body []byte) (uac.TrackedMessageResult, error) {
				downstream <- append([]byte(nil), body...)
				return uac.TrackedMessageResult{CallID: "ptz-query-downstream", CSeq: "1", StatusCode: 200, Attempted: true}, nil
			})
			db, platform, store, service := cascadeAuthorizedControlFixture(t, "2022", sender)
			require.NoError(t, db.AutoMigrate(&gbmodels.GbPTZPreset{}, &gbmodels.GbPTZHomePosition{}, &gbmodels.GbPTZCruiseTrack{}, &gbmodels.GbPTZState{}))
			scheduler := ptz.NewScheduler(service, ptz.WithSchedulerInterval(5*time.Millisecond))
			scheduler.Start(t.Context())
			t.Cleanup(scheduler.Stop)
			upstream := &cascadeStatusTransport{requests: make(chan *sip.Request, 10)}
			handler := newCascadeMessageHandler(store, newCascadePlatformClientFactory(upstream, nil, time.Second))
			body := []byte(fmt.Sprintf(`<Query><CmdType>%s</CmdType><SN>97</SN><DeviceID>34020000001320000012</DeviceID>%s</Query>`, test.cmd, test.query))
			req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, body)
			tx := siptest.NewServerTxRecorder(req)
			defer tx.Terminate()
			require.True(t, handler(req, tx), "upstream query must reach the cascade responder")
			require.Equal(t, 200, tx.Result()[0].StatusCode)
			var query []byte
			select {
			case query = <-downstream:
			case <-time.After(2 * time.Second):
				t.Fatal("no downstream query")
			}
			head, err := manscdp.ParseHead(query)
			require.NoError(t, err)
			require.Equal(t, "34020000001320000002", head.DeviceID)
			require.NotEqual(t, "97", head.SN)
			// The registered downstream is 2016; its precise-status response uses the legacy command.
			cmd := test.cmd
			if cmd == "PTZPosition" {
				cmd = manscdp.CmdPTZPreciseStatusQuery
			}
			require.Equal(t, cmd, head.CmdType)
			var operation gbmodels.GbPTZOperation
			require.Eventually(t, func() bool {
				return db.Where("sn=?", head.SN).Take(&operation).Error == nil && operation.Status == gbmodels.PTZOperationSent
			}, time.Second, 5*time.Millisecond)
			for i, page := range test.pages {
				response := []byte(fmt.Sprintf(`<Response><CmdType>%s</CmdType><SN>%s</SN><DeviceID>%s</DeviceID>%s<PrivateDeviceID>source-secret</PrivateDeviceID></Response>`, cmd, head.SN, head.DeviceID, page))
				require.NoError(t, service.OnPTZMessage(t.Context(), "34020000001320000001", fmt.Sprintf("query-response-%d", i), "2", response))
				if i < len(test.pages)-1 {
					require.NoError(t, service.OnPTZMessage(t.Context(), "34020000001320000001", fmt.Sprintf("query-response-repeat-%d", i), "2", response))
					select {
					case early := <-upstream.requests:
						t.Fatalf("partial result was forwarded: %s", early.Body())
					case <-time.After(40 * time.Millisecond):
					}
				}
			}
			for range 1 {
				response := awaitCascadeStatusResponse(t, upstream.requests)
				head, err := manscdp.ParseHead(response.Body())
				require.NoError(t, err)
				require.Equal(t, "97", head.SN)
				require.Equal(t, test.cmd, head.CmdType)
				require.Equal(t, "34020000001320000012", head.DeviceID)
				require.NotContains(t, string(response.Body()), "34020000001320000002")
				require.NotContains(t, string(response.Body()), "source-secret")
				require.NotContains(t, string(response.Body()), "<Result>ERROR</Result>")
				if test.cmd == "PresetQuery" || test.cmd == "CruiseTrackListQuery" {
					require.Contains(t, string(response.Body()), "<SumNum>2</SumNum>")
				}
				if test.cmd == "CruiseTrackQuery" {
					var result cascadePTZResponseXML
					require.NoError(t, manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2022), response.Body(), &result))
					require.NotNil(t, result.CruisePoints)
					require.Len(t, result.CruisePoints.Items, len(test.pages))
					require.Equal(t, 128, result.CruisePoints.Items[0].Speed)
				}
				require.False(t, strings.Contains(string(response.Body()), "<NumLower>"))
				require.Contains(t, string(response.Body()), string(protocol.CharsetGB18030))
			}
			replayTx := siptest.NewServerTxRecorder(req)
			defer replayTx.Terminate()
			require.True(t, handler(req, replayTx))
			require.Equal(t, 200, replayTx.Result()[0].StatusCode)
			replay := awaitCascadeStatusResponse(t, upstream.requests)
			require.NotContains(t, string(replay.Body()), "<Result>ERROR</Result>")
			require.Empty(t, downstream, "a transaction retransmission must reuse its exact query result")
		})
	}
}

func TestCascadePTZQueryRejectsUnauthorizedRequests(t *testing.T) {
	for _, test := range []struct {
		name, cmd, extra, target, statement string
		changeURI, changeSource             bool
		status                              int
	}{
		{name: "hidden device", target: "34020000001320000011", status: 403},
		{name: "unpublished", target: "34020000001320000099", status: 403},
		{name: "platform permission", statement: "UPDATE gb_cascade_platform SET ptz_enabled=0", status: 403},
		{name: "channel permission", statement: "UPDATE gb_cascade_channel_projection SET ptz_allowed=0", status: 403},
		{name: "target mismatch", changeURI: true, status: 403},
		{name: "unrecognized peer", changeSource: true},
		{name: "missing track", cmd: "CruiseTrackQuery", status: 400},
		{name: "negative track", cmd: "CruiseTrackQuery", extra: "<Number>-1</Number>", status: 400},
		{name: "unexpected track", extra: "<Number>1</Number>", status: 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, platform, store := cascadeDeviceStatusFixture(t, "2022")
			require.NoError(t, db.Model(&platform).Update("ptz_enabled", true).Error)
			require.NoError(t, db.Exec("UPDATE gb_cascade_channel_projection SET ptz_allowed=1").Error)
			if test.statement != "" {
				require.NoError(t, db.Exec(test.statement).Error)
			}
			cmd, target := test.cmd, test.target
			if cmd == "" {
				cmd = "PresetQuery"
			}
			if target == "" {
				target = "34020000001320000012"
			}
			req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, []byte(fmt.Sprintf(`<Query><CmdType>%s</CmdType><SN>97</SN><DeviceID>%s</DeviceID>%s</Query>`, cmd, target, test.extra)))
			if test.changeURI {
				req.Recipient.User = "34020000001320000099"
			}
			if test.changeSource {
				req.SetSource("192.0.2.99:5060")
			}
			executed := make(chan struct{}, 1)
			handler := newCascadePTZQueryHandlerWithQuery(store, newCascadePlatformClientFactory(&cascadeStatusTransport{requests: make(chan *sip.Request, 1)}, nil, time.Second), func(context.Context, cascadeDeviceStatusTarget, ptz.QueryKind, int, string) ([][]byte, error) {
				executed <- struct{}{}
				return nil, fmt.Errorf("must not execute")
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

func TestCascadePTZQueryFailureAndPublicationRevocation(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		t.Run(fmt.Sprint(revoked), func(t *testing.T) {
			db, platform, store := cascadeDeviceStatusFixture(t, "2022")
			require.NoError(t, db.Model(&platform).Update("ptz_enabled", true).Error)
			require.NoError(t, db.Exec("UPDATE gb_cascade_channel_projection SET ptz_allowed=1").Error)
			upstream := &cascadeStatusTransport{requests: make(chan *sip.Request, 1)}
			finished := make(chan struct{})
			handler := newCascadePTZQueryHandlerWithQuery(store, newCascadePlatformClientFactory(upstream, nil, time.Second), func(context.Context, cascadeDeviceStatusTarget, ptz.QueryKind, int, string) ([][]byte, error) {
				if revoked {
					require.NoError(t, db.Model(&platform).Update("projection_revision", platform.ProjectionRevision+1).Error)
				}
				close(finished)
				return nil, context.DeadlineExceeded
			})
			req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, []byte(`<Query><CmdType>PresetQuery</CmdType><SN>97</SN><DeviceID>34020000001320000012</DeviceID></Query>`))
			tx := siptest.NewServerTxRecorder(req)
			defer tx.Terminate()
			require.True(t, handler(req, tx))
			require.Equal(t, 200, tx.Result()[0].StatusCode)
			<-finished
			if revoked {
				select {
				case <-upstream.requests:
					t.Fatal("revoked publication was sent")
				case <-time.After(80 * time.Millisecond):
				}
				return
			}
			response := awaitCascadeStatusResponse(t, upstream.requests)
			require.Contains(t, string(response.Body()), "<Result>ERROR</Result>")
			require.NotContains(t, string(response.Body()), "<SumNum>0</SumNum>")
		})
	}
}

func TestCascadePTZQueryResponseTransportBudget(t *testing.T) {
	profile := protocol.ProfileFor(protocol.Version2022)
	query := cascadePTZQuery{CmdType: "PresetQuery", SN: 97, DeviceID: "34020000001320000012"}
	var items strings.Builder
	for id := 1; id <= 3; id++ {
		fmt.Fprintf(&items, "<Item><PresetID>%d</PresetID><PresetName>%s</PresetName></Item>", id, strings.Repeat("x", 200))
	}
	body := []byte(fmt.Sprintf(`<Response><CmdType>PresetQuery</CmdType><SN>1</SN><DeviceID>34020000001320000002</DeviceID><SumNum>3</SumNum><PresetList Num="3">%s</PresetList></Response>`, items.String()))
	pages, err := planCascadePTZQueryResponses(profile, query, [][]byte{body}, func(body []byte) bool { return len(body) <= 900 })
	require.NoError(t, err)
	require.Greater(t, len(pages), 1)
	ids := map[int]bool{}
	for _, page := range pages {
		require.LessOrEqual(t, len(page), 900)
		var result cascadePTZResponseXML
		require.NoError(t, manscdp.DecodeProfiledXML(profile, page, &result))
		require.Equal(t, 3, *result.SumNum)
		require.Equal(t, len(result.PresetList.Items), result.PresetList.Num)
		for _, item := range result.PresetList.Items {
			require.False(t, ids[item.ID])
			ids[item.ID] = true
		}
	}
	require.Len(t, ids, 3)
	_, err = planCascadePTZQueryResponses(profile, query, [][]byte{body}, func([]byte) bool { return false })
	require.Error(t, err, "one oversized item must not be sent as a broken packet")
}

func TestCascadePTZQueryRejectsIncompleteResponse(t *testing.T) {
	for _, fragment := range []string{
		`<Response><CmdType>PresetQuery</CmdType><SN>1</SN><DeviceID>source</DeviceID><SumNum>2</SumNum><PresetList Num="1"><Item><PresetID>1</PresetID><PresetName>one</PresetName></Item></PresetList></Response>`,
		`<Query><CmdType>PresetQuery</CmdType><SN>1</SN><DeviceID>source</DeviceID><SumNum>0</SumNum></Query>`,
	} {
		_, err := planCascadePTZQueryResponses(protocol.ProfileFor(protocol.Version2022), cascadePTZQuery{CmdType: "PresetQuery", SN: 97, DeviceID: "34020000001320000012"}, [][]byte{[]byte(fragment)}, nil)
		require.Error(t, err)
	}
}
