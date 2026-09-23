package manscdp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/manscdp"
	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

const videoParamDeviceID = "34020000001320000001"

// A.2.6.9 的标准样例：两条码流，第一条 CBR（带码率），第二条 VBR（条件必选缺席）。
const configDownloadTwoStreams = `<?xml version="1.0" encoding="GB2312"?>
<Response>
<CmdType>ConfigDownload</CmdType>
<SN>7</SN>
<DeviceID>34020000001320000001</DeviceID>
<Result>OK</Result>
<VideoParamAttribute Num="2">
<Item>
<StreamNumber>0</StreamNumber>
<VideoFormat>2</VideoFormat>
<Resolution>5</Resolution>
<FrameRate>25</FrameRate>
<BitRateType>1</BitRateType>
<VideoBitRate>2048</VideoBitRate>
</Item>
<Item>
<StreamNumber>1</StreamNumber>
<VideoFormat>2</VideoFormat>
<Resolution>2</Resolution>
<FrameRate>15</FrameRate>
<BitRateType>2</BitRateType>
</Item>
</VideoParamAttribute>
</Response>`

func strPtr(value string) *string { return &value }

// ---- 下发（DeviceConfig）----

func TestBuildVideoParamAttributeConfigUsesStandardShape(t *testing.T) {
	body, err := manscdp.BuildVideoParamAttributeConfigWithProfile(
		protocol.ProfileFor(protocol.Version2022), videoParamDeviceID, 7,
		[]manscdp.VideoParamItem{{
			StreamNumber: 0,
			VideoFormat:  manscdp.VideoFormatH264,
			Resolution:   manscdp.Resolution720P,
			FrameRate:    "25",
			BitRateType:  manscdp.BitRateTypeCBR,
			VideoBitRate: strPtr("2048"),
		}},
	)
	require.NoError(t, err)

	xml := string(body)
	require.Contains(t, xml, "<Control>")
	require.Contains(t, xml, "<CmdType>DeviceConfig</CmdType>")
	require.NotContains(t, xml, "<Query>", "写命令是 Control 不是 Query")
	require.Contains(t, xml, "<SN>7</SN>")
	require.Contains(t, xml, "<DeviceID>"+videoParamDeviceID+"</DeviceID>")
	// ⛔ Num 必须是**属性**：标准把它挂在 videoParamAttributeCfgType 上。
	require.Contains(t, xml, `<VideoParamAttribute Num="1">`)
	require.NotContains(t, xml, "<Num>", "Num 不是元素")
	require.NotContains(t, xml, "<SumNum>", "标准里没有 SumNum")
	require.Contains(t, xml, "<Item>")
	require.Contains(t, xml, "<StreamNumber>0</StreamNumber>")
	require.Contains(t, xml, "<VideoFormat>2</VideoFormat>")
	require.Contains(t, xml, "<Resolution>5</Resolution>")
	require.Contains(t, xml, "<FrameRate>25</FrameRate>")
	require.Contains(t, xml, "<BitRateType>1</BitRateType>")
	require.Contains(t, xml, "<VideoBitRate>2048</VideoBitRate>")
}

// 附录 G 的取值是**数字码值**。发人读串会被对端当 0 处理，
// 而这种错在回读对账里表现为"设备没照做"，归因成本极高。
func TestBuildVideoParamAttributeConfigNeverEmitsHumanReadableCodes(t *testing.T) {
	body, err := manscdp.BuildVideoParamAttributeConfigWithProfile(
		protocol.ProfileFor(protocol.Version2022), videoParamDeviceID, 1,
		[]manscdp.VideoParamItem{{
			StreamNumber: 0,
			VideoFormat:  manscdp.VideoFormatH264,
			Resolution:   manscdp.Resolution720P,
			FrameRate:    "25",
			BitRateType:  manscdp.BitRateTypeVBR,
		}},
	)
	require.NoError(t, err)
	xml := string(body)
	for _, humanReadable := range []string{"H.264", "H264", "720P", "1080P", "VBR", "CBR", "MPEG-4"} {
		require.NotContains(t, xml, humanReadable, "报文里只能有附录 G 的码值")
	}
}

