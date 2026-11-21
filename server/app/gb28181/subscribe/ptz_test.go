package subscribe

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	gbmodels "uvplatform.com/uvp-gb28181/app/gb28181/models"
)

type ptzNotificationSinkRecorder struct {
	deviceCode string
	callID     string
	cseq       string
	body       []byte
	err        error
}

func (r *ptzNotificationSinkRecorder) OnPTZNotify(_ context.Context, deviceCode, callID, cseq string, body []byte) error {
	r.deviceCode = deviceCode
	r.callID = callID
	r.cseq = cseq
	r.body = append([]byte(nil), body...)
	return r.err
}

type ptzNotificationRelayRecorder struct {
	calls      int
	deviceCode string
	callID     string
	cseq       string
	body       []byte
}

func (r *ptzNotificationRelayRecorder) RelayPTZNotify(_ context.Context, deviceCode, callID, cseq string, body []byte) {
	r.calls++
	r.deviceCode, r.callID, r.cseq = deviceCode, callID, cseq
	r.body = append([]byte(nil), body...)
}

func TestPTZProcessor_UsesValidatedSubscriptionDevice(t *testing.T) {
	sink := &ptzNotificationSinkRecorder{}
	relay := &ptzNotificationRelayRecorder{}
	processor := NewPTZProcessor(sink)
	processor.SetRelay(relay)
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
	require.Equal(t, 1, relay.calls)
	require.Equal(t, "D", relay.deviceCode)
	require.Equal(t, "ptz-call", relay.callID)
	require.Equal(t, "2 NOTIFY", relay.cseq)
	require.Equal(t, body, relay.body)
}

func TestPTZProcessor_DoesNotRelayWhenSinkRejectsNotification(t *testing.T) {
	sink := &ptzNotificationSinkRecorder{err: errors.New("sink rejected notification")}
	relay := &ptzNotificationRelayRecorder{}
	processor := NewPTZProcessor(sink)
	processor.SetRelay(relay)

	err := processor.Process(context.Background(), &gbmodels.GbDevice{DeviceID: "D"}, Notification{Body: []byte(`<Notify/>`)})

	require.Error(t, err)
	require.Zero(t, relay.calls)
}
