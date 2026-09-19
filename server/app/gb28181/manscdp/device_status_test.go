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

// 回放 2026-09-19 海康 DS-2DC2C40MY-DE 的现场应答原文：
// 这四种事实设备一直在报，改之前解析器把它们全丢了。
func TestParseDeviceStatusResponseReadsDeviceReportedFacts(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="GB18030"?>
<Response>
<CmdType>DeviceStatus</CmdType>
<SN>9809</SN>
<DeviceID>D</DeviceID>
<Result>OK</Result>
<Online>ONLINE</Online>
<Status>OK</Status>
<DeviceTime>2026-09-19T20:03:58</DeviceTime>
<Alarmstatus Num="0">
</Alarmstatus>
<Encode>ON</Encode>
<Record>ON</Record>
</Response>`)

	got, err := ParseDeviceStatusResponseFor(body, DeviceStatusExpectation{SN: 9809, DeviceID: "D"})
	require.NoError(t, err)
	require.Equal(t, DeviceOnlineStateOnline, got.Online)
	require.Equal(t, DeviceSelfTestOK, got.SelfTest)
	require.Equal(t, ControlStateOn, got.Encode)
	require.Equal(t, ControlStateOn, got.Record)
	require.Equal(t, "2026-09-19T20:03:58", got.DeviceTime)
	require.True(t, got.AlarmNumKnown, "设备明确写了 Num=0,这是已知的 0 而不是未知")
	require.Zero(t, got.AlarmNum)
	require.Empty(t, got.AlarmItems)
}

func TestParseDeviceStatusResponseAlarmInputCountForms(t *testing.T) {
	for _, tc := range []struct {
		name  string
		alarm string
		num   int
	}{
		{name: "属性形态", alarm: `<Alarmstatus Num="2"><Item><DeviceID>A</DeviceID><DutyStatus>ONDUTY</DutyStatus></Item></Alarmstatus>`, num: 2},
		{name: "小写属性形态", alarm: `<Alarmstatus num="1"><Item><DeviceID>A</DeviceID><DutyStatus>ONDUTY</DutyStatus></Item></Alarmstatus>`, num: 1},
		{name: "子元素形态", alarm: `<Alarmstatus><Num>1</Num><Item><DeviceID>A</DeviceID><DutyStatus>OFFDUTY</DutyStatus></Item></Alarmstatus>`, num: 1},
		{name: "明确零个", alarm: `<Alarmstatus Num="0"></Alarmstatus>`, num: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`<Response><CmdType>DeviceStatus</CmdType><SN>3</SN><DeviceID>D</DeviceID><Result>OK</Result>` + tc.alarm + `</Response>`)
			got, err := ParseDeviceStatusResponse(body)
			require.NoError(t, err)
			require.True(t, got.AlarmNumKnown)
			require.Equal(t, tc.num, got.AlarmNum)
		})
	}

	// 设备整段没提报警输入 ⇒ 未知,不能塌成"0 个"。
	missing := []byte(`<Response><CmdType>DeviceStatus</CmdType><SN>4</SN><DeviceID>D</DeviceID><Result>OK</Result><Record>OFF</Record></Response>`)
	got, err := ParseDeviceStatusResponse(missing)
	require.NoError(t, err)
	require.False(t, got.AlarmNumKnown, "设备没写报警输入数量时不得声称已知")
	require.Zero(t, got.AlarmNum)
}

func TestParseDeviceStatusUnknownValuesStayUnknown(t *testing.T) {
	body := []byte(`<Response><CmdType>DeviceStatus</CmdType><SN>5</SN><DeviceID>D</DeviceID><Result>OK</Result><Online>WHATEVER</Online><Status>BROKEN</Status><Encode>MAYBE</Encode></Response>`)
	got, err := ParseDeviceStatusResponse(body)
	require.NoError(t, err)
	require.Equal(t, DeviceOnlineStateUnknown, got.Online)
	require.Equal(t, DeviceSelfTestUnknown, got.SelfTest)
	require.Equal(t, ControlStateUnknown, got.Encode)
}

func TestParseDeviceSelfTestRecognizesError(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want DeviceSelfTestState
	}{
		{raw: "OK", want: DeviceSelfTestOK},
		{raw: "ok", want: DeviceSelfTestOK},
		{raw: "Error", want: DeviceSelfTestError},
		{raw: "ERROR", want: DeviceSelfTestError},
		{raw: "", want: DeviceSelfTestUnknown},
		{raw: "SomethingElse", want: DeviceSelfTestUnknown},
	} {
		body := []byte(`<Response><CmdType>DeviceStatus</CmdType><SN>6</SN><DeviceID>D</DeviceID><Result>OK</Result><Status>` + tc.raw + `</Status></Response>`)
		got, err := ParseDeviceStatusResponse(body)
		require.NoError(t, err)
		require.Equalf(t, tc.want, got.SelfTest, "Status=%q", tc.raw)
	}
}

func TestParseDeviceClock(t *testing.T) {
	parsed, ok := ParseDeviceClock("2026-09-19T20:03:58")
	require.True(t, ok)
	require.Equal(t, 2026, parsed.Year())
	require.Equal(t, 20, parsed.Hour())

	for _, invalid := range []string{"", "   ", "not-a-time", "2026/09/19 20:03:58"} {
		_, ok := ParseDeviceClock(invalid)
		require.Falsef(t, ok, "不应把 %q 解析成时间", invalid)
	}
}
