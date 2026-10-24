package manscdp

import (
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

func TestMediaStatusFileEndUsesStandardEnvelope(t *testing.T) {
	for _, version := range []protocol.Version{protocol.Version2016, protocol.Version2022} {
		body, err := BuildMediaStatusFileEnd(protocol.ProfileFor(version), "34020000001320000012", 9)
		require.NoError(t, err)
		notify, err := ParseMediaStatusNotify(body)
		require.NoError(t, err)
		require.Equal(t, "34020000001320000012", notify.DeviceID)
		require.Equal(t, 9, notify.SN)
		require.Equal(t, "121", notify.NotifyType)
	}
	for _, body := range []string{
		`<Notify><CmdType>Alarm</CmdType><SN>1</SN><DeviceID>C</DeviceID><NotifyType>121</NotifyType></Notify>`,
		`<Response><CmdType>MediaStatus</CmdType><SN>1</SN><DeviceID>C</DeviceID><NotifyType>121</NotifyType></Response>`,
		`<Notify><CmdType>MediaStatus</CmdType><SN>0</SN><DeviceID>C</DeviceID><NotifyType>121</NotifyType></Notify>`,
		`<Notify><CmdType>MediaStatus</CmdType><SN>1</SN><DeviceID>C</DeviceID></Notify>`,
	} {
		_, err := ParseMediaStatusNotify([]byte(body))
		require.Error(t, err)
	}
}
