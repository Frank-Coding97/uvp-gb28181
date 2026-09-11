package catalog

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

func TestPlanDeviceInfoResponseUsesPlatformAndProjectionWhitelists(t *testing.T) {
	platform := PlatformDeviceInfo{
		DeviceID:     "34020000002000000001",
		DeviceName:   "UVP platform",
		Manufacturer: "UVP",
		Model:        "cascade",
	}
	snapshot := Snapshot{Items: []CatalogItem{
		{
			ID:           "34020000001320000001",
			Kind:         CatalogItemDevice,
			Name:         "gate device",
			Manufacturer: "camera vendor",
			Model:        "camera model",
			Owner:        "must-not-leak",
			Address:      "must-not-leak",
			Status:       ResourceStatusOnline,
		},
	}}

	for _, version := range []protocol.Version{protocol.Version2016, protocol.Version2022} {
		t.Run(string(version)+"/platform", func(t *testing.T) {
			profile := protocol.ProfileFor(version)
			query, err := manscdp.BuildDeviceInfoQueryWithProfile(profile, platform.DeviceID, 17)
			require.NoError(t, err)

			response, err := PlanDeviceInfoResponse(profile, platform, snapshot, query)
			require.NoError(t, err)
			require.Equal(t, 17, response.SN)
			require.Equal(t, platform.DeviceID, response.DeviceID)
			require.Contains(t, string(response.Body), `encoding="`+string(protocol.CharsetFor(version))+`"`)

			decoded := decodeDeviceInfoResponse(t, profile, response.Body)
			require.Equal(t, deviceInfoResponseTestXML{
				CmdType: "DeviceInfo", SN: 17, DeviceID: platform.DeviceID, Result: "OK",
				DeviceName: "UVP platform", Manufacturer: "UVP", Model: "cascade",
			}, decoded)
		})

		t.Run(string(version)+"/published-device", func(t *testing.T) {
			profile := protocol.ProfileFor(version)
			query, err := manscdp.BuildDeviceInfoQueryWithProfile(profile, "34020000001320000001", 18)
			require.NoError(t, err)

			response, err := PlanDeviceInfoResponse(profile, platform, snapshot, query)
			require.NoError(t, err)
			decoded := decodeDeviceInfoResponse(t, profile, response.Body)
			require.Equal(t, "gate device", decoded.DeviceName)
			require.Equal(t, "camera vendor", decoded.Manufacturer)
			require.Equal(t, "camera model", decoded.Model)
			require.Nil(t, decoded.Firmware)
			require.Nil(t, decoded.Channel)
			require.NotContains(t, string(response.Body), "must-not-leak")
		})
	}
}

func TestPlanDeviceStatusResponseUsesExplicitFactsAndProfiledAlarmAttribute(t *testing.T) {
	const (
		platformID = "34020000002000000001"
		deviceID   = "34020000001320000001"
	)
	snapshot := Snapshot{Items: []CatalogItem{{ID: deviceID, Kind: CatalogItemDevice, Status: ResourceStatusOnline}}}
	recordOn := true
	facts := DeviceStatusFacts{
		deviceID: {
			Online:  false,
			Working: true,
			Record:  &recordOn,
			Alarm: &AlarmStatusFact{Items: []AlarmStatusItemFact{
				{DeviceID: deviceID, DutyStatus: "ONDUTY"},
			}},
		},
	}

	for _, version := range []protocol.Version{protocol.Version2016, protocol.Version2022} {
		t.Run(string(version), func(t *testing.T) {
			profile := protocol.ProfileFor(version)
			query, err := manscdp.BuildDeviceStatusQueryWithProfile(profile, deviceID, 27)
			require.NoError(t, err)

			response, err := PlanDeviceStatusResponse(profile, platformID, snapshot, facts, query)
			require.NoError(t, err)
			decoded := decodeDeviceStatusResponse(t, profile, response.Body)
			require.Equal(t, "OFFLINE", decoded.Online, "Catalog status must not be reused as the DeviceStatus fact")
			require.Equal(t, "OK", decoded.Status)
			require.Equal(t, "ON", decoded.Record)
			require.Empty(t, decoded.Encode)
			require.Len(t, decoded.Alarmstatus.Items, 1)

			if version == protocol.Version2016 {
				require.Equal(t, "1", decoded.Alarmstatus.NumLower)
				require.Empty(t, decoded.Alarmstatus.NumUpper)
				require.Contains(t, string(response.Body), `<Alarmstatus num="1">`)
			} else {
				require.Equal(t, "1", decoded.Alarmstatus.NumUpper)
				require.Empty(t, decoded.Alarmstatus.NumLower)
				require.Contains(t, string(response.Body), `<Alarmstatus Num="1">`)
			}
		})
	}
}

