package manscdp_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

// A.2.6.16 的标准样例：一张正常卡。
const sdcardOneCard = `<?xml version="1.0" encoding="GB2312"?>
<Response>
<CmdType>SDCardStatus</CmdType>
<SN>12</SN>
<DeviceID>34020000001320000001</DeviceID>
<SumNum>1</SumNum>
<SDCardStatusInfo>
<Item>
<ID>1</ID>
<HddName>SD Card 1</HddName>
<Status>ok</Status>
<FormatProgress>0</FormatProgress>
<Capacity>32768</Capacity>
<FreeSpace>24576</FreeSpace>
</Item>
</SDCardStatusInfo>
</Response>`

func TestBuildSDCardStatusQueryUsesStandardCommandName(t *testing.T) {
	body, err := manscdp.BuildSDCardStatusQueryWithProfile(protocol.ProfileFor(protocol.Version2022), "34020000001320000001", 12)
	require.NoError(t, err)

	xml := string(body)
	// ⛔ 这里必须钉死 "SDCardStatus"：早期实现自造过 StorageCardStatusQuery，
	// 那个名字在标准附录 A 里根本不存在（A.2.4.14 fixed="SDCardStatus"）。
	require.Contains(t, xml, "<CmdType>SDCardStatus</CmdType>")
	require.NotContains(t, xml, "StorageCardStatusQuery")
	require.NotContains(t, xml, "SDCardStatusQuery", "标准没有 *Query 后缀这种写法")
	require.Contains(t, xml, "<SN>12</SN>")
	require.Contains(t, xml, "<DeviceID>34020000001320000001</DeviceID>")
	require.Contains(t, xml, "<Query>")
	require.NotContains(t, xml, "<Response>")
}

func TestBuildSDCardStatusQueryRejectsEmptyTarget(t *testing.T) {
	_, err := manscdp.BuildSDCardStatusQuery("", 1)
	require.Error(t, err)
	_, err = manscdp.BuildSDCardStatusQuery("34020000001320000001", 0)
	require.Error(t, err, "SN 必须为正数")
}

func TestParseSDCardStatusResponseFullItem(t *testing.T) {
	status, err := manscdp.ParseSDCardStatusResponse([]byte(sdcardOneCard))
	require.NoError(t, err)

	require.Equal(t, manscdp.CmdSDCardStatus, status.CmdType)
	require.Equal(t, 12, status.SN)
	require.Equal(t, "34020000001320000001", status.DeviceID)
	require.Equal(t, 1, status.SumNum)
	require.Len(t, status.Items, 1)

	item := status.Items[0]
	require.Equal(t, 1, item.ID)
	require.Equal(t, "SD Card 1", item.HddName)
	require.Equal(t, manscdp.SDCardStateOK, item.Status)
	require.NotNil(t, item.FormatProgress)
	require.Equal(t, 0, *item.FormatProgress, "设备给了 0 就必须是 0，不能和'没给'混为一谈")
	require.Equal(t, 32768, item.Capacity)
	require.Equal(t, 24576, item.FreeSpace)
}

func TestParseSDCardStatusResponseWithoutCards(t *testing.T) {
	// 设备没装卡：SumNum=0 且不带 SDCardStatusInfo —— 标准允许的合法结果，
	// 不是错误。前端应展示"无存储卡"。
	body := `<Response><CmdType>SDCardStatus</CmdType><SN>3</SN>` +
		`<DeviceID>D</DeviceID><SumNum>0</SumNum></Response>`
	status, err := manscdp.ParseSDCardStatusResponse([]byte(body))
	require.NoError(t, err)
	require.Equal(t, 0, status.SumNum)
	require.Empty(t, status.Items)
}

func TestParseSDCardStatusResponseKeepsOptionalProgressAbsent(t *testing.T) {
	body := `<Response><CmdType>SDCardStatus</CmdType><SN>3</SN><DeviceID>D</DeviceID><SumNum>1</SumNum>` +
		`<SDCardStatusInfo><Item><ID>1</ID><HddName>X</HddName><Status>unformatted</Status>` +
		`<Capacity>100</Capacity><FreeSpace>0</FreeSpace></Item></SDCardStatusInfo></Response>`
	status, err := manscdp.ParseSDCardStatusResponse([]byte(body))
	require.NoError(t, err)
	require.Len(t, status.Items, 1)
	require.Nil(t, status.Items[0].FormatProgress, "minOccurs=0 的字段没给时必须是 nil")
	require.Equal(t, manscdp.SDCardStateUnformatted, status.Items[0].Status)
}

