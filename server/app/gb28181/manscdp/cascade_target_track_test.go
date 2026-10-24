package manscdp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

func TestParseCascadeTargetTrackControl(t *testing.T) {
	profile := protocol.ProfileFor(protocol.Version2022)
	for _, mode := range []TargetTrackMode{TargetTrackAuto, TargetTrackManual, TargetTrackStop} {
		t.Run(string(mode), func(t *testing.T) {
			command := TargetTrackCommand{Mode: mode, DeviceID2: targetTrackDomeCode}
			if mode == TargetTrackManual {
				a := manualTargetTrackArea()
				command.Area = &a
			}
			body, err := BuildTargetTrackControlWithProfile(profile, "34020000001320000012", 97, command)
			require.NoError(t, err)
			parsed, err := ParseTargetTrackControlWithProfile(profile, body)
			require.NoError(t, err)
			require.Equal(t, 97, parsed.SN)
			require.Equal(t, "34020000001320000012", parsed.DeviceID)
			require.Equal(t, command, parsed.Command)
		})
	}
}

func TestParseCascadeTargetTrackRejectsAmbiguity(t *testing.T) {
	body := `<Control><CmdType>DeviceControl</CmdType><SN>97</SN><DeviceID>34020000001320000012</DeviceID><TargetTrack>Auto</TargetTrack></Control>`
	for name, input := range map[string]string{
		"duplicate action":   strings.Replace(body, "</Control>", "<TargetTrack>Stop</TargetTrack></Control>", 1),
		"duplicate target":   strings.Replace(body, "</Control>", "<DeviceID>34020000001320000013</DeviceID></Control>", 1),
		"mixed command":      strings.Replace(body, "</Control>", "<TeleBoot>Boot</TeleBoot></Control>", 1),
		"private field":      strings.Replace(body, "</Control>", "<Password>secret</Password></Control>", 1),
		"nested scalar":      strings.Replace(body, "Auto", "<Item>Auto</Item>", 1),
		"invalid mode":       strings.Replace(body, "Auto", "Follow", 1),
		"manual missing box": strings.Replace(body, "Auto", "Manual", 1),
		"zero sequence":      strings.Replace(body, "97", "0", 1),
		"bad panorama":       strings.Replace(body, "</Control>", "<DeviceID2>source-only</DeviceID2></Control>", 1),
		"multiple roots":     body + body,
		"external text":      body + "unexpected",
		"namespace":          strings.Replace(body, "<Control>", `<Control xmlns="urn:private">`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseTargetTrackControlWithProfile(protocol.ProfileFor(protocol.Version2022), []byte(input))
			require.Error(t, err)
		})
	}
	_, err := ParseTargetTrackControlWithProfile(protocol.ProfileFor(protocol.Version2016), []byte(body))
	require.Error(t, err)
	area := manualTargetTrackArea()
	manual, err := BuildTargetTrackControlWithProfile(protocol.ProfileFor(protocol.Version2022), "34020000001320000012", 97, TargetTrackCommand{Mode: TargetTrackManual, Area: &area})
	require.NoError(t, err)
	for name, input := range map[string]string{
		"missing area coordinate":   strings.Replace(string(manual), "<MidPointX>960</MidPointX>", "", 1),
		"duplicate area coordinate": strings.Replace(string(manual), "</TargetArea>", "<MidPointX>0</MidPointX></TargetArea>", 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseTargetTrackControlWithProfile(protocol.ProfileFor(protocol.Version2022), []byte(input))
			require.Error(t, err)
		})
	}
}
