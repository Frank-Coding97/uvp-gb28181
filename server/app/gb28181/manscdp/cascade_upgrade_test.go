package manscdp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

func TestCascadeUpgradeControlParseAndRebuild(t *testing.T) {
	profile := protocol.ProfileFor(protocol.Version2022)
	command := DeviceUpgradeCommand{Firmware: "v2.3.4", FileURL: "https://firmware.example/v2.bin?token=opaque", Manufacturer: "制造商", SessionID: strings.Repeat("a", 32)}
	body, err := BuildDeviceUpgradeControlWithProfile(profile, "34020000001320000011", 97, command)
	require.NoError(t, err)
	parsed, err := ParseDeviceUpgradeControlWithProfile(profile, body)
	require.NoError(t, err)
	require.Equal(t, 97, parsed.SN)
	require.Equal(t, "34020000001320000011", parsed.DeviceID)
	require.Equal(t, command, parsed.Command)
	rebuilt, err := BuildDeviceUpgradeControlWithProfile(profile, "34020000001320000001", 101, parsed.Command)
	require.NoError(t, err)
	downstream, err := ParseDeviceUpgradeControlWithProfile(profile, rebuilt)
	require.NoError(t, err)
	require.Equal(t, "34020000001320000001", downstream.DeviceID)
	require.Equal(t, 101, downstream.SN)
	require.Equal(t, command, downstream.Command)
	_, err = ParseDeviceUpgradeControlWithProfile(protocol.ProfileFor(protocol.Version2016), body)
	require.Error(t, err)
}

func TestCascadeUpgradeControlRejectsAmbiguousOrInvalidCommands(t *testing.T) {
	profile := protocol.ProfileFor(protocol.Version2022)
	valid := `<Control><CmdType>DeviceControl</CmdType><SN>97</SN><DeviceID>34020000001320000011</DeviceID><DeviceUpgrade><Firmware>v2</Firmware><FileURL>https://example.test/v2.bin</FileURL><Manufacturer>UVP</Manufacturer><SessionID>` + strings.Repeat("a", 32) + `</SessionID></DeviceUpgrade></Control>`
	for _, test := range []struct{ name, body string }{
		{"wrong root", strings.ReplaceAll(valid, "Control>", "Notify>")},
		{"duplicate SN", strings.Replace(valid, "<SN>97</SN>", "<SN>97</SN><SN>98</SN>", 1)},
		{"duplicate block", strings.Replace(valid, "</Control>", "<DeviceUpgrade/></Control>", 1)},
		{"duplicate firmware", strings.Replace(valid, "<Firmware>v2</Firmware>", "<Firmware>v1</Firmware><Firmware>v2</Firmware>", 1)},
		{"mixed action", strings.Replace(valid, "</Control>", "<TeleBoot>Boot</TeleBoot></Control>", 1)},
		{"nested value", strings.Replace(valid, "<Firmware>v2</Firmware>", "<Firmware><Value>v2</Value></Firmware>", 1)},
		{"missing manufacturer", strings.Replace(valid, "<Manufacturer>UVP</Manufacturer>", "", 1)},
		{"zero SN", strings.Replace(valid, "<SN>97</SN>", "<SN>0</SN>", 1)},
		{"wrong command", strings.Replace(valid, "DeviceControl", "DeviceConfig", 1)},
		{"short session", strings.Replace(valid, strings.Repeat("a", 32), "short", 1)},
		{"URL credentials", strings.Replace(valid, "https://example.test/v2.bin", "https://user:secret@example.test/v2.bin", 1)},
		{"URL scheme", strings.Replace(valid, "https://example.test/v2.bin", "file:///tmp/firmware.bin", 1)},
		{"oversize firmware", strings.Replace(valid, "<Firmware>v2</Firmware>", "<Firmware>"+strings.Repeat("v", 256)+"</Firmware>", 1)},
		{"multiple roots", valid + valid},
		{"trailing text", valid + "garbage"},
		{"namespace", strings.Replace(valid, "<Control>", `<Control xmlns="urn:other">`, 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseDeviceUpgradeControlWithProfile(profile, []byte(test.body))
			require.Error(t, err)
		})
	}
}

func TestCascadeUpgradeResultRebuildsPublishedIdentity(t *testing.T) {
	profile := protocol.ProfileFor(protocol.Version2022)
	for _, result := range []string{"OK", "ERROR"} {
		t.Run(result, func(t *testing.T) {
			notification := DeviceUpgradeResultNotify{SN: 901, DeviceID: "34020000001320000011", SessionID: strings.Repeat("b", 32), UpgradeResult: result, Firmware: "v2"}
			if result == "ERROR" {
				notification.UpgradeFailedReason = "02"
			}
			body, err := BuildDeviceUpgradeResultWithProfile(profile, notification)
			require.NoError(t, err)
			parsed, err := ParseDeviceUpgradeResultWithProfile(profile, body)
			require.NoError(t, err)
			require.Equal(t, notification.SN, parsed.SN)
			require.Equal(t, notification.DeviceID, parsed.DeviceID)
			require.Equal(t, notification.SessionID, parsed.SessionID)
			require.Equal(t, notification.UpgradeResult, parsed.UpgradeResult)
			require.Equal(t, notification.UpgradeFailedReason, parsed.UpgradeFailedReason)
			if result == "OK" {
				require.NotContains(t, string(body), "UpgradeFailedReason")
			}
			_, err = BuildDeviceUpgradeResultWithProfile(protocol.ProfileFor(protocol.Version2016), notification)
			require.Error(t, err)
		})
	}
}

func TestCascadeUpgradeResultRejectsDuplicateAndTrailingFields(t *testing.T) {
	valid := `<Notify><CmdType>DeviceUpgradeResult</CmdType><SN>17</SN><DeviceID>D</DeviceID><SessionID>` + strings.Repeat("a", 32) + `</SessionID><UpgradeResult>ERROR</UpgradeResult><Firmware>v2</Firmware><UpgradeFailedReason>02</UpgradeFailedReason></Notify>`
	for _, body := range []string{
		strings.Replace(valid, "<UpgradeResult>ERROR</UpgradeResult>", "<UpgradeResult>OK</UpgradeResult><UpgradeResult>ERROR</UpgradeResult>", 1),
		strings.Replace(valid, "</Notify>", "<SessionID>"+strings.Repeat("b", 32)+"</SessionID></Notify>", 1),
		valid + valid,
		valid + "garbage",
	} {
		_, err := ParseDeviceUpgradeResult([]byte(body))
		require.Error(t, err)
	}
}
