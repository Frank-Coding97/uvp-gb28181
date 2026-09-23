package manscdp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildBroadcastNotifyAndParseResponse(t *testing.T) {
	body, err := BuildBroadcastNotify(BroadcastNotify{SN: 17, SourceID: "34020000002000000001", TargetID: "34020000001320000002"})
	require.NoError(t, err)
	text := string(body)
	require.Contains(t, text, "<CmdType>Broadcast</CmdType>")
	require.Contains(t, text, "<SN>17</SN>")
	require.Contains(t, text, "<SourceID>34020000002000000001</SourceID>")
	require.Contains(t, text, "<TargetID>34020000001320000002</TargetID>")

	response, err := ParseBroadcastResponse([]byte(`<?xml version="1.0"?><Response><CmdType>Broadcast</CmdType><SN>17</SN><DeviceID>34020000001320000001</DeviceID><TargetID>34020000001320000002</TargetID><Result>OK</Result><Info>accepted</Info></Response>`))
	require.NoError(t, err)
	require.Equal(t, 17, response.SN)
	require.Equal(t, "34020000001320000001", response.DeviceID)
	require.Equal(t, "34020000001320000002", response.TargetID)
	require.True(t, response.Success())

	_, err = ParseBroadcastResponse([]byte(strings.ReplaceAll(string(body), "<Notify>", "<Response>")))
	require.Error(t, err)
}

func TestBroadcastRejectsMissingCorrelation(t *testing.T) {
	_, err := BuildBroadcastNotify(BroadcastNotify{SN: 1, SourceID: "", TargetID: "target"})
	require.Error(t, err)
	_, err = ParseBroadcastResponse([]byte(`<Response><CmdType>Broadcast</CmdType><SN>0</SN><DeviceID>D</DeviceID><Result>OK</Result></Response>`))
	require.Error(t, err)
}
