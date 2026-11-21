package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/emiago/sipgo/sip"
	"github.com/emiago/sipgo/siptest"
	"github.com/stretchr/testify/require"
	gbconfig "uvplatform.com/uvp-gb28181/app/gb28181/config"
	"uvplatform.com/uvp-gb28181/app/gb28181/subscribe"
)

type alarmAcceptanceRecorder struct {
	err    error
	device string
}

func (r *alarmAcceptanceRecorder) OnAlarmMessage(_ context.Context, device, _, _ string, _ []byte) error {
	r.device = device
	return r.err
}
func (r *alarmAcceptanceRecorder) OnNotify(context.Context, subscribe.Notification) error {
	return r.err
}

func TestAlarmAcknowledgmentRequiresDurableAcceptance(t *testing.T) {
	for _, method := range []sip.RequestMethod{sip.MESSAGE, sip.NOTIFY} {
		for _, accepted := range []bool{false, true} {
			t.Run(string(method)+map[bool]string{false: "/rejected", true: "/accepted"}[accepted], func(t *testing.T) {
				req := sip.NewRequest(method, sip.Uri{User: "platform", Host: "3402000000"})
				prepareNotifyRequest(req)
				req.SetBody([]byte(`<Notify><CmdType>Alarm</CmdType><SN>7</SN><DeviceID>channel</DeviceID></Notify>`))
				req.AppendHeader(sip.NewHeader("Event", "Alarm"))
				call := sip.CallIDHeader("alarm-acceptance")
				req.AppendHeader(&call)
				req.AppendHeader(&sip.CSeqHeader{SeqNo: 1, MethodName: method})
				processor := &alarmAcceptanceRecorder{}
				if !accepted {
					processor.err = errors.Join(subscribe.ErrAlarmNotAccepted, errors.New("database unavailable"))
				}
				tx := siptest.NewServerTxRecorder(req)
				defer tx.Terminate()
				if method == sip.MESSAGE {
					h := NewMessageHandler(gbconfig.Config{})
					h.SetAlarmProcessor(processor)
					h.Handle(req, tx)
					require.Equal(t, "device", processor.device, "MESSAGE source is From, while XML DeviceID may identify a channel")
				} else {
					NewNotifyHandler(processor).Handle(req, tx)
				}
				require.Len(t, tx.Result(), 1)
				expected := 503
				if accepted {
					expected = 200
				}
				require.Equal(t, expected, tx.Result()[0].StatusCode)
			})
		}
	}
}
