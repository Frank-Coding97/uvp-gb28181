package gb28181

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/cascade/model"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
	"uvplatform.com/uvp-gb28181/app/gb28181/ptz"
	"uvplatform.com/uvp-gb28181/app/gb28181/uac"
	"uvplatform.com/uvp-gb28181/internal/authoritytest"
)

func TestCascadeConfigWriteAuthorizedEndToEnd(t *testing.T) {
	if !authoritytest.InProcess(t) {
		return
	}
	downstream := make(chan []byte, 4)
	sender := cascadeRecordSenderFunc(func(_ context.Context, device, address, transport string, body []byte) (uac.TrackedMessageResult, error) {
		if device != "34020000001320000001" || address != "192.0.2.30:5060" {
			return uac.TrackedMessageResult{}, fmt.Errorf("wrong downstream target")
		}
		downstream <- append([]byte(nil), body...)
		return uac.TrackedMessageResult{CallID: "config-write-downstream", CSeq: "1", StatusCode: 200, Attempted: true}, nil
	})
	db, platform, store, service := cascadeAuthorizedControlFixture(t, "2022", sender)
	configureCascadeSnapshotIngress(t, db)
	require.NoError(t, db.Model(&platform).Updates(map[string]interface{}{"config_enabled": true, "ptz_enabled": false, "transport": "TCP"}).Error)
	require.NoError(t, db.Table("gb_device").Where("id = ?", 10).Update("effective_version", "2022").Error)
	require.NoError(t, db.AutoMigrate(&model.GbCascadeSnapshotSession{}))
	platform.Transport = "TCP"
	scheduler := ptz.NewScheduler(service, ptz.WithSchedulerInterval(5*time.Millisecond))
	scheduler.Start(t.Context())
	t.Cleanup(scheduler.Stop)
	upstream := &cascadeStatusTransport{requests: make(chan *sip.Request, 4)}
	handler := newCascadeMessageHandler(store, newCascadePlatformClientFactory(upstream, nil, time.Second))
	body := []byte(`<Control><CmdType>DeviceConfig</CmdType><SN>97</SN><DeviceID>34020000001320000012</DeviceID><BasicParam><HeartBeatInterval>60</HeartBeatInterval><Password>secret</Password></BasicParam><FrameMirror>2</FrameMirror><VideoParamAttribute Num="0"/><SVACEncodeConfig><AudioParam><AudioRecognitionFlag>1</AudioRecognitionFlag></AudioParam></SVACEncodeConfig><SVACDecodeConfig><SVCParam><SVCSTMMode>3</SVCSTMMode></SVCParam></SVACDecodeConfig><VideoRecordPlan><RecordEnable>0</RecordEnable><RecordScheduleSumNum>0</RecordScheduleSumNum><StreamNumber>0</StreamNumber></VideoRecordPlan><VideoAlarmRecord><RecordEnable>0</RecordEnable><StreamNumber>0</StreamNumber></VideoAlarmRecord><PictureMask><On>0</On><SumNum>0</SumNum></PictureMask><AlarmReport><MotionDetection>0</MotionDetection><FieldDetection>0</FieldDetection></AlarmReport><OSDConfig><Length>704</Length><Width>576</Width><TimeX>0</TimeX><TimeY>0</TimeY><SumNum>0</SumNum></OSDConfig><SnapShotConfig><SnapNum>1</SnapNum><UploadURL>https://example.test/upload</UploadURL><SessionID>01234567890123456789012345678901</SessionID></SnapShotConfig></Control>`)
	req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, body)
	req.SetTransport("TCP")
	req.Via().Transport = "TCP"
	tx := siptest.NewServerTxRecorder(req)
	defer tx.Terminate()
	require.True(t, handler(req, tx))
	require.Equal(t, 200, tx.Result()[0].StatusCode)
	for _, targetCode := range []string{"34020000001320000002", "34020000001320000001"} {
		var sent []byte
		select {
		case sent = <-downstream:
		case <-time.After(3 * time.Second):
			t.Fatal("no configuration command")
		}
		command, err := manscdp.ParseCascadeConfigWrite(sent)
		require.NoError(t, err)
		require.Equal(t, targetCode, command.DeviceID())
		require.NotEqual(t, 97, command.SN())
		require.NotContains(t, string(sent), "secret")
		require.Empty(t, upstream.requests, "SIP 200 is not an application result")
		var operation gbmodels.GbPTZOperation
		require.NoError(t, db.Where("sn=? AND cmd_type=?", command.SN(), "DeviceConfig").Take(&operation).Error)
		require.NotNil(t, operation.DeviceEpoch)
		require.NotEmpty(t, operation.DeviceIntentID)
		require.NotContains(t, operation.PayloadJSON, "secret")
		require.Equal(t, 1, operation.MaxAttempts)
		response := []byte(fmt.Sprintf(`<Response><CmdType>DeviceConfig</CmdType><SN>%d</SN><DeviceID>%s</DeviceID><Result>OK</Result></Response>`, command.SN(), targetCode))
		require.NoError(t, service.OnPTZMessage(t.Context(), "34020000001320000001", "config-write-response", "2", response))
	}
	var response *sip.Request
	select {
	case response = <-upstream.requests:
	case <-time.After(3 * time.Second):
		t.Fatal("no upstream result")
	}
	var ack struct{ CmdType, SN, DeviceID, Result string }
	require.NoError(t, manscdp.DecodeProfiledXML(protocol.ProfileFor(protocol.Version2022), response.Body(), &ack))
	require.Equal(t, "DeviceConfig", ack.CmdType)
	require.Equal(t, "97", ack.SN)
	require.Equal(t, "34020000001320000012", ack.DeviceID)
	require.Equal(t, "OK", ack.Result)
	replay := siptest.NewServerTxRecorder(req)
	defer replay.Terminate()
	require.True(t, handler(req, replay))
	require.Equal(t, 200, replay.Result()[0].StatusCode, replay.Result()[0].Reason)
	select {
	case <-upstream.requests:
	case <-time.After(3 * time.Second):
		t.Fatal("no replay result")
	}
	require.Empty(t, downstream, "transaction retransmission must not repeat writes")
	changed := req.Clone()
	changed.SetBody([]byte(`<Control><CmdType>DeviceConfig</CmdType><SN>97</SN><DeviceID>34020000001320000012</DeviceID><FrameMirror>3</FrameMirror></Control>`))
	changedTx := siptest.NewServerTxRecorder(changed)
	defer changedTx.Terminate()
	require.True(t, handler(changed, changedTx))
	require.Equal(t, 409, changedTx.Result()[0].StatusCode, "a changed capture transaction must be rejected before execution")
	require.Empty(t, upstream.requests)
	require.Empty(t, downstream, "a transaction key cannot be rebound to a different write")
	var count int64
	require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Count(&count).Error)
	require.EqualValues(t, 2, count)

	var capture model.GbCascadeSnapshotSession
	require.NoError(t, db.Where("upstream_session_id = ?", "01234567890123456789012345678901").Take(&capture).Error)
	require.Equal(t, model.CascadeSnapshotSessionPending, capture.State)
	noticeBody, err := manscdp.BuildUploadSnapShotFinished(protocol.ProfileFor(protocol.Version2022), manscdp.UploadSnapShotFinished{
		SN: "9001", DeviceID: "34020000001320000002", SessionID: *capture.DownstreamSessionID, FileIDs: []string{"source-file-1", "source-file-2"},
	})
	require.NoError(t, err)
	forgedBody, err := manscdp.BuildUploadSnapShotFinished(protocol.ProfileFor(protocol.Version2022), manscdp.UploadSnapShotFinished{
		SN: "9001", DeviceID: "34020000001320000099", SessionID: *capture.DownstreamSessionID, FileIDs: []string{"forged-file"},
	})
	require.NoError(t, err)
	forgedReq := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, forgedBody)
	forgedReq.From().Address.User = "34020000001320000001"
	forgedTx := siptest.NewServerTxRecorder(forgedReq)
	defer forgedTx.Terminate()
	require.True(t, handler(forgedReq, forgedTx))
	require.Equal(t, 403, forgedTx.Result()[0].StatusCode)
	require.Empty(t, upstream.requests, "a mismatched source must not route the result")
	require.NoError(t, db.Where("id = ?", capture.ID).Take(&capture).Error)
	require.Equal(t, model.CascadeSnapshotSessionPending, capture.State)
	noticeReq := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, noticeBody)
	noticeReq.From().Address.User = "34020000001320000001"
	missingFrom := noticeReq.Clone()
	missingFrom.RemoveHeader("From")
	missingFromTx := siptest.NewServerTxRecorder(missingFrom)
	defer missingFromTx.Terminate()
	require.True(t, handler(missingFrom, missingFromTx))
	require.Equal(t, 403, missingFromTx.Result()[0].StatusCode, "completion without a SIP source identity must be rejected")
	require.Empty(t, upstream.requests)
	var originalEpoch int64
	require.NoError(t, db.Table("gb_device").Select("access_epoch").Where("id = ?", 10).Scan(&originalEpoch).Error)
	require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=access_epoch+1, cleanup_completed_epoch=cleanup_completed_epoch+1 WHERE id=10").Error)
	staleTx := siptest.NewServerTxRecorder(noticeReq)
	defer staleTx.Terminate()
	require.True(t, handler(noticeReq, staleTx))
	require.Equal(t, 403, staleTx.Result()[0].StatusCode, "a newer device registration cannot complete an earlier capture")
	require.Empty(t, upstream.requests)
	require.NoError(t, db.Table("gb_device").Where("id = ?", 10).Updates(map[string]interface{}{"access_epoch": originalEpoch, "cleanup_completed_epoch": originalEpoch}).Error)
	noticeTx := siptest.NewServerTxRecorder(noticeReq)
	defer noticeTx.Terminate()
	require.True(t, handler(noticeReq, noticeTx))
	require.Equal(t, 200, noticeTx.Result()[0].StatusCode)
	require.NoError(t, db.Where("id = ?", capture.ID).Take(&capture).Error)
	require.Equal(t, model.CascadeSnapshotSessionCompleted, capture.State)
	upstreamNotice := awaitCascadeStatusResponse(t, upstream.requests)
	forwarded, err := manscdp.ParseUploadSnapShotFinished(upstreamNotice.Body())
	require.NoError(t, err)
	require.Equal(t, "9001", forwarded.SN)
	require.Equal(t, "34020000001320000012", forwarded.DeviceID)
	require.Equal(t, capture.UpstreamSessionID, forwarded.SessionID)
	require.Equal(t, []string{"source-file-1", "source-file-2"}, forwarded.FileIDs)
	replayedNotice := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, noticeBody)
	replayedNotice.From().Address.User = "34020000001320000001"
	replayNoticeTx := siptest.NewServerTxRecorder(replayedNotice)
	defer replayNoticeTx.Terminate()
	require.True(t, handler(replayedNotice, replayNoticeTx))
	require.Equal(t, 200, replayNoticeTx.Result()[0].StatusCode)
	require.Empty(t, upstream.requests, "a completed SessionID must not be forwarded twice")
	forgedReplayTx := siptest.NewServerTxRecorder(forgedReq)
	defer forgedReplayTx.Terminate()
	require.True(t, handler(forgedReq, forgedReplayTx))
	require.Equal(t, 403, forgedReplayTx.Result()[0].StatusCode, "completed sessions still require the original source identity")
	require.NoError(t, db.Exec("UPDATE gb_device SET access_epoch=access_epoch+1, cleanup_completed_epoch=cleanup_completed_epoch+1 WHERE id=10").Error)
	staleReplayTx := siptest.NewServerTxRecorder(noticeReq)
	defer staleReplayTx.Terminate()
	require.True(t, handler(noticeReq, staleReplayTx))
	require.Equal(t, 403, staleReplayTx.Result()[0].StatusCode, "completed-session retries still require the original device registration")
}

