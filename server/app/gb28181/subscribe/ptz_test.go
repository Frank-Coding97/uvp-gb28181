package subscribe

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.cn/uvp-gb28181/app/gb28181/models"
)

type ptzNotificationSinkRecorder struct {
	deviceCode string
	callID     string
	cseq       string
	body       []byte
}

func (r *ptzNotificationSinkRecorder) OnPTZNotify(_ context.Context, deviceCode, callID, cseq string, body []byte) error {
	r.deviceCode = deviceCode
	r.callID = callID
	r.cseq = cseq
	r.body = append([]byte(nil), body...)
	return nil
}

func TestPTZProcessor_UsesValidatedSubscriptionDevice(t *testing.T) {
	sink := &ptzNotificationSinkRecorder{}
	processor := NewPTZProcessor(sink)
	body := []byte(`<Notify><CmdType>PTZPosition</CmdType><SN>3</SN><DeviceID>C</DeviceID></Notify>`)

	err := processor.Process(context.Background(), &gbmodels.GbDevice{DeviceID: "D"}, Notification{
		Kind: gbmodels.SubscriptionKindPTZPrecisePosition, DeviceCode: "C",
		CallID: "ptz-call", CSeq: "2 NOTIFY", Body: body,
	})

	require.NoError(t, err)
	require.Equal(t, "D", sink.deviceCode)
	require.Equal(t, "ptz-call", sink.callID)
	require.Equal(t, "2 NOTIFY", sink.cseq)
	require.Equal(t, body, sink.body)
}