func TestParseSDCardStatusResponseStatusIsCaseInsensitiveAndUnknownSafe(t *testing.T) {
	cases := map[string]manscdp.SDCardState{
		"ok":          manscdp.SDCardStateOK,
		"OK":          manscdp.SDCardStateOK,
		" Formatting": manscdp.SDCardStateFormatting,
		"IDLE":        manscdp.SDCardStateIdle,
		"error":       manscdp.SDCardStateError,
		"weird":       manscdp.SDCardStateUnknown,
		"":            manscdp.SDCardStateUnknown,
	}
	for raw, want := range cases {
		t.Run(raw, func(t *testing.T) {
			body := `<Response><CmdType>SDCardStatus</CmdType><SN>1</SN><DeviceID>D</DeviceID><SumNum>1</SumNum>` +
				`<SDCardStatusInfo><Item><ID>1</ID><HddName>X</HddName><Status>` + raw + `</Status>` +
				`<Capacity>1</Capacity><FreeSpace>1</FreeSpace></Item></SDCardStatusInfo></Response>`
			status, err := manscdp.ParseSDCardStatusResponse([]byte(body))
			require.NoError(t, err, "未知状态不该让整条应答解析失败")
			require.Equal(t, want, status.Items[0].Status)
		})
	}
}

func TestParseSDCardStatusResponseRejectsMoreThanEightItems(t *testing.T) {
	items := strings.Builder{}
	for i := 1; i <= 9; i++ {
		items.WriteString(`<Item><ID>` + itoa(i) + `</ID><HddName>X</HddName><Status>ok</Status><Capacity>1</Capacity><FreeSpace>1</FreeSpace></Item>`)
	}
	body := `<Response><CmdType>SDCardStatus</CmdType><SN>1</SN><DeviceID>D</DeviceID><SumNum>9</SumNum>` +
		`<SDCardStatusInfo>` + items.String() + `</SDCardStatusInfo></Response>`
	_, err := manscdp.ParseSDCardStatusResponse([]byte(body))
	require.Error(t, err)
	var parseErr *manscdp.SDCardParseError
	require.ErrorAs(t, err, &parseErr)
	require.Equal(t, manscdp.SDCardErrorTooManyItems, parseErr.Code, "标准写死了 maxOccurs=8")
}

func TestParseSDCardStatusResponseAcceptsEightItems(t *testing.T) {
	items := strings.Builder{}
	for i := 1; i <= 8; i++ {
		items.WriteString(`<Item><ID>` + itoa(i) + `</ID><HddName>X</HddName><Status>ok</Status><Capacity>1</Capacity><FreeSpace>1</FreeSpace></Item>`)
	}
	body := `<Response><CmdType>SDCardStatus</CmdType><SN>1</SN><DeviceID>D</DeviceID><SumNum>8</SumNum>` +
		`<SDCardStatusInfo>` + items.String() + `</SDCardStatusInfo></Response>`
	status, err := manscdp.ParseSDCardStatusResponse([]byte(body))
	require.NoError(t, err, "正好 8 条是边界内，必须收")
	require.Len(t, status.Items, 8)
}

func TestParseSDCardStatusResponseRejectsWrongCommandName(t *testing.T) {
	// 模拟器早期的非标准写法必须被拒：它连 CmdType 都对不上。
	body := `<Response><CmdType>StorageCardStatusQuery</CmdType><SN>1</SN><DeviceID>D</DeviceID><SumNum>1</SumNum></Response>`
	_, err := manscdp.ParseSDCardStatusResponse([]byte(body))
	require.Error(t, err)
	var parseErr *manscdp.SDCardParseError
	require.ErrorAs(t, err, &parseErr)
	require.Equal(t, manscdp.SDCardErrorCmdType, parseErr.Code)
}