func TestCascadeConfigWriteRejectsMissingPermissionAndInvalidInput(t *testing.T) {
	for _, scenario := range []struct {
		name, mutation, block string
		status                int
	}{
		{"default denied", "", "<FrameMirror>1</FrameMirror>", 403},
		{"read only", "UPDATE gb_cascade_platform SET config_enabled=1", "<VideoParamOpt/>", 400},
		{"missing field", "UPDATE gb_cascade_platform SET config_enabled=1", "<AlarmReport><FieldDetection>0</FieldDetection></AlarmReport>", 400},
		{"unpublished", "UPDATE gb_cascade_platform SET config_enabled=1; UPDATE gb_cascade_channel_projection SET active=0", "<FrameMirror>1</FrameMirror>", 403},
		{"disabled", "UPDATE gb_cascade_platform SET config_enabled=1,enabled=0", "<FrameMirror>1</FrameMirror>", 403},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if !authoritytest.InProcess(t) {
				return
			}
			db, platform, store, _ := cascadeAuthorizedControlFixture(t, "2022", cascadeRecordSenderFunc(func(context.Context, string, string, string, []byte) (uac.TrackedMessageResult, error) {
				return uac.TrackedMessageResult{}, fmt.Errorf("unexpected downstream send")
			}))
			if scenario.mutation != "" {
				require.NoError(t, db.Exec(scenario.mutation).Error)
			}
			body := []byte(`<Control><CmdType>DeviceConfig</CmdType><SN>97</SN><DeviceID>34020000001320000012</DeviceID>` + scenario.block + `</Control>`)
			req := newCascadeStatusRequest(platform, platform.LocalDeviceID, platform.LocalDeviceID, body)
			tx := siptest.NewServerTxRecorder(req)
			defer tx.Terminate()
			handler := newCascadeMessageHandler(store, newCascadePlatformClientFactory(&cascadeStatusTransport{requests: make(chan *sip.Request, 1)}, nil, time.Second))
			require.True(t, handler(req, tx))
			require.Equal(t, scenario.status, tx.Result()[0].StatusCode)
			var count int64
			require.NoError(t, db.Model(&gbmodels.GbPTZOperation{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}