func TestBuildVideoParamAttributeConfigKeepsBitRateAbsentForVBR(t *testing.T) {
	body, err := manscdp.BuildVideoParamAttributeConfigWithProfile(
		protocol.ProfileFor(protocol.Version2022), videoParamDeviceID, 1,
		[]manscdp.VideoParamItem{{
			StreamNumber: 0,
			VideoFormat:  manscdp.VideoFormatH265,
			Resolution:   manscdp.Resolution1080P,
			FrameRate:    "30",
			BitRateType:  manscdp.BitRateTypeVBR,
		}},
	)
	require.NoError(t, err)
	require.NotContains(t, string(body), "VideoBitRate",
		"标准的 VideoBitRate 是条件必选：VBR 时不该出现在报文里")
}

func TestBuildVideoParamAttributeConfigAllowsEmptyItems(t *testing.T) {
	body, err := manscdp.BuildVideoParamAttributeConfig(videoParamDeviceID, 3, nil)
	require.NoError(t, err)
	xml := string(body)
	// A.2.1.13 的 Item 是 minOccurs="0" ⇒ 空配置合法，且必须仍然发出这个元素，
	// 否则设备分不清"清空配置"与"这一帧没有配置段"。
	require.Contains(t, xml, `<VideoParamAttribute Num="0">`)
	require.NotContains(t, xml, "<Item>")
}

func TestBuildVideoParamAttributeConfigAcceptsWxHResolution(t *testing.T) {
	body, err := manscdp.BuildVideoParamAttributeConfig(videoParamDeviceID, 1,
		[]manscdp.VideoParamItem{{
			StreamNumber: 0,
			VideoFormat:  manscdp.VideoFormatH264,
			Resolution:   "1920x1080",
			FrameRate:    "25",
			BitRateType:  manscdp.BitRateTypeVBR,
		}})
	require.NoError(t, err, "附录 G 允许「其余分辨率用 WxH」表示")
	require.Contains(t, string(body), "<Resolution>1920x1080</Resolution>")
}

func TestBuildVideoParamAttributeConfigRejectsAppendixGViolations(t *testing.T) {
	base := func(mutate func(item *manscdp.VideoParamItem)) manscdp.VideoParamItem {
		item := manscdp.VideoParamItem{
			StreamNumber: 0,
			VideoFormat:  manscdp.VideoFormatH264,
			Resolution:   manscdp.Resolution720P,
			FrameRate:    "25",
			BitRateType:  manscdp.BitRateTypeCBR,
			VideoBitRate: strPtr("2048"),
		}
		mutate(&item)
		return item
	}

	cases := map[string][]manscdp.VideoParamItem{
		"VideoFormat 越界":  {base(func(i *manscdp.VideoParamItem) { i.VideoFormat = "9" })},
		"VideoFormat 人读串": {base(func(i *manscdp.VideoParamItem) { i.VideoFormat = "H.264" })},
		"VideoFormat 缺失":  {base(func(i *manscdp.VideoParamItem) { i.VideoFormat = "" })},
		"Resolution 非法":   {base(func(i *manscdp.VideoParamItem) { i.Resolution = "abc" })},
		// 目录 Info 里设备用的是 1920*1080，但本处是平台按标准拼的码值，
		// 附录 G 写的是 WxH(x)，两者不能互相宽容。
		"Resolution 用星号":  {base(func(i *manscdp.VideoParamItem) { i.Resolution = "1920*1080" })},
		"Resolution 前导零":  {base(func(i *manscdp.VideoParamItem) { i.Resolution = "0640x480" })},
		"FrameRate 越界":    {base(func(i *manscdp.VideoParamItem) { i.FrameRate = "100" })},
		"FrameRate 为负":    {base(func(i *manscdp.VideoParamItem) { i.FrameRate = "-1" })},
		"FrameRate 非数字":   {base(func(i *manscdp.VideoParamItem) { i.FrameRate = "25fps" })},
		"FrameRate 缺失":    {base(func(i *manscdp.VideoParamItem) { i.FrameRate = "" })},
		"BitRateType 越界":  {base(func(i *manscdp.VideoParamItem) { i.BitRateType = "3" })},
		"BitRateType 人读串": {base(func(i *manscdp.VideoParamItem) { i.BitRateType = "VBR" })},
		"CBR 缺码率":         {base(func(i *manscdp.VideoParamItem) { i.VideoBitRate = nil })},
		"CBR 码率越界":        {base(func(i *manscdp.VideoParamItem) { i.VideoBitRate = strPtr("100001") })},
		"CBR 码率为负":        {base(func(i *manscdp.VideoParamItem) { i.VideoBitRate = strPtr("-1") })},
		"CBR 码率为空串":       {base(func(i *manscdp.VideoParamItem) { i.VideoBitRate = strPtr("") })},
		"VBR 不该带码率": {
			base(func(i *manscdp.VideoParamItem) { i.BitRateType = manscdp.BitRateTypeVBR }),
		},
		"StreamNumber 为负": {base(func(i *manscdp.VideoParamItem) { i.StreamNumber = -1 })},
		"StreamNumber 重复": {
			base(func(i *manscdp.VideoParamItem) {}),
			base(func(i *manscdp.VideoParamItem) {}),
		},
	}
	for name, items := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := manscdp.BuildVideoParamAttributeConfig(videoParamDeviceID, 1, items)
			require.Error(t, err, "平台自己发出的取值必须落在附录 G 内")
		})
	}
}