func TestParseSDCardStatusResponseRequiresSumNum(t *testing.T) {
	body := `<Response><CmdType>SDCardStatus</CmdType><SN>1</SN><DeviceID>D</DeviceID></Response>`
	_, err := manscdp.ParseSDCardStatusResponse([]byte(body))
	require.Error(t, err, "SumNum 是必选字段")
	var parseErr *manscdp.SDCardParseError
	require.ErrorAs(t, err, &parseErr)
	require.Equal(t, manscdp.SDCardErrorMalformed, parseErr.Code)
}

func TestParseSDCardStatusResponseRejectsMissingRequiredItemFields(t *testing.T) {
	for name, item := range map[string]string{
		"ID 缺失":        `<Item><HddName>X</HddName><Status>ok</Status><Capacity>1</Capacity><FreeSpace>1</FreeSpace></Item>`,
		"Capacity 缺失":  `<Item><ID>1</ID><HddName>X</HddName><Status>ok</Status><FreeSpace>1</FreeSpace></Item>`,
		"FreeSpace 缺失": `<Item><ID>1</ID><HddName>X</HddName><Status>ok</Status><Capacity>1</Capacity></Item>`,
		"ID 非整数":       `<Item><ID>abc</ID><HddName>X</HddName><Status>ok</Status><Capacity>1</Capacity><FreeSpace>1</FreeSpace></Item>`,
	} {
		t.Run(name, func(t *testing.T) {
			body := `<Response><CmdType>SDCardStatus</CmdType><SN>1</SN><DeviceID>D</DeviceID><SumNum>1</SumNum>` +
				`<SDCardStatusInfo>` + item + `</SDCardStatusInfo></Response>`
			_, err := manscdp.ParseSDCardStatusResponse([]byte(body))
			require.Error(t, err, "必选整数缺失必须报错，不能静默当 0")
		})
	}
}

func TestParseSDCardStatusResponseRejectsRangeViolations(t *testing.T) {
	for name, item := range map[string]string{
		"Capacity 为负":       `<Item><ID>1</ID><HddName>X</HddName><Status>ok</Status><Capacity>-1</Capacity><FreeSpace>1</FreeSpace></Item>`,
		"FreeSpace 为负":      `<Item><ID>1</ID><HddName>X</HddName><Status>ok</Status><Capacity>1</Capacity><FreeSpace>-1</FreeSpace></Item>`,
		"FormatProgress 越界": `<Item><ID>1</ID><HddName>X</HddName><Status>formatting</Status><FormatProgress>101</FormatProgress><Capacity>1</Capacity><FreeSpace>1</FreeSpace></Item>`,
	} {
		t.Run(name, func(t *testing.T) {
			body := `<Response><CmdType>SDCardStatus</CmdType><SN>1</SN><DeviceID>D</DeviceID><SumNum>1</SumNum>` +
				`<SDCardStatusInfo>` + item + `</SDCardStatusInfo></Response>`
			_, err := manscdp.ParseSDCardStatusResponse([]byte(body))
			require.Error(t, err)
		})
	}
}

func TestParseSDCardStatusResponseForChecksCorrelation(t *testing.T) {
	expectation := manscdp.SDCardStatusExpectation{SN: 12, DeviceID: "34020000001320000001"}
	_, err := manscdp.ParseSDCardStatusResponseFor([]byte(sdcardOneCard), expectation)
	require.NoError(t, err)

	_, err = manscdp.ParseSDCardStatusResponseFor([]byte(sdcardOneCard), manscdp.SDCardStatusExpectation{SN: 99})
	require.Error(t, err)
	var parseErr *manscdp.SDCardParseError
	require.ErrorAs(t, err, &parseErr)
	require.Equal(t, manscdp.SDCardErrorCorrelation, parseErr.Code, "SN 对不上说明答的不是我们问的那件事")

	_, err = manscdp.ParseSDCardStatusResponseFor([]byte(sdcardOneCard), manscdp.SDCardStatusExpectation{SN: 12, DeviceID: "other"})
	require.Error(t, err)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
