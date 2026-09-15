package handler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/metrics"
)

type blockingRecordInfoSink struct {
	mu      sync.Mutex
	sender  string
	body    []byte
	started chan struct{}
	release chan struct{}
	err     error
}

type playbackEndSink struct {
	started chan struct{}
	device  string
	callID  string
}

func (s *playbackEndSink) OnPlaybackFileToEnd(_ context.Context, callID, device string, _ []byte) error {
	s.callID, s.device = callID, device
	close(s.started)
	return nil
}

func (s *blockingRecordInfoSink) OnRecordInfoMessage(_ context.Context, sender string, body []byte) error {
	s.mu.Lock()
	s.sender = sender
	s.body = append([]byte(nil), body...)
	s.mu.Unlock()
	close(s.started)
	<-s.release
	return s.err
}

func recordInfoRequest(body []byte) *sip.Request {
	req := sip.NewRequest(sip.MESSAGE, sip.Uri{User: "platform", Host: "3402000000"})
	prepareNotifyRequest(req)
	req.SetBody(body)
	req.RemoveHeader("From")
	req.AppendHeader(&sip.FromHeader{Address: sip.Uri{User: "34020000001320000001", Host: "3402000000"}})
	callID := sip.CallIDHeader("record-info-message")
	req.AppendHeader(&callID)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 9, MethodName: sip.MESSAGE})
	return req
}

func TestTxKindFromCmdRecordInfo(t *testing.T) {
	require.Equal(t, metrics.TxRecord, txKindFromCmd(manscdp.CmdRecordInfo))
}

func TestMessageHandlerAcknowledgesRecordInfoBeforeSinkAndUsesFromIdentity(t *testing.T) {
	body := []byte(`<Response><CmdType>RecordInfo</CmdType><SN>7</SN><DeviceID>34020000001320000002</DeviceID><SumNum>0</SumNum></Response>`)
	req := recordInfoRequest(body)
	tx := siptest.NewServerTxRecorder(req)
	sink := &blockingRecordInfoSink{started: make(chan struct{}), release: make(chan struct{})}
	handler := NewMessageHandler(gbconfig.Config{})
	handler.SetRecordInfoSink(sink)
	done := make(chan struct{})
	go func() {
		handler.Handle(req, tx)
		close(done)
	}()

	select {
	case <-sink.started:
	case <-time.After(time.Second):
		t.Fatal("RecordInfo sink was not called")
	}
	require.Len(t, tx.Result(), 1, "SIP 200 must be sent before a blocking sink runs")
	require.EqualValues(t, 200, tx.Result()[0].StatusCode)
	sink.mu.Lock()
	require.Equal(t, "34020000001320000001", sink.sender)
	require.Equal(t, body, sink.body)
	sink.mu.Unlock()
	close(sink.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not return after sink release")
	}
	require.Len(t, tx.Result(), 1, "RecordInfo must be acknowledged exactly once")
}

func TestMessageHandlerRecordInfoSinkFailureAndMissingSinkStillAcknowledge(t *testing.T) {
	body := []byte(`<Response><CmdType>RecordInfo</CmdType><SN>7</SN><DeviceID>channel</DeviceID><SumNum>0</SumNum></Response>`)
	for _, test := range []struct {
		name string
		sink RecordInfoSink
	}{
		{name: "missing sink"},
		{name: "sink error", sink: &blockingRecordInfoSink{started: make(chan struct{}), release: closedSignal(), err: errors.New("closed runtime")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := recordInfoRequest(body)
			tx := siptest.NewServerTxRecorder(req)
			handler := NewMessageHandler(gbconfig.Config{})
			handler.SetRecordInfoSink(test.sink)
			handler.Handle(req, tx)
			require.Len(t, tx.Result(), 1)
			require.EqualValues(t, 200, tx.Result()[0].StatusCode)
		})
	}
}

func TestMalformedRecordInfoDoesNotReachSink(t *testing.T) {
	req := recordInfoRequest([]byte(`<Response><CmdType>RecordInfo`))
	tx := siptest.NewServerTxRecorder(req)
	sink := &blockingRecordInfoSink{started: make(chan struct{}), release: closedSignal()}
	handler := NewMessageHandler(gbconfig.Config{})
	handler.SetRecordInfoSink(sink)
	handler.Handle(req, tx)
	require.Len(t, tx.Result(), 1)
	select {
	case <-sink.started:
		t.Fatal("malformed XML must not reach RecordInfo sink")
	default:
	}
}

func TestMessageHandlerAcknowledgesPlaybackFileToEndBeforeFinalizer(t *testing.T) {
	req := recordInfoRequest([]byte(`<Notify><CmdType>MediaStatus</CmdType><DeviceID>34020000001320000002</DeviceID><Status>File to End</Status></Notify>`))
	tx := siptest.NewServerTxRecorder(req)
	sink := &playbackEndSink{started: make(chan struct{})}
	h := NewMessageHandler(gbconfig.Config{})
	h.SetPlaybackEndSink(sink)
	h.Handle(req, tx)
	require.Len(t, tx.Result(), 1)
	require.EqualValues(t, 200, tx.Result()[0].StatusCode)
	select {
	case <-sink.started:
	default:
		t.Fatal("playback end sink was not called")
	}
	require.Equal(t, "record-info-message", sink.callID)
	require.Equal(t, "34020000001320000001", sink.device)
}

func closedSignal() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
