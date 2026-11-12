package handler

import (
	"context"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	"uvplatform.com/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.com/uvp-gb28181/app/gb28181/metrics"
)

type messagePTZRecorder struct {
	bodies [][]byte
}

type blockingConfigProcessor struct {
	entered chan struct{}
	release chan struct{}
}

func (p *blockingConfigProcessor) OnPTZMessage(context.Context, string, string, string, []byte) error {
	close(p.entered)
	<-p.release
	return nil
}

func (r *messagePTZRecorder) OnPTZMessage(_ context.Context, _, _, _ string, body []byte) error {
	r.bodies = append(r.bodies, append([]byte(nil), body...))
	return nil
}

func TestTxKindFromCmd_PTZ2022(t *testing.T) {
	for _, cmd := range []string{manscdp.CmdDeviceControl, manscdp.CmdPTZPreciseCtrl, manscdp.CmdPTZPosition, manscdp.CmdPresetQuery, manscdp.CmdHomePositionQuery, manscdp.CmdCruiseTrackListQuery, manscdp.CmdCruiseTrackQuery, manscdp.CmdPTZPreciseStatusQuery} {
		require.Equal(t, metrics.TxPTZ, txKindFromCmd(cmd), cmd)
	}
}

func TestPTZDeviceCodeUsesSIPFromInsteadOfResponseChannel(t *testing.T) {
	req := sip.NewRequest(sip.MESSAGE, sip.Uri{User: "platform", Host: "127.0.0.1"})
	req.AppendHeader(&sip.FromHeader{Address: sip.Uri{User: "34020000001320000001", Host: "3402000000"}})
	require.Equal(t, "34020000001320000001", ptzDeviceCode(req, "34020000001310000001"))
}

func TestMessageHandlerPTZResponseDispatchesAndAcknowledges(t *testing.T) {
	req := sip.NewRequest(sip.MESSAGE, sip.Uri{User: "platform", Host: "3402000000"})
	prepareNotifyRequest(req)
	req.SetBody([]byte(`<Response><CmdType>DeviceControl</CmdType><SN>7</SN><DeviceID>C</DeviceID><Result>OK</Result></Response>`))
	callID := sip.CallIDHeader("ptz-message")
	req.AppendHeader(&callID)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 3, MethodName: sip.MESSAGE})
	tx := siptest.NewServerTxRecorder(req)
	recorder := &messagePTZRecorder{}
	handler := NewMessageHandler(gbconfig.Config{})
	handler.SetPTZProcessor(recorder)
	handler.Handle(req, tx)

	require.Len(t, recorder.bodies, 1)
	require.Len(t, tx.Result(), 1)
	require.EqualValues(t, 200, tx.Result()[0].StatusCode)
}

func TestMessageHandlerPTZPositionDispatchesAndAcknowledges(t *testing.T) {
	req := sip.NewRequest(sip.MESSAGE, sip.Uri{User: "platform", Host: "3402000000"})
	prepareNotifyRequest(req)
	req.SetBody([]byte("<Response><CmdType>PTZPosition</CmdType><SN>7</SN><DeviceID>C</DeviceID><Pan>1</Pan></Response>"))
	callID := sip.CallIDHeader("ptz-position-message")
	req.AppendHeader(&callID)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 3, MethodName: sip.MESSAGE})
	tx := siptest.NewServerTxRecorder(req)
	recorder := &messagePTZRecorder{}
	handler := NewMessageHandler(gbconfig.Config{})
	handler.SetPTZProcessor(recorder)
	handler.Handle(req, tx)

	require.Len(t, recorder.bodies, 1)
	require.Len(t, tx.Result(), 1)
	require.EqualValues(t, 200, tx.Result()[0].StatusCode)
}

func TestConfigDownloadMessageAcknowledgedBeforePersistence(t *testing.T) {
	req := sip.NewRequest(sip.MESSAGE, sip.Uri{User: "platform", Host: "127.0.0.1"})
	prepareNotifyRequest(req)
	req.SetBody([]byte(`<Response><CmdType>ConfigDownload</CmdType><SN>7</SN><DeviceID>34020000001320000001</DeviceID><Result>OK</Result></Response>`))
	callID := sip.CallIDHeader("config-early-ack")
	req.AppendHeader(&callID)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 3, MethodName: sip.MESSAGE})
	tx := siptest.NewServerTxRecorder(req)
	processor := &blockingConfigProcessor{entered: make(chan struct{}), release: make(chan struct{})}
	handler := NewMessageHandler(gbconfig.Config{})
	handler.SetPTZProcessor(processor)
	done := make(chan struct{})
	go func() {
		handler.Handle(req, tx)
		close(done)
	}()
	defer func() {
		close(processor.release)
		<-done
	}()

	select {
	case <-processor.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("ConfigDownload 应答没有进入处理器")
	}
	require.Len(t, tx.Result(), 1, "处理器等待数据库时，SIP MESSAGE 应已收到应答")
	require.EqualValues(t, 200, tx.Result()[0].StatusCode)
}
