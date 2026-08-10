package manscdp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
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
		{name: "preset", raw: "A50F018100030039", kind: ErrUnsupportedPTZInstruction},
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