func TestPlanDeviceStatusResponseOmitsUnknownOptionalFacts(t *testing.T) {
	const platformID = "34020000002000000001"
	profile := protocol.ProfileFor(protocol.Version2022)
	query, err := manscdp.BuildDeviceStatusQueryWithProfile(profile, platformID, 31)
	require.NoError(t, err)

	response, err := PlanDeviceStatusResponse(profile, platformID, Snapshot{}, DeviceStatusFacts{
		platformID: {Online: true, Working: false, Reason: "degraded"},
	}, query)
	require.NoError(t, err)
	require.Contains(t, string(response.Body), "<Online>ONLINE</Online>")
	require.Contains(t, string(response.Body), "<Status>ERROR</Status>")
	require.Contains(t, string(response.Body), "<Reason>degraded</Reason>")
	require.NotContains(t, string(response.Body), "<Encode>")
	require.NotContains(t, string(response.Body), "<Record>")
	require.NotContains(t, string(response.Body), "<Alarmstatus")
}

func TestInfoStatusRespondersRejectInvalidOrUnscopedQueries(t *testing.T) {
	const platformID = "34020000002000000001"
	profile := protocol.ProfileFor(protocol.Version2016)
	platform := PlatformDeviceInfo{DeviceID: platformID}

	_, err := PlanDeviceInfoResponse(profile, platform, Snapshot{}, []byte(`<Response><CmdType>DeviceInfo</CmdType><SN>1</SN><DeviceID>`+platformID+`</DeviceID></Response>`))
	require.ErrorIs(t, err, ErrInvalidInfoStatusQuery)

	wrongCommand, err := manscdp.BuildDeviceStatusQueryWithProfile(profile, platformID, 2)
	require.NoError(t, err)
	_, err = PlanDeviceInfoResponse(profile, platform, Snapshot{}, wrongCommand)
	require.ErrorIs(t, err, ErrInvalidInfoStatusQuery)

	unknown, err := manscdp.BuildDeviceInfoQueryWithProfile(profile, "34020000001320000009", 3)
	require.NoError(t, err)
	_, unknownErr := PlanDeviceInfoResponse(profile, platform, Snapshot{}, unknown)
	require.ErrorIs(t, unknownErr, ErrUnknownInfoStatusTarget)
	var targetErr *InfoStatusTargetError
	require.True(t, errors.As(unknownErr, &targetErr))
	require.Equal(t, "34020000001320000009", targetErr.DeviceID)

	statusQuery, err := manscdp.BuildDeviceStatusQueryWithProfile(profile, platformID, 4)
	require.NoError(t, err)
	_, err = PlanDeviceStatusResponse(profile, platformID, Snapshot{}, DeviceStatusFacts{}, statusQuery)
	require.ErrorIs(t, err, ErrMissingDeviceStatusFact)

}

type deviceInfoResponseTestXML struct {
	CmdType      string  `xml:"CmdType"`
	SN           int     `xml:"SN"`
	DeviceID     string  `xml:"DeviceID"`
	Result       string  `xml:"Result"`
	DeviceName   string  `xml:"DeviceName"`
	Manufacturer string  `xml:"Manufacturer"`
	Model        string  `xml:"Model"`
	Firmware     *string `xml:"Firmware"`
	Channel      *int    `xml:"Channel"`
}

type deviceStatusResponseTestXML struct {
	CmdType     string `xml:"CmdType"`
	SN          int    `xml:"SN"`
	DeviceID    string `xml:"DeviceID"`
	Result      string `xml:"Result"`
	Online      string `xml:"Online"`
	Status      string `xml:"Status"`
	Reason      string `xml:"Reason"`
	Encode      string `xml:"Encode"`
	Record      string `xml:"Record"`
	Alarmstatus struct {
		NumLower string `xml:"num,attr"`
		NumUpper string `xml:"Num,attr"`
		Items    []struct {
			DeviceID   string `xml:"DeviceID"`
			DutyStatus string `xml:"DutyStatus"`
		} `xml:"Item"`
	} `xml:"Alarmstatus"`
}

func decodeDeviceInfoResponse(t *testing.T, profile protocol.Profile, body []byte) deviceInfoResponseTestXML {
	t.Helper()
	var decoded deviceInfoResponseTestXML
	require.NoError(t, manscdp.DecodeProfiledXML(profile, body, &decoded))
	return decoded
}

func decodeDeviceStatusResponse(t *testing.T, profile protocol.Profile, body []byte) deviceStatusResponseTestXML {
	t.Helper()
	var decoded deviceStatusResponseTestXML
	require.NoError(t, manscdp.DecodeProfiledXML(profile, body, &decoded))
	return decoded
}
