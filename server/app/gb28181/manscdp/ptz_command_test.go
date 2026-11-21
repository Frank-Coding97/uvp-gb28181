package manscdp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

func TestParsePTZCommandAcceptsStandardPTZAndFI(t *testing.T) {
	tests := []struct {
		raw    string
		action string
	}{
		{raw: "A50F0100000000B5", action: "stop"},
		{raw: "A50F010A080800CF", action: "left_up"},
		{raw: "A50F0110000010D5", action: "zoom_in"},
		{raw: "A50F0142080000FF", action: "focus_near"},
		{raw: "A50F014800080005", action: "iris_close"},
		{raw: "A50F0140000000F5", action: "fi_stop"},
		{raw: "A50F0119080810EE", action: "ptz_combined"},
		{raw: "A50F01490808000E", action: "fi_combined"},
		{raw: "A50F0000000000B4", action: "stop"},
	}
	for _, test := range tests {
		t.Run(test.action, func(t *testing.T) {
			command, err := ParsePTZCommand(test.raw)
			require.NoError(t, err)
			require.Equal(t, test.action, command.Action)
			require.Equal(t, test.raw, command.Raw)
		})
	}
}

func TestParsePTZCommandRejectsUnsupportedAndInvalid(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		kind error
	}{
		{name: "reserved instruction", raw: "A50F018B00000040", kind: ErrUnsupportedPTZInstruction},
		{name: "conflicting pan", raw: "A50F0103080800C8", kind: ErrInvalidPTZCommand},
		{name: "bad checksum", raw: "A50F0100000000B6", kind: ErrInvalidPTZCommand},
		{name: "short", raw: "A50F", kind: ErrInvalidPTZCommand},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParsePTZCommand(test.raw)
			require.ErrorIs(t, err, test.kind)
		})
	}
}

func TestBuildRawPTZControlWithProfilePreservesCommandAndUsesSourceTarget(t *testing.T) {
	for _, version := range []protocol.Version{protocol.Version2016, protocol.Version2022} {
		profile := protocol.ProfileFor(version)
		body, err := BuildRawPTZControlWithProfile(profile, "source-channel", 99, "A50F0142080000FF")
		require.NoError(t, err)
		text := string(body)
		require.Contains(t, text, "<SN>99</SN>")
		require.Contains(t, text, "<DeviceID>source-channel</DeviceID>")
		require.Contains(t, text, "<PTZCmd>A50F0142080000FF</PTZCmd>")
		require.True(t, strings.Contains(text, string(profile.Charset)))
	}
}

func TestBuildRawPTZControlWithProfilePreservesHexCasing(t *testing.T) {
	body, err := BuildRawPTZControlWithProfile(
		protocol.ProfileFor(protocol.Version2016), "source-channel", 99, "a50f0142080000ff",
	)
	require.NoError(t, err)
	require.Contains(t, string(body), "<PTZCmd>a50f0142080000ff</PTZCmd>")
}

func TestParsePTZCommandAcceptsExtendedInstructions(t *testing.T) {
	for _, tc := range []struct{ raw, action string }{
		{"A50F018100030039", "preset_set"},
		{"A50F01820003003A", "preset_call"},
		{"A50F01830003003B", "preset_delete"},
		{"A50F01840203003E", "cruise_add_stop"},
		{"A50F01850203003F", "cruise_delete_stop"},
		{"A50F01850200003C", "cruise_delete_path"},
		{"A50F01860201104E", "cruise_set_speed"},
		{"A50F018702031051", "cruise_set_dwell"},
		{"A50F01880200003F", "cruise_start"},
		{"A50F018902000040", "scan_start"},
		{"A50F018902010041", "scan_set_left"},
		{"A50F018902020042", "scan_set_right"},
		{"A50F018A0201F032", "scan_set_speed"},
		{"A50F018C01000042", "aux_on"},
		{"A50F018D01000043", "aux_off"},
	} {
		t.Run(tc.action, func(t *testing.T) {
			parsed, err := ParsePTZCommand(tc.raw)
			require.NoError(t, err)
			require.Equal(t, tc.action, parsed.Action)
			require.Equal(t, tc.raw, parsed.Raw)
		})
	}
}

func TestParsePTZCommandRejectsInvalidExtendedParameters(t *testing.T) {
	for _, raw := range []string{
		"A50F018100000036", // preset zero is reserved
		"A50F01810103003A", // preset byte 5 must be zero
		"A50F01840200003B", // cruise add requires a preset
		"A50F01860200003D", // speed zero
		"A50F018802010040", // cruise start byte 6 must be zero
		"A50F018902030043", // no scan sub-action 3
		"A50F018A02000041", // scan speed zero
	} {
		_, err := ParsePTZCommand(raw)
		require.ErrorIs(t, err, ErrInvalidPTZCommand, raw)
	}
}

func TestPTZControlRejectsInvalidCommandParameters(t *testing.T) {
	for _, tc := range []struct{ raw, params string }{
		{"A50F01820003003A", "<PresetName>not a preset set</PresetName>"},
		{"A50F018100030039", "<CruiseTrackName>not a cruise</CruiseTrackName>"},
		{"A50F01880200003F", "<CruiseTrackName>" + strings.Repeat("a", 33) + "</CruiseTrackName>"},
	} {
		body := []byte(`<Control><CmdType>DeviceControl</CmdType><SN>1</SN><DeviceID>channel</DeviceID><PTZCmd>` + tc.raw + `</PTZCmd><PTZCmdParams>` + tc.params + `</PTZCmdParams></Control>`)
		_, err := ParsePTZControlWithProfile(protocol.ProfileFor(protocol.Version2022), body)
		require.ErrorIs(t, err, ErrInvalidPTZCommand)
	}
}

func TestPTZCruiseNameLimitUsesWireBytes(t *testing.T) {
	profile := protocol.ProfileFor(protocol.Version2022)
	for _, length := range []int{16, 17} {
		name := strings.Repeat("路", length)
		body, err := MarshalProfiledXML(profile, ptzControlXML{
			CmdType: CmdDeviceControl, SN: 1, DeviceID: "channel", PTZCmd: "A50F01880200003F",
			Params: &PTZCommandParams{CruiseTrackName: name},
		})
		require.NoError(t, err)
		parsed, err := ParsePTZControlWithProfile(profile, body)
		if length == 16 {
			require.NoError(t, err)
			require.Equal(t, name, parsed.Params.CruiseTrackName)
		} else {
			require.ErrorIs(t, err, ErrInvalidPTZCommand)
		}
	}
}