func TestBuildVideoParamAttributeConfigRejectsEmptyTarget(t *testing.T) {
	items := []manscdp.VideoParamItem{{
		StreamNumber: 0, VideoFormat: manscdp.VideoFormatH264, Resolution: manscdp.Resolution720P,
		FrameRate: "25", BitRateType: manscdp.BitRateTypeVBR,
	}}
	_, err := manscdp.BuildVideoParamAttributeConfig("", 1, items)
	require.Error(t, err)
	_, err = manscdp.BuildVideoParamAttributeConfig(videoParamDeviceID, 0, items)
	require.Error(t, err, "SN 必须为正数")
}

// ---- 读取（ConfigDownload）----

func TestBuildConfigDownloadQueryJoinsMultipleTypes(t *testing.T) {
	body, err := manscdp.BuildConfigDownloadQueryWithProfile(
		protocol.ProfileFor(protocol.Version2022), videoParamDeviceID, 7,
		[]string{manscdp.ConfigTypeVideoParamOpt, manscdp.ConfigTypeVideoParamAttribute},
	)
	require.NoError(t, err)

	xml := string(body)
	require.Contains(t, xml, "<Query>")
	require.Contains(t, xml, "<CmdType>ConfigDownload</CmdType>")
	require.NotContains(t, xml, "<Response>")
	// 多类型以 `/` 分隔（A.2.4.7）。
	require.Contains(t, xml, "<ConfigType>VideoParamOpt/VideoParamAttribute</ConfigType>")
}

func TestBuildConfigDownloadQueryDeduplicatesTypes(t *testing.T) {
	body, err := manscdp.BuildConfigDownloadQuery(videoParamDeviceID, 1,
		[]string{manscdp.ConfigTypeVideoParamAttribute, manscdp.ConfigTypeVideoParamAttribute})
	require.NoError(t, err)
	require.Contains(t, string(body), "<ConfigType>VideoParamAttribute</ConfigType>")
}

func TestBuildConfigDownloadQueryRejectsMalformedTypes(t *testing.T) {
	for name, types := range map[string][]string{
		"空列表":  nil,
		"空项":   {manscdp.ConfigTypeVideoParamAttribute, "  "},
		"含分隔符": {"VideoParamOpt/VideoParamAttribute"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := manscdp.BuildConfigDownloadQuery(videoParamDeviceID, 1, types)
			require.Error(t, err)
		})
	}
}

// ---- 解析应答 ----

