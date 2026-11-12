package manscdp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

const cascadeBroadcastNotifyXML = `<Notify><CmdType>Broadcast</CmdType><SN>97</SN><SourceID>34020000002000000001</SourceID><TargetID>34020000001320000012</TargetID></Notify>`

func TestCascadeBroadcastNotifyProfiles(t *testing.T) {
	for _, version := range []protocol.Version{protocol.Version2016, protocol.Version2022} {
		t.Run(string(version), func(t *testing.T) {
			profile := protocol.ProfileFor(version)
			notify, err := ParseCascadeBroadcastNotify(profile, []byte(cascadeBroadcastNotifyXML))
			require.NoError(t, err)
			require.Equal(t, BroadcastNotify{SN: 97, SourceID: "34020000002000000001", TargetID: "34020000001320000012"}, notify)
			body, err := BuildBroadcastResponseWithProfile(profile, 97, "34020000002000000002", "OK")
			require.NoError(t, err)
			ack, err := ParseBroadcastResponse(body)
			require.NoError(t, err)
			require.Equal(t, 97, ack.SN)
			require.Equal(t, "34020000002000000002", ack.DeviceID)
			require.Equal(t, "OK", ack.Result)
			require.Empty(t, ack.TargetID)
			require.NotContains(t, string(body), "<Info>")
			require.Contains(t, string(body), `encoding="`+string(profile.Charset)+`"`)
		})
	}
}

func TestCascadeBroadcastNotifyRejectsAmbiguousDocuments(t *testing.T) {
	mutations := map[string]string{
		"duplicate source":  strings.Replace(cascadeBroadcastNotifyXML, "</Notify>", "<SourceID>34020000002000000099</SourceID></Notify>", 1),
		"duplicate target":  strings.Replace(cascadeBroadcastNotifyXML, "</Notify>", "<TargetID>34020000001320000099</TargetID></Notify>", 1),
		"duplicate serial":  strings.Replace(cascadeBroadcastNotifyXML, "</Notify>", "<SN>98</SN></Notify>", 1),
		"duplicate command": strings.Replace(cascadeBroadcastNotifyXML, "</Notify>", "<CmdType>DeviceControl</CmdType></Notify>", 1),
		"missing target":    strings.Replace(cascadeBroadcastNotifyXML, "<TargetID>34020000001320000012</TargetID>", "", 1),
		"nested target":     strings.Replace(cascadeBroadcastNotifyXML, "<TargetID>34020000001320000012</TargetID>", "<TargetID><TargetID>34020000001320000012</TargetID></TargetID>", 1),
		"mixed control":     strings.Replace(cascadeBroadcastNotifyXML, "</Notify>", "<PTZCmd>A50F0100000000B5</PTZCmd></Notify>", 1),
		"private field":     strings.Replace(cascadeBroadcastNotifyXML, "</Notify>", "<Password>secret</Password></Notify>", 1),
		"second root":       cascadeBroadcastNotifyXML + cascadeBroadcastNotifyXML,
		"leading text":      "extra" + cascadeBroadcastNotifyXML,
		"trailing text":     cascadeBroadcastNotifyXML + "extra",
		"namespace":         strings.Replace(cascadeBroadcastNotifyXML, "<Notify>", `<Notify xmlns="urn:vendor">`, 1),
		"attribute":         strings.Replace(cascadeBroadcastNotifyXML, "<SN>", `<SN value="1">`, 1),
		"directive":         "<!DOCTYPE Notify>" + cascadeBroadcastNotifyXML,
		"wrong root":        strings.ReplaceAll(cascadeBroadcastNotifyXML, "Notify", "Control"),
		"wrong command":     strings.Replace(cascadeBroadcastNotifyXML, "Broadcast", "DeviceControl", 1),
		"zero serial":       strings.Replace(cascadeBroadcastNotifyXML, "<SN>97</SN>", "<SN>0</SN>", 1),
		"short source":      strings.Replace(cascadeBroadcastNotifyXML, "34020000002000000001", "340200", 1),
		"nondecimal target": strings.Replace(cascadeBroadcastNotifyXML, "34020000001320000012", "3402000000132000001x", 1),
	}
	for name, body := range mutations {
		t.Run(name, func(t *testing.T) {
			_, err := ParseCascadeBroadcastNotify(protocol.ProfileFor(protocol.Version2022), []byte(body))
			require.Error(t, err)
		})
	}
}

func TestCascadeBroadcastResponseRejectsInvalidCorrelation(t *testing.T) {
	for _, input := range []struct {
		sn             int
		device, result string
	}{
		{0, "34020000002000000002", "OK"},
		{1, "source", "OK"},
		{1, "34020000002000000002", "SUCCESS"},
		{1, "34020000002000000002", ""},
	} {
		_, err := BuildBroadcastResponseWithProfile(protocol.ProfileFor(protocol.Version2022), input.sn, input.device, input.result)
		require.Error(t, err)
	}
	_, err := BuildBroadcastResponseWithProfile(protocol.ProfileFor(protocol.Version2022), 1, "34020000002000000002", "ERROR")
	require.NoError(t, err)
}
