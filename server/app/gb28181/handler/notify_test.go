package handler

import (
	"context"
	"testing"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
	"uvplatform.cn/uvp-gb28181/app/gb28181/subscribe"
)

type notifyRecorder struct{ values []subscribe.Notification }

func (r *notifyRecorder) OnNotify(_ context.Context, value subscribe.Notification) error {
	r.values = append(r.values, value)
	return nil
}

func prepareNotifyRequest(req *sip.Request) {
	viaParams := sip.NewParams()
	viaParams.Add("branch", "z9hG4bK-test")
	fromParams := sip.NewParams()
	fromParams.Add("tag", "device-tag")
	req.AppendHeader(&sip.ViaHeader{ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: "UDP", Host: "127.0.0.1", Port: 5060, Params: viaParams})
	req.AppendHeader(&sip.FromHeader{Address: sip.Uri{User: "device", Host: "3402000000"}, Params: fromParams})
	req.AppendHeader(&sip.ToHeader{Address: sip.Uri{User: "platform", Host: "3402000000"}, Params: sip.NewParams()})
}

func TestNotifyHandler_AcknowledgesAndDispatches(t *testing.T) {
	req := sip.NewRequest(sip.NOTIFY, sip.Uri{User: "D", Host: "3402000000"})
	prepareNotifyRequest(req)
	req.SetBody([]byte(`<Notify><CmdType>Alarm</CmdType><SN>1</SN><DeviceID>D</DeviceID></Notify>`))
	req.AppendHeader(sip.NewHeader("Event", "presence"))
	callID := sip.CallIDHeader("notify-call")
	req.AppendHeader(&callID)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 1, MethodName: sip.NOTIFY})
	tx := siptest.NewServerTxRecorder(req)
	recorder := &notifyRecorder{}
	NewNotifyHandler(recorder).Handle(req, tx)

	require.Len(t, recorder.values, 1)
	require.Equal(t, gbmodels.SubscriptionKindAlarm, recorder.values[0].Kind)
	require.Equal(t, "notify-call", recorder.values[0].CallID)
	require.Len(t, tx.Result(), 1)
	require.EqualValues(t, 200, tx.Result()[0].StatusCode)
}

func TestNotifyHandler_MalformedStillAcknowledged(t *testing.T) {
	req := sip.NewRequest(sip.NOTIFY, sip.Uri{User: "D", Host: "3402000000"})
	prepareNotifyRequest(req)
	req.SetBody([]byte(`<Notify>`))
	req.AppendHeader(sip.NewHeader("Event", "presence"))
	callID := sip.CallIDHeader("notify-call")
	req.AppendHeader(&callID)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 1, MethodName: sip.NOTIFY})
	tx := siptest.NewServerTxRecorder(req)
	recorder := &notifyRecorder{}
	NewNotifyHandler(recorder).Handle(req, tx)
	require.Empty(t, recorder.values)
	require.Len(t, tx.Result(), 1)
	require.EqualValues(t, 200, tx.Result()[0].StatusCode)
}

func TestNotifyHandler_PTZPreciseDispatchesAndAcknowledges(t *testing.T) {
	req := sip.NewRequest(sip.NOTIFY, sip.Uri{User: "D", Host: "3402000000"})
	prepareNotifyRequest(req)
	req.SetBody([]byte(`<Notify><CmdType>PTZPrecisePosition</CmdType><SN>3</SN><DeviceID>C</DeviceID><Pan>12.5</Pan></Notify>`))
	req.AppendHeader(sip.NewHeader("Event", "PTZPrecisePosition"))
	callID := sip.CallIDHeader("ptz-notify")
	req.AppendHeader(&callID)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 2, MethodName: sip.NOTIFY})
	tx := siptest.NewServerTxRecorder(req)
	notifier := &notifyRecorder{}
	h := NewNotifyHandler(notifier)
	h.Handle(req, tx)
	require.Len(t, notifier.values, 1)
	require.Equal(t, gbmodels.SubscriptionKindPTZPrecisePosition, notifier.values[0].Kind)
	require.Equal(t, "C", notifier.values[0].DeviceCode)
	require.Equal(t, "ptz-notify", notifier.values[0].CallID)
	require.Len(t, tx.Result(), 1)
	require.EqualValues(t, 200, tx.Result()[0].StatusCode)
}

func TestNotifyHandler_PTZPositionDispatchesAndAcknowledges(t *testing.T) {
	req := sip.NewRequest(sip.NOTIFY, sip.Uri{User: "D", Host: "3402000000"})
	prepareNotifyRequest(req)
	req.SetBody([]byte("<Notify><CmdType>PTZPosition</CmdType><SN>3</SN><DeviceID>C</DeviceID><Pan>12.5</Pan></Notify>"))
	req.AppendHeader(sip.NewHeader("Event", "PTZPosition"))
	callID := sip.CallIDHeader("ptz-position-notify")
	req.AppendHeader(&callID)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 2, MethodName: sip.NOTIFY})
	tx := siptest.NewServerTxRecorder(req)
	notifier := &notifyRecorder{}
	h := NewNotifyHandler(notifier)
	h.Handle(req, tx)
	require.Len(t, notifier.values, 1)
	require.Equal(t, gbmodels.SubscriptionKindPTZPrecisePosition, notifier.values[0].Kind)
	require.Equal(t, "C", notifier.values[0].DeviceCode)
	require.Equal(t, "ptz-position-notify", notifier.values[0].CallID)
	require.Len(t, tx.Result(), 1)
	require.EqualValues(t, 200, tx.Result()[0].StatusCode)
}