func TestParseConfigDownloadResponseFullBlock(t *testing.T) {
	result, err := manscdp.ParseConfigDownloadResponse([]byte(configDownloadTwoStreams), manscdp.ConfigTypeVideoParamAttribute)
	require.NoError(t, err)

	require.Equal(t, manscdp.CmdConfigDownload, result.CmdType)
	require.Equal(t, 7, result.SN)
	require.Equal(t, videoParamDeviceID, result.DeviceID)
	require.Equal(t, "OK", result.Result)
	require.True(t, result.HasVideoParamAttribute())
	require.True(t, result.VideoParamAttribute.NumPresent)
	require.Equal(t, 2, result.VideoParamAttribute.Num)

	items := result.VideoParamItems()
	require.Len(t, items, 2)

	require.Equal(t, 0, items[0].StreamNumber)
	require.Equal(t, manscdp.VideoFormatH264, items[0].VideoFormat)
	require.Equal(t, manscdp.Resolution720P, items[0].Resolution)
	require.Equal(t, "25", items[0].FrameRate)
	require.Equal(t, manscdp.BitRateTypeCBR, items[0].BitRateType)
	require.NotNil(t, items[0].VideoBitRate)
	require.Equal(t, "2048", *items[0].VideoBitRate)

	require.Equal(t, 1, items[1].StreamNumber)
	require.Equal(t, manscdp.BitRateTypeVBR, items[1].BitRateType)
	require.Nil(t, items[1].VideoBitRate, "条件必选字段缺席必须是 nil，不是空串")
}

// ⛔ 这是本卡最关键的协议约定：`Result=OK` 但**没带**请求的配置类型，
// 等价于"设备不支持该类型"（规格 §十④ 里最可靠的判据）。
// 绝不能返回一个空结果让调用方以为一切正常。
func TestParseConfigDownloadResponseReportsTypeAbsent(t *testing.T) {
	body := `<Response><CmdType>ConfigDownload</CmdType><SN>7</SN>` +
		`<DeviceID>D</DeviceID><Result>OK</Result></Response>`
	_, err := manscdp.ParseConfigDownloadResponse([]byte(body), manscdp.ConfigTypeVideoParamAttribute)
	require.Error(t, err)
	var parseErr *manscdp.ConfigDownloadError
	require.ErrorAs(t, err, &parseErr)
	require.Equal(t, manscdp.ConfigErrorTypeAbsent, parseErr.Code)
}

func TestParseConfigDownloadResponseDoesNotReportTypeAbsentWhenDeviceRejects(t *testing.T) {
	// 设备明确回 ERROR 是"拒绝"，与"不认识这个类型"是两件事：
	// 前者归因给设备策略，后者归因给版本/厂商实现。分流不同，不能合并。
	body := `<Response><CmdType>ConfigDownload</CmdType><SN>7</SN>` +
		`<DeviceID>D</DeviceID><Result>ERROR</Result></Response>`
	result, err := manscdp.ParseConfigDownloadResponse([]byte(body), manscdp.ConfigTypeVideoParamAttribute)
	require.NoError(t, err, "Result=ERROR 是合法应答，交上层按结论分流")
	require.Equal(t, "ERROR", result.Result)
	require.False(t, result.HasVideoParamAttribute())
}

func TestParseConfigDownloadResponseAcceptsEmptyBlock(t *testing.T) {
	// 带元素但一条 Item 都没有 = 设备明确回了空配置。
	// ⛔ 不能与"设备不支持"混为一谈 —— 所以判据是"元素在不在"，不是"有没有内容"。
	body := `<Response><CmdType>ConfigDownload</CmdType><SN>7</SN><DeviceID>D</DeviceID>` +
		`<Result>OK</Result><VideoParamAttribute Num="0"></VideoParamAttribute></Response>`
	result, err := manscdp.ParseConfigDownloadResponse([]byte(body), manscdp.ConfigTypeVideoParamAttribute)
	require.NoError(t, err)
	require.True(t, result.HasVideoParamAttribute())
	require.Empty(t, result.VideoParamItems())
	require.True(t, result.VideoParamAttribute.NumPresent)
}

// 宽松收：设备回了不合附录 G 的写法（人读串、越界值），也要原样带上来。
// 若在解析层报错，整份应答会被丢掉，用户看到"协议错误"，
// 而真相只是"设备回了个人读串" —— 后者才是可行动的结论。
func TestParseConfigDownloadResponseKeepsUnrecognizedValues(t *testing.T) {
	for name, item := range map[string]string{
		"人读编码格式": `<Item><StreamNumber>0</StreamNumber><VideoFormat>H.264</VideoFormat><Resolution>720P</Resolution>` +
			`<FrameRate>25</FrameRate><BitRateType>CBR</BitRateType><VideoBitRate>2048</VideoBitRate></Item>`,
		"帧率越界": `<Item><StreamNumber>0</StreamNumber><VideoFormat>2</VideoFormat><Resolution>5</Resolution>` +
			`<FrameRate>120</FrameRate><BitRateType>2</BitRateType></Item>`,
		"取值全缺": `<Item><StreamNumber>3</StreamNumber></Item>`,
	} {
		t.Run(name, func(t *testing.T) {
			body := `<Response><CmdType>ConfigDownload</CmdType><SN>1</SN><DeviceID>D</DeviceID>` +
				`<Result>OK</Result><VideoParamAttribute Num="1">` + item + `</VideoParamAttribute></Response>`
			result, err := manscdp.ParseConfigDownloadResponse([]byte(body), manscdp.ConfigTypeVideoParamAttribute)
			require.NoError(t, err, "取值问题不该让整份应答解析失败")
			require.Len(t, result.VideoParamItems(), 1)
		})
	}
}

