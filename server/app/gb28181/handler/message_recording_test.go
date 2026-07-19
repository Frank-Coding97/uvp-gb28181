package handler

import (
	"context"
	"testing"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"

	gbconfig "uvplatform.cn/uvp-gb28181/app/gb28181/config"
	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
)

type recordInfoRecorder struct{ values []*manscdp.RecordInfoResponse }

func (r *recordInfoRecorder) OnRecordInfo(_ context.Context, value *manscdp.RecordInfoResponse) error {
	r.values = append(r.values, value)
	return nil
}

func TestMessageHandler_DispatchesRecordInfo(t *testing.T) {
	req := sip.NewRequest(sip.MESSAGE, sip.Uri{User: "platform", Host: "3402000000"})
	prepareNotifyRequest(req)
	callID := sip.CallIDHeader("record-info-call")
	req.AppendHeader(&callID)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 1, MethodName: sip.MESSAGE})
	req.SetBody([]byte(`<Response><CmdType>RecordInfo</CmdType><SN>8</SN><DeviceID>34020000002000000001</DeviceID><SumNum>1</SumNum><RecordList Num="1"><Item><DeviceID>34020000001320000001</DeviceID><StartTime>2026-07-19T08:00:00</StartTime><EndTime>2026-07-19T08:10:00</EndTime></Item></RecordList></Response>`))
	tx := siptest.NewServerTxRecorder(req)
	recorder := &recordInfoRecorder{}
	h := NewMessageHandler(gbconfig.Config{})
	h.SetRecordInfoSink(recorder)
	h.Handle(req, tx)

	require.Len(t, recorder.values, 1)
	require.Equal(t, 8, recorder.values[0].SN)
	require.Len(t, recorder.values[0].Records, 1)
	require.Len(t, tx.Result(), 1)
	require.EqualValues(t, 200, tx.Result()[0].StatusCode)
}

func TestMessageHandler_MalformedRecordInfoIsAcknowledged(t *testing.T) {
	req := sip.NewRequest(sip.MESSAGE, sip.Uri{User: "platform", Host: "3402000000"})
	prepareNotifyRequest(req)
	callID := sip.CallIDHeader("record-info-malformed-call")
	req.AppendHeader(&callID)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 1, MethodName: sip.MESSAGE})
	req.SetBody([]byte(`<Response><CmdType>RecordInfo</CmdType>`))
	tx := siptest.NewServerTxRecorder(req)
	recorder := &recordInfoRecorder{}
	h := NewMessageHandler(gbconfig.Config{})
	h.SetRecordInfoSink(recorder)
	h.Handle(req, tx)

	require.Empty(t, recorder.values)
	require.Len(t, tx.Result(), 1)
	require.EqualValues(t, 200, tx.Result()[0].StatusCode)
}
