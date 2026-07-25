package manscdp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

func TestBuildDeviceStatusQueryUsesProfile(t *testing.T) {
	body, err := BuildDeviceStatusQueryWithProfile(protocol.ProfileFor(protocol.Version2022), "D", 7)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(body), `<?xml version="1.0" encoding="GB18030"?>`))
	require.Contains(t, string(body), "<CmdType>DeviceStatus</CmdType>")
	require.Contains(t, string(body), "<SN>7</SN>")
	require.Contains(t, string(body), "<DeviceID>D</DeviceID>")
}

func TestParseDeviceStatusResponseTriStateAndNumVariants(t *testing.T) {
	for _, tc := range []struct {
		name   string
		attr   string
		record ControlState
		duty   DutyStatus
	}{
		{name: "2016 lower num", attr: `num="1"`, record: ControlStateOn, duty: DutyStatusOnDuty},
		{name: "2022 upper Num", attr: `Num="1"`, record: ControlStateOff, duty: DutyStatusOffDuty},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`<?xml version="1.0" encoding="GB2312"?><Response><CmdType>DeviceStatus</CmdType><SN>7</SN><DeviceID>D</DeviceID><Result>OK</Result><Record>` + string(tc.record) + `</Record><Alarmstatus><Item ` + tc.attr + `><DeviceID>A</DeviceID><DutyStatus>` + string(tc.duty) + `</DutyStatus></Item></Alarmstatus></Response>`)
			got, err := ParseDeviceStatusResponseFor(body, DeviceStatusExpectation{SN: 7, DeviceID: "D"})
			require.NoError(t, err)
			require.Equal(t, tc.record, got.Record)
			require.Equal(t, tc.duty, got.AlarmItems[0].DutyStatus)
			require.Equal(t, 1, got.AlarmItems[0].Num)
		})
	}
}

func TestParseDeviceStatusResponseReadsAlarmListNum(t *testing.T) {
	body := []byte(`<Response><CmdType>DeviceStatus</CmdType><SN>8</SN><DeviceID>D</DeviceID><Result>OK</Result><Alarmstatus Num="2"><Item><DeviceID>A</DeviceID><DutyStatus>ONDUTY</DutyStatus></Item><Item><DeviceID>B</DeviceID><DutyStatus>ALARM</DutyStatus></Item></Alarmstatus></Response>`)

	got, err := ParseDeviceStatusResponseFor(body, DeviceStatusExpectation{SN: 8, DeviceID: "D"})
	require.NoError(t, err)
	require.Equal(t, 2, got.AlarmNum)
	require.Len(t, got.AlarmItems, 2)
	require.Zero(t, got.AlarmItems[0].Num)
	require.Zero(t, got.AlarmItems[1].Num)
}

func TestParseDeviceStatusMissingFieldsRemainUnknown(t *testing.T) {
	body := []byte(`<Response><CmdType>DeviceStatus</CmdType><SN>1</SN><DeviceID>D</DeviceID><Result>OK</Result><Alarmstatus><Item><DeviceID>A</DeviceID></Item></Alarmstatus></Response>`)
	got, err := ParseDeviceStatusResponse(body)
	require.NoError(t, err)
	require.Equal(t, ControlStateUnknown, got.Record)
	require.Equal(t, DutyStatusUnknown, got.AlarmItems[0].DutyStatus)
}

func TestParseDeviceStatusRejectsMismatchedResponse(t *testing.T) {
	body := []byte(`<Response><CmdType>DeviceStatus</CmdType><SN>2</SN><DeviceID>other</DeviceID><Result>OK</Result></Response>`)
	_, err := ParseDeviceStatusResponseFor(body, DeviceStatusExpectation{SN: 1, DeviceID: "D"})
	var parseErr *DeviceStatusParseError
	require.ErrorAs(t, err, &parseErr)
	require.Equal(t, DeviceStatusErrorCorrelation, parseErr.Code)

	errorBody := []byte(`<Response><CmdType>DeviceStatus</CmdType><SN>1</SN><DeviceID>D</DeviceID><Result>ERROR</Result></Response>`)
	_, err = ParseDeviceStatusResponse(errorBody)
	require.ErrorAs(t, err, &parseErr)
	require.Equal(t, DeviceStatusErrorResult, parseErr.Code)
}