func TestParseConfigDownloadResponseKeepsUnrecognizedValuesVerbatim(t *testing.T) {
	body := `<Response><CmdType>ConfigDownload</CmdType><SN>1</SN><DeviceID>D</DeviceID>` +
		`<Result>OK</Result><VideoParamAttribute Num="1">` +
		`<Item><StreamNumber>0</StreamNumber><VideoFormat>H.264</VideoFormat><Resolution>720P</Resolution>` +
		`<FrameRate>25</FrameRate><BitRateType>CBR</BitRateType></Item></VideoParamAttribute></Response>`
	result, err := manscdp.ParseConfigDownloadResponse([]byte(body), manscdp.ConfigTypeVideoParamAttribute)
	require.NoError(t, err)
	item := result.VideoParamItems()[0]
	require.Equal(t, "H.264", item.VideoFormat, "对账要比的就是设备原样给的值")
	require.Equal(t, "720P", item.Resolution)
	require.Equal(t, "CBR", item.BitRateType)
}

// StreamNumber 是**结构**字段（决定分段与落库唯一键），缺了按 malformed 拒；
// 这与上面那组"取值宽松"并不矛盾 —— 一个是结构，一个是取值。
func TestParseConfigDownloadResponseRejectsMissingStreamNumber(t *testing.T) {
	body := `<Response><CmdType>ConfigDownload</CmdType><SN>1</SN><DeviceID>D</DeviceID>` +
		`<Result>OK</Result><VideoParamAttribute Num="1">` +
		`<Item><VideoFormat>2</VideoFormat></Item></VideoParamAttribute></Response>`
	_, err := manscdp.ParseConfigDownloadResponse([]byte(body), manscdp.ConfigTypeVideoParamAttribute)
	require.Error(t, err)
	var parseErr *manscdp.ConfigDownloadError
	require.ErrorAs(t, err, &parseErr)
	require.Equal(t, manscdp.ConfigErrorMalformed, parseErr.Code)
}

func TestParseConfigDownloadResponseReportsNumMismatchWithoutRejecting(t *testing.T) {
	// Num 是设备自己声明的数，Items 是它实际给的内容。以 Items 为准（分段靠它），
	// 同时把 Num 原样带上来，让上层有机会发现"设备自相矛盾"。
	body := `<Response><CmdType>ConfigDownload</CmdType><SN>1</SN><DeviceID>D</DeviceID>` +
		`<Result>OK</Result><VideoParamAttribute Num="5">` +
		`<Item><StreamNumber>0</StreamNumber></Item></VideoParamAttribute></Response>`
	result, err := manscdp.ParseConfigDownloadResponse([]byte(body), manscdp.ConfigTypeVideoParamAttribute)
	require.NoError(t, err)
	require.Equal(t, 5, result.VideoParamAttribute.Num)
	require.Len(t, result.VideoParamItems(), 1)
}

func TestParseConfigDownloadResponseRejectsWrongCommandName(t *testing.T) {
	body := `<Response><CmdType>DeviceConfig</CmdType><SN>1</SN><DeviceID>D</DeviceID><Result>OK</Result></Response>`
	_, err := manscdp.ParseConfigDownloadResponse([]byte(body), "")
	require.Error(t, err, "写入应答与读取应答虽然同名，但 CmdType 不同，不能互认")
	var parseErr *manscdp.ConfigDownloadError
	require.ErrorAs(t, err, &parseErr)
	require.Equal(t, manscdp.ConfigErrorCmdType, parseErr.Code)
}

