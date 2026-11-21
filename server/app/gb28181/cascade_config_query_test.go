package gb28181

import (
	"context"
	"encoding/xml"
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

func TestCascadeConfigQueryAuthorizedAllTypes(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	blocks := map[string]string{
		"BasicParam":          `<BasicParam><Name>录像设备</Name><Expiration>3600</Expiration><Password>secret</Password></BasicParam>`,
		"VideoParamOpt":       `<VideoParamOpt><DownloadSpeed>1/2</DownloadSpeed><Resolution>1/2</Resolution></VideoParamOpt>`,
		"SVACEncodeConfig":    `<SVACEncodeConfig><AudioParam><AudioRecognitionFlag>1</AudioRecognitionFlag></AudioParam></SVACEncodeConfig>`,
		"SVACDecodeConfig":    `<SVACDecodeConfig><SVCParam><SVCSTMMode>1</SVCSTMMode><SVCSpaceSupportMode>1</SVCSpaceSupportMode><SVCTimeSupportMode>1</SVCTimeSupportMode></SVCParam></SVACDecodeConfig>`,
		"VideoParamAttribute": `<VideoParamAttribute Num="0"/>`,
		"VideoRecordPlan":     `<VideoRecordPlan><RecordEnable>1</RecordEnable><RecordScheduleSumNum>0</RecordScheduleSumNum><StreamNumber>0</StreamNumber></VideoRecordPlan><VideoRecordPlan><RecordEnable>0</RecordEnable><RecordScheduleSumNum>0</RecordScheduleSumNum><StreamNumber>1</StreamNumber></VideoRecordPlan>`,
		"VideoAlarmRecord":    `<VideoAlarmRecord><RecordEnable>1</RecordEnable><StreamNumber>0</StreamNumber></VideoAlarmRecord><VideoAlarmRecord><RecordEnable>0</RecordEnable><StreamNumber>1</StreamNumber></VideoAlarmRecord>`,
		"PictureMask":         `<PictureMask><On>0</On><SumNum>0</SumNum></PictureMask>`,
		"FrameMirror":         `<FrameMirror>2</FrameMirror>`,
		"AlarmReport":         `<AlarmReport><MotionDetection>1</MotionDetection><FieldDetection>0</FieldDetection></AlarmReport>`,
		"OSDConfig":           `<OSDConfig><Length>704</Length><Width>576</Width><TimeX>0</TimeX><TimeY>0</TimeY><TimeEnable>0</TimeEnable><TimeType>0</TimeType><TextEnable>0</TextEnable><SumNum>0</SumNum></OSDConfig>`,
		"SnapShotConfig":      `<SnapShot><SnapNum>1</SnapNum><UploadURL>https://example.test/upload</UploadURL><SessionID>01234567890123456789012345678901</SessionID></SnapShot>`,
	}
	types := []string{"BasicParam", "VideoParamOpt", "SVACEncodeConfig", "SVACDecodeConfig", "VideoParamAttribute", "VideoRecordPlan", "VideoAlarmRecord", "PictureMask", "FrameMirror", "AlarmReport", "OSDConfig", "SnapShotConfig"}
	downstream := make(chan []byte, 16)
	sender := cascadeRecordSenderFunc(func(_ context.Context, device, address, transport string, body []byte) (uac.TrackedMessageResult, error) {
		downstream <- append([]byte(nil), body...)
		return uac.TrackedMessageResult{CallID: "config-downstream", CSeq: "1", StatusCode: 200, Attempted: true}, nil
	})
	db, platform, store, service := cascadeAuthorizedControlFixture(t, "2022", sender)
	require.NoError(t, db.Model(&platform).Updates(map[string]interface{}{"transport": "TCP", "ptz_enabled": false}).Error)
	require.NoError(t, db.Exec("UPDATE gb_cascade_channel_projection SET ptz_allowed=0").Error)
	platform.Transport = "TCP"
	scheduler := ptz.NewScheduler(service, ptz.WithSchedulerInterval(5*time.Millisecond))
	scheduler.Start(t.Context())
	t.Cleanup(scheduler.Stop)
	upstream := &cascadeStatusTransport{requests: make(chan *sip.Request, 16)}
	handler := newCascadeMessageHandler(store, newCascadePlatformClientFactory(upstream, nil, time.Second))
	req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, []byte(`<Query><CmdType>ConfigDownload</CmdType><SN>97</SN><DeviceID>34020000001320000012</DeviceID><ConfigType>`+strings.Join(types, "/")+`</ConfigType></Query>`))
	req.SetTransport("TCP")
	req.Via().Transport = "TCP"
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()
	require.True(t, handler(req, tx), "configuration query must have a cascade responder")
	require.Equal(t, 200, tx.Result()[0].StatusCode)
	seen := map[string]bool{}
	for range types {
		var query []byte
		select {
		case query = <-downstream:
		case <-time.After(3 * time.Second):
			t.Fatal("missing downstream configuration query")
		}
		var head struct{ CmdType, SN, DeviceID, ConfigType string }
		require.NoError(t, manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), query, &head))
		require.Equal(t, "ConfigDownload", head.CmdType)
		require.NotEqual(t, "97", head.SN)
		require.False(t, seen[head.ConfigType])
		seen[head.ConfigType] = true
		if head.ConfigType == "BasicParam" {
			require.Equal(t, "34020000001320000001", head.DeviceID)
		} else {
			require.Equal(t, "34020000001320000002", head.DeviceID)
		}
		var operation gbmodels.GbPTZOperation
		require.Eventually(t, func() bool {
			return db.Where("sn=?", head.SN).Take(&operation).Error == nil && operation.Status == gbmodels.PTZOperationSent
		}, time.Second, 5*time.Millisecond)
		require.NotNil(t, operation.DeviceEpoch)
		require.NotNil(t, operation.DeviceIntentID)
		response := []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><Response><CmdType>ConfigDownload</CmdType><SN>%s</SN><DeviceID>%s</DeviceID><Result>OK</Result>%s<PrivateDeviceID>private-source</PrivateDeviceID></Response>`, head.SN, head.DeviceID, blocks[head.ConfigType]))
		require.NoError(t, service.OnPTZMessage(t.Context(), "34020000001320000001", "response-"+head.ConfigType, "2", response))
	}
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			replay := siptest.NewServerTxRecorder(req)
			defer replay.Terminate()
			require.True(t, handler(req, replay))
			require.Equal(t, 200, replay.Result()[0].StatusCode)
		}
		for _, kind := range types {
			response := awaitCascadeStatusResponse(t, upstream.requests)
			var result struct {
				XMLName                       xml.Name
				CmdType, SN, DeviceID, Result string
			}
			require.NoError(t, manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2022), response.Body(), &result))
			require.Equal(t, "Response", result.XMLName.Local)
			require.Equal(t, "97", result.SN)
			require.Equal(t, "34020000001320000012", result.DeviceID)
			if kind != "SnapShotConfig" {
				require.Equal(t, "OK", result.Result)
			}
			utf8Body, err := manscdp.DecodeProfiledXMLBytes(protocol.ProfileFor(protocol.Version2022), response.Body())
			require.NoError(t, err)
			body := string(utf8Body)
			if kind == "SnapShotConfig" {
				// This fixture is an unowned device configuration; never publish
				// its upload address or fabricate a partial successful response.
				require.Equal(t, "ERROR", result.Result)
				require.NotContains(t, body, "<SnapShot>")
				continue
			}
			for _, hidden := range []string{"secret", "private-source", "34020000001320000001", "34020000001320000002"} {
				require.NotContains(t, body, hidden)
			}
			element := kind
			if kind == "SnapShotConfig" {
				element = "SnapShot"
			}
			require.Contains(t, body, "<"+element)
			if kind == "BasicParam" {
				require.Contains(t, body, "录像设备")
			}
			if kind == "VideoRecordPlan" {
				require.Equal(t, 2, strings.Count(body, "<VideoRecordPlan>"))
			}
			if kind == "VideoAlarmRecord" {
				require.Equal(t, 2, strings.Count(body, "<VideoAlarmRecord>"))
			}
		}
	}
	require.Empty(t, downstream, "same transaction must not create more reads")
}

func TestCascadeConfigQueryRevalidatesCompletedResultsBeforeSending(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	downstream := make(chan []byte, 4)
	sender := cascadeRecordSenderFunc(func(_ context.Context, device, address, transport string, body []byte) (uac.TrackedMessageResult, error) {
		downstream <- append([]byte(nil), body...)
		return uac.TrackedMessageResult{CallID: "config-revoke", CSeq: "1", StatusCode: 200, Attempted: true}, nil
	})
	db, platform, store, service := cascadeAuthorizedControlFixture(t, "2022", sender)
	scheduler := ptz.NewScheduler(service, ptz.WithSchedulerInterval(5*time.Millisecond))
	scheduler.Start(t.Context())
	t.Cleanup(scheduler.Stop)
	upstream := &cascadeStatusTransport{requests: make(chan *sip.Request, 3)}
	firstReturned := make(chan struct{})
	handler, wait := newCascadeConfigQueryHandlerWithQuery(store, newCascadePlatformClientFactory(upstream, nil, time.Second), func(ctx context.Context, target cascadeDeviceStatusTarget, kind, key string) (cascadeConfigQueryResult, error) {
		body, err := queryCascadeConfig(ctx, target, kind, key)
		if kind == "FrameMirror" && err == nil {
			close(firstReturned)
		}
		return body, err
	})
	t.Cleanup(wait)
	req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, []byte(`<Query><CmdType>ConfigDownload</CmdType><SN>97</SN><DeviceID>34020000001320000012</DeviceID><ConfigType>FrameMirror/BasicParam</ConfigType></Query>`))
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()
	require.True(t, handler(req, tx))
	require.Equal(t, 200, tx.Result()[0].StatusCode)
	responses := map[string][]byte{}
	for range 2 {
		var body []byte
		select {
		case body = <-downstream:
		case <-time.After(2 * time.Second):
			t.Fatal("missing configuration request")
		}
		var head struct{ ConfigType, SN, DeviceID string }
		require.NoError(t, manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2016), body, &head))
		var op gbmodels.GbPTZOperation
		require.Eventually(t, func() bool {
			return db.Where("sn=?", head.SN).Take(&op).Error == nil && op.Status == gbmodels.PTZOperationSent
		}, time.Second, 5*time.Millisecond)
		block := `<FrameMirror>2</FrameMirror>`
		if head.ConfigType == "BasicParam" {
			block = `<BasicParam><Name>old-source-data</Name></BasicParam>`
		}
		responses[head.ConfigType] = []byte(fmt.Sprintf(`<Response><CmdType>ConfigDownload</CmdType><SN>%s</SN><DeviceID>%s</DeviceID><Result>OK</Result>%s</Response>`, head.SN, head.DeviceID, block))
	}
	require.NoError(t, service.OnPTZMessage(t.Context(), "34020000001320000001", "fast-response", "2", responses["FrameMirror"]))
	select {
	case <-firstReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("first configuration read did not complete")
	}
	require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=2, cleanup_completed_epoch=2 WHERE id=10").Error)
	require.NoError(t, service.OnPTZMessage(t.Context(), "34020000001320000001", "slow-response", "2", responses["BasicParam"]))
	for range 2 {
		body := string(awaitCascadeStatusResponse(t, upstream.requests).Body())
		require.Contains(t, body, "<Result>ERROR</Result>", "a result buffered before revocation must not be sent later")
		require.NotContains(t, body, "<FrameMirror>")
		require.NotContains(t, body, "old-source-data")
	}
}

func TestCascadeConfigQueryRejectsUnauthorizedAndInvalidRequests(t *testing.T) {
	for _, test := range []struct {
		name, target, configType, statement string
		mismatch, unknownSource             bool
		status                              int
	}{
		{name: "hidden parent", target: "34020000001320000011", configType: "BasicParam", status: 403},
		{name: "unpublished channel", target: "34020000001320000099", configType: "FrameMirror", status: 403},
		{name: "platform is not a device config", target: "platform", configType: "BasicParam", status: 403},
		{name: "disabled platform", configType: "FrameMirror", statement: "UPDATE gb_cascade_platform SET enabled=0", status: 403},
		{name: "SIP target mismatch", configType: "FrameMirror", mismatch: true, status: 403},
		{name: "unknown upstream", configType: "FrameMirror", unknownSource: true},
		{name: "missing type", status: 400},
		{name: "empty type item", configType: "BasicParam//FrameMirror", status: 400},
		{name: "write command is not a query type", configType: "TeleBoot", status: 400},
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
			req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, []byte(fmt.Sprintf(`<Query><CmdType>ConfigDownload</CmdType><SN>97</SN><DeviceID>%s</DeviceID><ConfigType>%s</ConfigType></Query>`, target, test.configType)))
			if test.mismatch {
				req.Recipient.User = "34020000001320000099"
			}
			if test.unknownSource {
				req.SetSource("192.0.2.99:5060")
			}
			executed := make(chan struct{}, 1)
			handler, wait := newCascadeConfigQueryHandlerWithQuery(store, newCascadePlatformClientFactory(&cascadeStatusTransport{requests: make(chan *sip.Request, 1)}, nil, time.Second), func(context.Context, cascadeDeviceStatusTarget, string, string) (cascadeConfigQueryResult, error) {
				executed <- struct{}{}
				return cascadeConfigQueryResult{}, fmt.Errorf("must not query")
			})
			t.Cleanup(wait)
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

func TestCascadeConfigQueryFailureAndPublicationRevocation(t *testing.T) {
	for _, scenario := range []string{"partial failure", "revoked", "oversized UDP", "duplicate types", "missing source guard"} {
		t.Run(scenario, func(t *testing.T) {
			db, platform, store := cascadeDeviceStatusFixture(t, "2022")
			upstream := &cascadeStatusTransport{requests: make(chan *sip.Request, 3)}
			queries := make(chan string, 3)
			finished := make(chan struct{}, 3)
			handler, wait := newCascadeConfigQueryHandlerWithQuery(store, newCascadePlatformClientFactory(upstream, nil, time.Second), func(_ context.Context, _ cascadeDeviceStatusTarget, kind, _ string) (cascadeConfigQueryResult, error) {
				defer func() { finished <- struct{}{} }()
				queries <- kind
				if scenario == "partial failure" && kind == "BasicParam" {
					return cascadeConfigQueryResult{}, context.DeadlineExceeded
				}
				if scenario == "revoked" {
					require.NoError(t, db.Model(&platform).Update("projection_revision", platform.ProjectionRevision+1).Error)
				}
				block := `<FrameMirror>2</FrameMirror>`
				if scenario == "oversized UDP" {
					block = `<BasicParam><Name>` + strings.Repeat("x", 3000) + `</Name></BasicParam>`
				}
				guard := func(context.Context) error { return nil }
				if scenario == "missing source guard" {
					guard = nil
				}
				return cascadeConfigQueryResult{body: []byte(`<Response><CmdType>ConfigDownload</CmdType><SN>5</SN><DeviceID>34020000001320000002</DeviceID><Result>OK</Result>` + block + `</Response>`), authorize: guard}, nil
			})
			t.Cleanup(wait)
			types := "FrameMirror"
			want := 1
			if scenario == "partial failure" {
				types = "BasicParam/FrameMirror"
				want = 2
			}
			if scenario == "duplicate types" {
				types = "FrameMirror/FrameMirror"
			}
			if scenario == "oversized UDP" {
				types = "BasicParam"
			}
			req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, []byte(`<Query><CmdType>ConfigDownload</CmdType><SN>97</SN><DeviceID>34020000001320000012</DeviceID><ConfigType>`+types+`</ConfigType></Query>`))
			tx := siptest.NewServerTxRecorder(req)
			defer tx.Terminate()
			require.True(t, handler(req, tx))
			require.Equal(t, 200, tx.Result()[0].StatusCode)
			for range want {
				select {
				case <-finished:
				case <-time.After(time.Second):
					t.Fatal("configuration executor was not called")
				}
			}
			require.Len(t, queries, want)
			if scenario == "revoked" {
				select {
				case <-upstream.requests:
					t.Fatal("revoked publication was sent")
				case <-time.After(100 * time.Millisecond):
				}
				return
			}
			for i := range want {
				body := string(awaitCascadeStatusResponse(t, upstream.requests).Body())
				if scenario == "oversized UDP" || scenario == "missing source guard" || scenario == "partial failure" && i == 0 {
					require.Contains(t, body, "<Result>ERROR</Result>")
					require.NotContains(t, body, "<BasicParam>")
				} else {
					require.Contains(t, body, "<Result>OK</Result>")
					require.Contains(t, body, "<FrameMirror>2</FrameMirror>")
				}
			}
		})
	}
}
