package manscdp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

func TestBuildDeviceUpgradeControl2022(t *testing.T) {
	profile := protocol.ProfileFor(protocol.Version2022)
	body, err := BuildDeviceUpgradeControlWithProfile(profile, "34020000001320000001", 17, DeviceUpgradeCommand{
		Firmware:     "v2.3.4",
		FileURL:      "https://fixture.example/firmware-v2.3.4.bin?token=redacted",
		Manufacturer: "UVP",
		SessionID:    "0123456789abcdef0123456789abcdef-20220905",
	})
	require.NoError(t, err)
	text := string(body)
	require.Contains(t, text, "<CmdType>DeviceControl</CmdType>")
	require.Contains(t, text, "<DeviceUpgrade><Firmware>v2.3.4</Firmware><FileURL>https://fixture.example/firmware-v2.3.4.bin?token=redacted</FileURL><Manufacturer>UVP</Manufacturer><SessionID>0123456789abcdef0123456789abcdef-20220905</SessionID></DeviceUpgrade>")
	require.Contains(t, text, "encoding=\"GB18030\"")
}

func TestBuildDeviceUpgradeControlRejectsInvalidSession(t *testing.T) {
	_, err := BuildDeviceUpgradeControlWithProfile(protocol.ProfileFor(protocol.Version2022), "D", 1, DeviceUpgradeCommand{
		Firmware: "v2", FileURL: "https://example.com/v2", Manufacturer: "M", SessionID: "short",
	})
	require.Error(t, err)
}

func TestParseDeviceUpgradeResult(t *testing.T) {
	body := []byte(`<Notify><CmdType>DeviceUpgradeResult</CmdType><SN>17</SN><DeviceID>D</DeviceID><SessionID>0123456789abcdef0123456789abcdef-20220905</SessionID><UpgradeResult>ERROR</UpgradeResult><Firmware>v2.3.3</Firmware><UpgradeFailedReason>02</UpgradeFailedReason></Notify>`)
	result, err := ParseDeviceUpgradeResult(body)
	require.NoError(t, err)
	require.Equal(t, CmdDeviceUpgradeResult, result.CmdType)
	require.Equal(t, 17, result.SN)
	require.Equal(t, "D", result.DeviceID)
	require.Equal(t, "ERROR", result.UpgradeResult)
	require.Equal(t, "v2.3.3", result.Firmware)
	require.Equal(t, "02", result.UpgradeFailedReason)
}

func TestParseDeviceUpgradeResultRejectsMissingFailureReason(t *testing.T) {
	body := `<Notify><CmdType>DeviceUpgradeResult</CmdType><SN>17</SN><DeviceID>D</DeviceID><SessionID>` + strings.Repeat("a", 32) + `</SessionID><UpgradeResult>ERROR</UpgradeResult><Firmware>v2</Firmware></Notify>`
	_, err := ParseDeviceUpgradeResult([]byte(body))
	require.Error(t, err)
}