func TestParseConfigDownloadResponseRejectsUnsupportedWantType(t *testing.T) {
	body := `<Response><CmdType>ConfigDownload</CmdType><SN>1</SN><DeviceID>D</DeviceID><Result>OK</Result></Response>`
	_, err := manscdp.ParseConfigDownloadResponse([]byte(body), manscdp.ConfigTypePictureMask)
	require.Error(t, err, "静默放过会让调用方把'平台没实现'误读成'设备没给'")
	var parseErr *manscdp.ConfigDownloadError
	require.ErrorAs(t, err, &parseErr)
	require.Equal(t, manscdp.ConfigErrorUnsupportedType, parseErr.Code)
}

func TestParseConfigDownloadResponseWithoutWantTypeSkipsTypeCheck(t *testing.T) {
	// 只想知道"设备回没回 OK"的场合：不传 wantType，不判缺席。
	body := `<Response><CmdType>ConfigDownload</CmdType><SN>1</SN><DeviceID>D</DeviceID><Result>OK</Result></Response>`
	result, err := manscdp.ParseConfigDownloadResponse([]byte(body), "")
	require.NoError(t, err)
	require.Equal(t, "OK", result.Result)
	require.Nil(t, result.VideoParamItems())
}

func TestParseConfigDownloadResponseForChecksCorrelation(t *testing.T) {
	expectation := manscdp.ConfigDownloadExpectation{SN: 7, DeviceID: videoParamDeviceID}
	_, err := manscdp.ParseConfigDownloadResponseFor([]byte(configDownloadTwoStreams), expectation, manscdp.ConfigTypeVideoParamAttribute)
	require.NoError(t, err)

	_, err = manscdp.ParseConfigDownloadResponseFor([]byte(configDownloadTwoStreams),
		manscdp.ConfigDownloadExpectation{SN: 99}, manscdp.ConfigTypeVideoParamAttribute)
	require.Error(t, err)
	var parseErr *manscdp.ConfigDownloadError
	require.ErrorAs(t, err, &parseErr)
	require.Equal(t, manscdp.ConfigErrorCorrelation, parseErr.Code, "SN 对不上说明答的不是我们问的那件事")

	_, err = manscdp.ParseConfigDownloadResponseFor([]byte(configDownloadTwoStreams),
		manscdp.ConfigDownloadExpectation{SN: 7, DeviceID: "other"}, manscdp.ConfigTypeVideoParamAttribute)
	require.Error(t, err)
}

// ---- 取值域辅助 ----

func TestIsValidResolution(t *testing.T) {
	for _, valid := range []string{"1", "2", "3", "4", "5", "6", "1920x1080", "640x480", " 1280x720 "} {
		require.True(t, manscdp.IsValidResolution(valid), "%q 应合法", valid)
	}
	for _, invalid := range []string{"", "0", "7", "720P", "1920*1080", "1920X1080", "x1080", "1920x", "0x480", "-1x480"} {
		require.False(t, manscdp.IsValidResolution(invalid), "%q 应非法", invalid)
	}
}

func TestConfigTypeConstantsCoverBothGenerations(t *testing.T) {
	// 2016 只有 4 个配置类型；后 8 个是 2022 新增。这条测试的价值在于：
	// 谁想把某个 2022 类型挪进"两版都有"那一组，会先在这里被拦一下。
	generation2016 := []string{
		manscdp.ConfigTypeBasicParam, manscdp.ConfigTypeVideoParamOpt,
		manscdp.ConfigTypeSVACEncodeConfig, manscdp.ConfigTypeSVACDecodeConfig,
	}
	generation2022 := []string{
		manscdp.ConfigTypeVideoParamAttribute, manscdp.ConfigTypeVideoRecordPlan,
		manscdp.ConfigTypeVideoAlarmRecord, manscdp.ConfigTypePictureMask,
		manscdp.ConfigTypeFrameMirror, manscdp.ConfigTypeAlarmReport,
		manscdp.ConfigTypeOSDConfig, manscdp.ConfigTypeSnapShotConfig,
	}
	seen := map[string]bool{}
	for _, name := range append(append([]string{}, generation2016...), generation2022...) {
		require.False(t, seen[name], "配置类型名重复: %s", name)
		require.NotEmpty(t, name)
		seen[name] = true
	}
	require.Len(t, seen, 12)
}
