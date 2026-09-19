package manscdp

import (
	"strconv"
	"strings"
	"testing"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

// 本文件的 golden 报文**逐字抄自模拟器侧的 render 实现**（`uvp-gb28181-sim` 的
// `BasicParamConfig.render` / `PictureMaskConfig.render` / …）。这是刻意的：
// 解析器的锚点必须是「现场设备真的会发什么」，不是「我们以为设备会发什么」。
// 两侧各写一遍形态而互不校验，是这类协议族最容易出现的假通过。

// configReadResponse 包一条 A.2.6.9 应答。用 UTF-8 声明：解码器的声明优先规则里
// UTF-8 是直通分支，本文件要测的是**结构**而不是字符集（字符集由 I-1 的用例覆盖）。
func configReadResponse(sn int, deviceID, blocks string) []byte {
	return configReadResponseWithDeclaration("UTF-8", sn, deviceID, blocks)
}

// configReadResponseWithDeclaration 是带声明字符集的版本，给 round-trip 用。
//
// ⛔ 信封自身的文本是纯 ASCII，而 ASCII 是 GB2312/GB18030 的真子集 —— 所以
// 「声明写成 GB2312 + 塞进 GB2312 编码的块」得到的是一条**字符集自洽**的报文。
// 反过来做（GB2312 字节塞进 UTF-8 信封）解析器会按声明直通 UTF-8，在中文上炸
// `invalid UTF-8` —— 那是**测试构造错**，不是解析器的错（本用例第一版就这么错的）。
func configReadResponseWithDeclaration(declaration string, sn int, deviceID, blocks string) []byte {
	return []byte("<?xml version=\"1.0\" encoding=\"" + declaration + "\"?>\r\n" +
		"<Response>\r\n<CmdType>ConfigDownload</CmdType>\r\n" +
		"<SN>" + strconv.Itoa(sn) + "</SN>\r\n<DeviceID>" + deviceID + "</DeviceID>\r\n" +
		"<Result>OK</Result>\r\n" + blocks + "</Response>\r\n")
}

// controlMessageParts 从一条 Control 报文里取出它的声明字符集与「块序列」部分，
// 供 round-trip 复用同一个解析入口。
//
// ⛔ 不要用 TrimPrefix("<Control>\r\n") / TrimSuffix("</Control>\r\n") 剥壳：
// 线格式由 xml.Marshal 产出，**除声明后那个 CRLF 外通篇没有换行**，带换行的前后缀
// 永远匹配不上。匹配不上的后果不是报错而是**静默**：带 <Control> 外壳的整段被塞进
// <Response>，解析器忽略未知的外壳元素 ⇒ 八块全部判缺席，用例"通过"而什么都没测。
//
// 可靠的切法是找顶层 </DeviceID>：Control 的块序列恒在其后、</Control> 之前。
func controlMessageParts(t *testing.T, body []byte) (declaration, blocks string) {
	t.Helper()
	text := string(body)
	declaration = "UTF-8"
	if start := strings.Index(text, `encoding="`); start >= 0 {
		rest := text[start+len(`encoding="`):]
		if end := strings.Index(rest, `"`); end >= 0 {
			declaration = rest[:end]
		}
	}
	opened := strings.Index(text, "</DeviceID>")
	closed := strings.LastIndex(text, "</Control>")
	if opened < 0 || closed < 0 || closed < opened {
		t.Fatalf("Control 报文缺少顶层 </DeviceID> 或 </Control>:\n%s", text)
	}
	return declaration, text[opened+len("</DeviceID>") : closed]
}

const testDeviceID = "34020000001320000010"

// TestParseDeviceConfigReadResponse_AllBlocks 一条应答带齐 8 块 ——
// 这是「一条报文可带多个配置类型」的标准形态（A.2.4.7 / A.2.6.9）。
func TestParseDeviceConfigReadResponse_AllBlocks(t *testing.T) {
	body := configReadResponse(7, testDeviceID, strings.Join([]string{
		"<BasicParam>\n<Name>东门球机</Name>\n<Expiration>3600</Expiration>\n" +
			"<HeartBeatInterval>60</HeartBeatInterval>\n<HeartBeatCount>3</HeartBeatCount>\n</BasicParam>\n",
		"<VideoParamOpt>\n<DownloadSpeed>1/2/4</DownloadSpeed>\n<Resolution>1/2/3/4/5/6</Resolution>\n</VideoParamOpt>\n",
		"<VideoRecordPlan>\n<RecordEnable>1</RecordEnable>\n<RecordScheduleSumNum>1</RecordScheduleSumNum>\n" +
			"<RecordSchedule>\n<WeekDayNum>1</WeekDayNum>\n<TimeSegmentSumNum>1</TimeSegmentSumNum>\n" +
			"<TimeSegment><StartHour>8</StartHour><StartMin>0</StartMin><StartSec>0</StartSec>" +
			"<StopHour>12</StopHour><StopMin>30</StopMin><StopSec>0</StopSec></TimeSegment>\n" +
			"</RecordSchedule>\n<StreamNumber>0</StreamNumber>\n</VideoRecordPlan>\n",
		"<VideoAlarmRecord>\n<RecordEnable>1</RecordEnable>\n<RecordTime>30</RecordTime>\n" +
			"<PreRecordTime>5</PreRecordTime>\n<StreamNumber>0</StreamNumber>\n</VideoAlarmRecord>\n",
		"<PictureMask>\n<On>1</On>\n<SumNum>2</SumNum>\n<RegionList Num=\"2\">\n" +
			"<Item><Seq>1</Seq><Point>20,30,50,60</Point></Item>\n" +
			"<Item><Seq>2</Seq><Point>100,120,200,220</Point></Item>\n</RegionList>\n</PictureMask>\n",
		"<FrameMirror>2</FrameMirror>\n",
		"<AlarmReport>\n<MotionDetection>1</MotionDetection>\n<FieldDetection>0</FieldDetection>\n</AlarmReport>\n",
		"<OSDConfig>\n<Length>1920</Length>\n<Width>1080</Width>\n<TimeX>10</TimeX>\n<TimeY>10</TimeY>\n" +
			"<TimeEnable>1</TimeEnable>\n<TextEnable>1</TextEnable>\n<SumNum>1</SumNum>\n" +
			"<Item><Text>通道1</Text><X>10</X><Y>34</Y></Item>\n</OSDConfig>\n",
	}, ""))

	result, err := ParseDeviceConfigReadResponseFor(body, ConfigDownloadExpectation{SN: 7, DeviceID: testDeviceID})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	got := result.Blocks.PresentConfigTypes()
	want := []string{
		ConfigTypeBasicParam, ConfigTypeVideoParamOpt, ConfigTypeVideoRecordPlan,
		ConfigTypeVideoAlarmRecord, ConfigTypePictureMask, ConfigTypeFrameMirror,
		ConfigTypeAlarmReport, ConfigTypeOSDConfig,
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("出现的配置类型不符\n got=%v\nwant=%v", got, want)
	}

	if bp := result.Blocks.BasicParam; bp == nil || bp.Name == nil || *bp.Name != "东门球机" ||
		bp.Expiration == nil || *bp.Expiration != 3600 || bp.HeartBeatInterval == nil ||
		*bp.HeartBeatInterval != 60 || bp.HeartBeatCount == nil || *bp.HeartBeatCount != 3 {
		t.Fatalf("BasicParam 解析不符: %+v", bp)
	}
	if opt := result.Blocks.VideoParamOpt; opt == nil ||
		strings.Join(splitSlashCodes(opt.Resolution), ",") != "1,2,3,4,5,6" {
		t.Fatalf("VideoParamOpt 解析不符: %+v", opt)
	}
	plan := result.Blocks.VideoRecordPlan
	if plan == nil || plan.RecordEnable != 1 || plan.StreamNumber != 0 || len(plan.Schedules) != 1 {
		t.Fatalf("VideoRecordPlan 外层不符: %+v", plan)
	}
	if schedule := plan.Schedules[0]; schedule.WeekDayNum != 1 || len(schedule.Segments) != 1 {
		t.Fatalf("RecordSchedule 不符: %+v", schedule)
	} else if segment := schedule.Segments[0]; segment.StartHour != 8 || segment.StopMin != 30 {
		t.Fatalf("TimeSegment 不符: %+v", segment)
	}
	alarm := result.Blocks.VideoAlarmRecord
	if alarm == nil || alarm.RecordEnable != 1 || alarm.StreamNumber != 0 ||
		alarm.RecordTime == nil || *alarm.RecordTime != 30 ||
		alarm.PreRecordTime == nil || *alarm.PreRecordTime != 5 {
		t.Fatalf("VideoAlarmRecord 解析不符: %+v", alarm)
	}
	mask := result.Blocks.PictureMask
	if mask == nil || mask.On != 1 || len(mask.Regions) != 2 {
		t.Fatalf("PictureMask 外层不符: %+v", mask)
	}
	if region := mask.Regions[0]; region.PointLiteral() != "20,30,50,60" {
		// ⛔ 这一条是「Point 是左上+右下、不是 x/y/w/h」的回归锚点。
		t.Fatalf("PictureMask 区域坐标不符（Point 应为 lx,ly,rx,ry）: %+v", region)
	}
	if mirror := result.Blocks.FrameMirror; mirror == nil || mirror.Value != FrameMirrorUpDown {
		t.Fatalf("FrameMirror 解析不符（simpleType 元素体应为整数）: %+v", mirror)
	}
	report := result.Blocks.AlarmReport
	if report == nil || report.MotionDetection != AlarmReportOn || report.FieldDetection != AlarmReportOff {
		t.Fatalf("AlarmReport 解析不符: %+v", report)
	}
	osd := result.Blocks.OSDConfig
	if osd == nil || osd.Length != 1920 || osd.Width != 1080 || osd.TimeX != 10 {
		t.Fatalf("OSDConfig 外层不符: %+v", osd)
	}
	if len(osd.Items) != 1 || osd.Items[0].Text != "通道1" || osd.Items[0].Y != 34 {
		t.Fatalf("OSD Item 不符: %+v", osd.Items)
	}
	if osd.TimeType != nil {
		t.Fatalf("TimeType 缺席时应为 nil（不是 0）: %v", *osd.TimeType)
	}
	if osd.SumNum() != 1 {
		t.Fatalf("SumNum 应等于实到条数: %d", osd.SumNum())
	}
}

// TestParseDeviceConfigReadResponse_AbsentBlocksStayNil 缺席的块必须保持 nil。
//
// ⛔ 这是全族最关键的判据：设备"没回这一块" = **该设备不支持这个配置类型**
// （A-5 §十④ 那条最可靠的判据）。若把它解析成"空块"，平台就再也分不出
// 「设备不支持」与「设备支持但没配」——前者要换设备，后者只要点一次下发。
func TestParseDeviceConfigReadResponse_AbsentBlocksStayNil(t *testing.T) {
	body := configReadResponse(3, testDeviceID,
		"<FrameMirror>0</FrameMirror>\n")
	result, err := ParseDeviceConfigReadResponse(body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if result.Blocks.FrameMirror == nil {
		t.Fatal("FrameMirror 在场却解析成 nil")
	}
	if result.Blocks.Has(ConfigTypeOSDConfig) || result.Blocks.OSDConfig != nil {
		t.Fatal("未出现的 OSDConfig 必须保持 nil")
	}
	if got := result.Blocks.PresentConfigTypes(); len(got) != 1 || got[0] != ConfigTypeFrameMirror {
		t.Fatalf("只有一块时应只报一块: %v", got)
	}
}

// TestParseDeviceConfigReadResponse_EmptyRegionListIsNotAbsent PictureMask 带开关、
// 无区域 —— 「在场但只有一个空列表」与「整块缺席」是两件事。
func TestParseDeviceConfigReadResponse_EmptyRegionListIsNotAbsent(t *testing.T) {
	body := configReadResponse(4, testDeviceID, "<PictureMask>\n<On>0</On>\n<SumNum>0</SumNum>\n</PictureMask>\n")
	result, err := ParseDeviceConfigReadResponse(body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	mask := result.Blocks.PictureMask
	if mask == nil {
		t.Fatal("PictureMask 在场却解析成 nil")
	}
	if mask.On != PictureMaskOff || len(mask.Regions) != 0 {
		t.Fatalf("空区域列表解析不符: %+v", mask)
	}
}

// TestBuildDeviceConfigBlocks_RoundTrip 平台构建 → 再解析，逐字段相等。
//
// ⛔ 覆盖的是"平台发出去的东西，平台自己的解析器认得"这一半；
// 另一半（设备认不认）由模拟器侧的 `DeviceControlDispatcherTest` 守着。
func TestBuildDeviceConfigBlocks_RoundTrip(t *testing.T) {
	profile := protocol.ProfileFor(protocol.Version2016)
	timeType := osdTimeTypeCNDate
	recordTime, preRecordTime := 30, 5
	name := "东门球机"

	blocks := DeviceConfigBlocks{
		BasicParam: &BasicParamBlock{Name: &name, Expiration: intPtr(3600), HeartBeatInterval: intPtr(60)},
		PictureMask: &PictureMaskBlock{On: PictureMaskOn, Regions: []PictureMaskRegion{
			{Seq: 1, Left: 20, Top: 30, Right: 50, Bottom: 60},
		}},
		FrameMirror: &FrameMirrorBlock{Value: FrameMirrorLeftRight},
		AlarmReport: &AlarmReportBlock{MotionDetection: AlarmReportOn, FieldDetection: AlarmReportOff},
		VideoAlarmRecord: &VideoAlarmRecordBlock{
			RecordEnable: AlarmReportOn, RecordTime: &recordTime,
			PreRecordTime: &preRecordTime, StreamNumber: 0,
		},
		VideoRecordPlan: &VideoRecordPlanBlock{
			RecordEnable: AlarmReportOn, StreamNumber: 0,
			Schedules: []RecordSchedule{{WeekDayNum: 1, Segments: []RecordTimeSegment{
				{StartHour: 8, StartMin: 0, StartSec: 0, StopHour: 12, StopMin: 30, StopSec: 0},
			}}},
		},
		OSDConfig: &OSDConfigBlock{
			Length: 1920, Width: 1080, TimeX: 10, TimeY: 10,
			TimeEnable: AlarmReportOn, TimeType: &timeType, TextEnable: AlarmReportOn,
			Items: []OSDTextItem{{Text: "通道1", X: 10, Y: 34}},
		},
	}

	body, err := BuildDeviceConfigBlocksWithProfile(profile, testDeviceID, 11, blocks)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "<CmdType>DeviceConfig</CmdType>") {
		t.Fatalf("CmdType 不是 DeviceConfig:\n%s", text)
	}
	// ⛔ FrameMirror 必须是 simpleType 形态（元素体是整数、无子元素）。
	if !strings.Contains(text, "<FrameMirror>1</FrameMirror>") {
		t.Fatalf("FrameMirror 不是 simpleType 形态:\n%s", text)
	}
	// ⛔ PictureMask 的 RegionList 必须带 Num **属性**。
	// 值取 4（不是 1）：`On=1` 的报文是**全量声明**，未给的槽位补零面积 —— 理由见
	// `TestPictureMaskWire_DeclaresClearedSlotsToDeleteThem`。
	if !strings.Contains(text, "<RegionList Num=\"4\">") {
		t.Fatalf("RegionList 的 Num 应为属性、且是全量声明的 4 条:\n%s", text)
	}

	// 把 Control 换成 Response 骨架，复用同一个解析入口做 round-trip。
	// 声明字符集从构建结果里取回来 —— 构建走的是 GB2312 profile，
	// 信封必须跟着走 GB2312，否则中文过不了转码。
	declaration, blocksXML := controlMessageParts(t, body)
	response := configReadResponseWithDeclaration(declaration, 11, testDeviceID, blocksXML)
	parsed, err := ParseDeviceConfigReadResponse(response)
	if err != nil {
		t.Fatalf("round-trip 解析失败: %v\n%s", err, text)
	}
	if got := strings.Join(parsed.Blocks.PresentConfigTypes(), ","); got !=
		strings.Join(blocks.PresentConfigTypes(), ",") {
		t.Fatalf("round-trip 配置类型不符\n got=%s\nwant=%s", got, strings.Join(blocks.PresentConfigTypes(), ","))
	}
	if parsed.Blocks.FrameMirror.Value != FrameMirrorLeftRight {
		t.Fatalf("FrameMirror round-trip 不符: %+v", parsed.Blocks.FrameMirror)
	}
	if parsed.Blocks.PictureMask.Regions[0].PointLiteral() != "20,30,50,60" {
		t.Fatalf("PictureMask round-trip 不符: %+v", parsed.Blocks.PictureMask.Regions[0])
	}
	if parsed.Blocks.OSDConfig.TimeType == nil || *parsed.Blocks.OSDConfig.TimeType != osdTimeTypeCNDate {
		t.Fatalf("OSD TimeType round-trip 不符: %+v", parsed.Blocks.OSDConfig.TimeType)
	}
	if parsed.Blocks.VideoRecordPlan.Schedules[0].Segments[0].StopMin != 30 {
		t.Fatalf("VideoRecordPlan round-trip 不符: %+v", parsed.Blocks.VideoRecordPlan)
	}
	if parsed.Blocks.BasicParam.Name == nil || *parsed.Blocks.BasicParam.Name != "东门球机" {
		t.Fatalf("BasicParam.Name round-trip 不符（含中文，字符集须真转码）: %+v", parsed.Blocks.BasicParam.Name)
	}
}

// TestBuildDeviceConfigBlocks_RejectsEmpty 空容器拒发：空报文没有语义。
func TestBuildDeviceConfigBlocks_RejectsEmpty(t *testing.T) {
	if _, err := BuildDeviceConfigBlocksWithProfile(
		protocol.ProfileFor(protocol.Version2016), testDeviceID, 1, DeviceConfigBlocks{}); err == nil {
		t.Fatal("空配置块应被拒发")
	}
}

// TestValidateDeviceConfigBlocks_RejectsOutOfRange 取值域在**发出前**收口。
//
// ⛔ 严格发 / 宽松收：平台自己发出的值乱来，对端会静默丢弃或当 0 处理，
// 而这种错在回读对账里只表现为"设备没照做"，归因成本极高。
func TestValidateDeviceConfigBlocks_RejectsOutOfRange(t *testing.T) {
	cases := []struct {
		name   string
		blocks DeviceConfigBlocks
	}{
		{"FrameMirror 越界", DeviceConfigBlocks{FrameMirror: &FrameMirrorBlock{Value: 7}}},
		{"AlarmReport 开关非法", DeviceConfigBlocks{AlarmReport: &AlarmReportBlock{MotionDetection: 3}}},
		{"PictureMask 坐标倒置", DeviceConfigBlocks{PictureMask: &PictureMaskBlock{
			On: PictureMaskOn, Regions: []PictureMaskRegion{{Seq: 1, Left: 60, Top: 30, Right: 50, Bottom: 60}}}}},
		{"PictureMask 区域超上限", DeviceConfigBlocks{PictureMask: &PictureMaskBlock{
			On: PictureMaskOn, Regions: []PictureMaskRegion{
				{Seq: 1}, {Seq: 2}, {Seq: 3}, {Seq: 4}, {Seq: 1}}}}},
		{"VideoRecordPlan 星期越界", DeviceConfigBlocks{VideoRecordPlan: &VideoRecordPlanBlock{
			Schedules: []RecordSchedule{{WeekDayNum: 8}}}}},
		{"VideoRecordPlan 时段秒越界", DeviceConfigBlocks{VideoRecordPlan: &VideoRecordPlanBlock{
			Schedules: []RecordSchedule{{WeekDayNum: 1, Segments: []RecordTimeSegment{{StartSec: 60}}}}}}},
		{"BasicParam 全空", DeviceConfigBlocks{BasicParam: &BasicParamBlock{}}},
		{"VideoAlarmRecord 开关非法", DeviceConfigBlocks{
			VideoAlarmRecord: &VideoAlarmRecordBlock{RecordEnable: 5}}},
		{"OSD 窗口非正", DeviceConfigBlocks{OSDConfig: &OSDConfigBlock{
			Length: 0, Width: 1080, TimeEnable: 1, TextEnable: 1}}},
		{"OSD 文本超长", DeviceConfigBlocks{OSDConfig: &OSDConfigBlock{
			Length: 1920, Width: 1080, TimeEnable: 1, TextEnable: 1,
			Items: []OSDTextItem{{Text: strings.Repeat("字", maxOSDTextLength+1)}}}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := ValidateDeviceConfigBlocks(testCase.blocks); err == nil {
				t.Fatal("越界取值应被拒")
			}
		})
	}
}

// TestValidateDeviceConfigBlocks_AcceptsBoundary 边界值必须放行 ——
// 收严了会把合法配置判成非法（本仓为"门禁误伤合法操作"付过代价）。
func TestValidateDeviceConfigBlocks_AcceptsBoundary(t *testing.T) {
	allRegions := make([]PictureMaskRegion, 0, MaxPictureMaskRegions)
	for seq := 1; seq <= MaxPictureMaskRegions; seq++ {
		allRegions = append(allRegions, PictureMaskRegion{Seq: seq, Left: 0, Top: 0, Right: 0, Bottom: 0})
	}
	textItems := make([]OSDTextItem, 0, maxOSDTextItems)
	for index := 0; index < maxOSDTextItems; index++ {
		textItems = append(textItems, OSDTextItem{Text: strings.Repeat("字", maxOSDTextLength)})
	}
	blocks := DeviceConfigBlocks{
		FrameMirror: &FrameMirrorBlock{Value: FrameMirrorCenter},
		AlarmReport: &AlarmReportBlock{MotionDetection: AlarmReportOff, FieldDetection: AlarmReportOn},
		PictureMask: &PictureMaskBlock{On: PictureMaskOn, Regions: allRegions},
		OSDConfig: &OSDConfigBlock{
			Length: 1, Width: 1, TimeEnable: 1, TextEnable: 0, Items: textItems,
		},
		VideoRecordPlan: &VideoRecordPlanBlock{RecordEnable: 0, Schedules: []RecordSchedule{
			{WeekDayNum: 7, Segments: []RecordTimeSegment{
				{StartHour: 0, StartMin: 0, StartSec: 0, StopHour: 23, StopMin: 59, StopSec: 59}}},
		}},
	}
	if err := ValidateDeviceConfigBlocks(blocks); err != nil {
		t.Fatalf("边界值应放行: %v", err)
	}
}

// buildMaskBody 只带 PictureMask 构建一次下发报文。
func buildMaskBody(t *testing.T, mask *PictureMaskBlock) []byte {
	t.Helper()
	body, err := BuildDeviceConfigBlocksWithProfile(
		protocol.ProfileFor(protocol.Version2016), testDeviceID, 7, DeviceConfigBlocks{PictureMask: mask})
	if err != nil {
		t.Fatalf("构建 PictureMask 报文失败: %v", err)
	}
	return body
}

// compactXML 去掉换行与缩进，便于对报文做包含性断言。
func compactXML(body []byte) string {
	return strings.NewReplacer("\n", "", "\r", "", "\t", "").Replace(string(body))
}

// TestPictureMaskWire_ClearsResidualRegionsWhenDisabled ⛔ 停用必须**连区域一起清**。
//
// 真机（海康 IPC，2026-09-19）实证：`On=0` 而 `RegionList` 缺席时设备只关开关、
// 把区域原样留着，页面上表现为"遮挡框删不掉"，平台之后**再也抹不掉它**。
// 锚点：停用报文里必须出现 `Seq 1..4` 全部零面积，而不是一个 `RegionList` 都没有。
func TestPictureMaskWire_ClearsResidualRegionsWhenDisabled(t *testing.T) {
	cases := []struct {
		name    string
		regions []PictureMaskRegion
	}{
		{"带设备残留区域（用户现场形态）", []PictureMaskRegion{
			{Seq: 1, Left: 144, Top: 295, Right: 704, Bottom: 576}}},
		{"区域为空", nil},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			text := compactXML(buildMaskBody(t, &PictureMaskBlock{On: PictureMaskOff, Regions: testCase.regions}))
			if !strings.Contains(text, "<On>0</On>") {
				t.Fatalf("停用报文的 On 应为 0: %s", text)
			}
			if !strings.Contains(text, "<SumNum>4</SumNum>") {
				t.Fatalf("停用必须声明 %d 个清空占位: %s", MaxPictureMaskRegions, text)
			}
			if !strings.Contains(text, `RegionList Num="4"`) {
				t.Fatalf("停用必须带 RegionList，且 Num 是属性: %s", text)
			}
			if got := strings.Count(text, "<Point>"+PictureMaskClearPoint+"</Point>"); got != MaxPictureMaskRegions {
				t.Fatalf("清空占位应有 %d 条零面积，实际 %d: %s", MaxPictureMaskRegions, got, text)
			}
			// ⛔ 残留坐标**不得**出现在停用报文里 —— 出现就等于"把区域又写回去"。
			if strings.Contains(text, "144,295,704,576") {
				t.Fatalf("停用报文不得回写原区域: %s", text)
			}
		})
	}
}

// TestPictureMaskWire_KeepsRegionsWhenEnabled 启用时**真实区域照常逐条下发**，
// 不得被清空逻辑波及；未给的槽位补零面积（= 全量声明，见 [pictureMaskFullRegions]）。
//
// ⛔ 反向锚点：少了它，"停用清空"很容易被写成"一律清空" —— 那样"启用遮挡"会变成
// "启用 + 清空"，遮挡永远画不上，而且单看停用用例还是绿的。
func TestPictureMaskWire_KeepsRegionsWhenEnabled(t *testing.T) {
	text := compactXML(buildMaskBody(t, &PictureMaskBlock{On: PictureMaskOn, Regions: []PictureMaskRegion{
		{Seq: 1, Left: 10, Top: 20, Right: 30, Bottom: 40},
		{Seq: 2, Left: 50, Top: 60, Right: 70, Bottom: 80},
	}}))
	if !strings.Contains(text, "<On>1</On>") {
		t.Fatalf("启用报文的 On 应为 1: %s", text)
	}
	// 真实区域必须原样在报文里 —— 这是上面那条反向锚点的核心。
	for _, point := range []string{"10,20,30,40", "50,60,70,80"} {
		if !strings.Contains(text, "<Point>"+point+"</Point>") {
			t.Fatalf("启用报文缺少真实区域 %s: %s", point, text)
		}
	}
	if !strings.Contains(text, `<RegionList Num="4">`) || !strings.Contains(text, "<SumNum>4</SumNum>") {
		t.Fatalf("启用报文应是全量声明（4 条）: %s", text)
	}
	// 零面积**只应出现在未给的那两个槽位**上（数量也是锚点：铺多了就是把真实区域清掉了）。
	if got := strings.Count(text, "<Point>"+PictureMaskClearPoint+"</Point>"); got != MaxPictureMaskRegions-2 {
		t.Fatalf("零面积占位应只有未给的 %d 条，实际 %d: %s", MaxPictureMaskRegions-2, got, text)
	}
}

// TestPictureMaskWire_DeclaresClearedSlotsToDeleteThem ⛔「删掉一个遮挡区」必须能说出口。
//
// ⭐ 真机现场（2026-09-20，海康 DS-2DC2C040MY-DE，192.168.10.203）：设备里原有 `Seq 1..4`，
// 操作员在页面上删掉 Seq4 后下发，意图是 `regions=[1,2,3]`；前端会跳过全零槽位，报文于是
// **只有 3 条**。而设备把 `RegionList` 当**按 `Seq` 的增量补丁** —— **省略一个槽位等于
// "别碰它"**，实测回读 `Num="4"`、Seq4 原样还在：
//
//	| 设备里 | 平台发出 | 设备回读 |
//	| --- | --- | --- |
//	| Seq1..4 全有 | 只带 Seq1/2/3 | `Num="4"`，Seq4 **还在** ❌ |
//	| Seq1..4 全有 | Seq1/2/3 真实 + Seq4 零面积 | `Num="3"`，Seq4 **消失** ✅ |
//
// 一个根因两个症状：① 那块遮挡**删不掉**（`gb_ptz_operation` id=1520/1522 的下发意图是
// 3 条，设备端却没变化）；② 紧接着的自动回读报 `DEVICE_CONFIG_RECONCILE_MISMATCH`
// （界面上的「设备已接受命令，但值未生效」）。
//
// 锚点：被删掉的槽位必须以**显式零面积**出现，而且必须挂在**它自己的 Seq** 上
// （挂错 Seq 等于删错了另一块遮挡）。
func TestPictureMaskWire_DeclaresClearedSlotsToDeleteThem(t *testing.T) {
	text := compactXML(buildMaskBody(t, &PictureMaskBlock{On: PictureMaskOn, Regions: []PictureMaskRegion{
		{Seq: 1, Left: 286, Top: 396, Right: 419, Bottom: 547},
		{Seq: 2, Left: 560, Top: 50, Right: 694, Bottom: 351},
		{Seq: 3, Left: 89, Top: 132, Right: 307, Bottom: 335},
		// 操作员删掉的 Seq4 不在平台给出的区域里。
	}}))
	if !strings.Contains(text, "<Item><Seq>4</Seq><Point>"+PictureMaskClearPoint+"</Point></Item>") {
		t.Fatalf("被删掉的 Seq4 必须以显式零面积出现（否则设备原样保留，删不掉）: %s", text)
	}
	for _, point := range []string{"286,396,419,547", "560,50,694,351", "89,132,307,335"} {
		if !strings.Contains(text, "<Point>"+point+"</Point>") {
			t.Fatalf("保留的区域 %s 不得受影响: %s", point, text)
		}
	}

	// 同一根因的另一半：「启用 + 一个区域都没有」也得铺满 —— 否则"把设备里所有区域清掉"
	// 同样是省略，同样删不掉（见 buildPicture 放行 `On=1` 无区域的那条产品决定）。
	empty := compactXML(buildMaskBody(t, &PictureMaskBlock{On: PictureMaskOn}))
	if got := strings.Count(empty, "<Point>"+PictureMaskClearPoint+"</Point>"); got != MaxPictureMaskRegions {
		t.Fatalf("启用且不带区域时应铺满 %d 条零面积，实际 %d: %s", MaxPictureMaskRegions, got, empty)
	}
	if !strings.Contains(empty, `<RegionList Num="4">`) {
		t.Fatalf("启用且不带区域时也必须带 RegionList: %s", empty)
	}
}

// TestPictureMaskClearPointIsLegalAndParseable 清空占位必须**既合法、又能解析回来**。
//
// ⛔ 两个反向约束都要钉住：
//   - `validate()` 放行零面积 —— 否则将来谁把清空逻辑挪到校验之前，停用就整个发不出去；
//   - 回读解析把 `0,0,0,0` 认成一条零面积区域（不是 -1 哨兵），
//     否则设备真回了零面积时平台会显示成坐标解析失败。
func TestPictureMaskClearPointIsLegalAndParseable(t *testing.T) {
	if err := ValidateDeviceConfigBlocks(DeviceConfigBlocks{
		PictureMask: &PictureMaskBlock{On: PictureMaskOff, Regions: pictureMaskClearRegions()},
	}); err != nil {
		t.Fatalf("零面积占位必须是合法取值: %v", err)
	}
	region, ok := parsePictureMaskPoint(1, PictureMaskClearPoint)
	if !ok {
		t.Fatalf("%q 应能解析为区域", PictureMaskClearPoint)
	}
	if region.Left != 0 || region.Top != 0 || region.Right != 0 || region.Bottom != 0 {
		t.Fatalf("清空占位解析结果应为零面积: %+v", region)
	}
	if region.Left == pictureMaskUnparsable {
		t.Fatal("零面积不得与解析失败哨兵撞车")
	}
}

// TestParseDeviceConfigReadResponse_RejectsWrongCmdType 骨架校验不能松。
func TestParseDeviceConfigReadResponse_RejectsWrongCmdType(t *testing.T) {
	body := []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\r\n<Response>\r\n" +
		"<CmdType>DeviceConfig</CmdType>\r\n<SN>1</SN>\r\n<DeviceID>" + testDeviceID + "</DeviceID>\r\n" +
		"<Result>OK</Result>\r\n</Response>\r\n")
	if _, err := ParseDeviceConfigReadResponse(body); err == nil {
		t.Fatal("CmdType 不符应报错")
	}
}

// TestParseDeviceConfigReadResponseFor_RejectsCorrelationMismatch
// ⛔ SN / DeviceID 对不上必须报错：平台按**通道编码**查，设备回错编码的后果是
// 整条应答被判"不属于本次操作"，平台永远停在 never_read，且两侧日志都不报错。
func TestParseDeviceConfigReadResponseFor_RejectsCorrelationMismatch(t *testing.T) {
	body := configReadResponse(7, testDeviceID, "<FrameMirror>0</FrameMirror>\n")
	if _, err := ParseDeviceConfigReadResponseFor(body,
		ConfigDownloadExpectation{SN: 8, DeviceID: testDeviceID}); err == nil {
		t.Fatal("SN 不符应报错")
	}
	if _, err := ParseDeviceConfigReadResponseFor(body,
		ConfigDownloadExpectation{SN: 7, DeviceID: "34020000001320000099"}); err == nil {
		t.Fatal("DeviceID 不符应报错")
	}
}

// TestUnparsableSentinelsAreNotLegalValues 哨兵值不能与合法取值撞车。
//
// ⛔ 这是"设备回了垃圾"与"设备把功能关了"必须分得开的底线：
// 用 0 当哨兵会把不合规应答显示成"镜像已关闭"。
func TestUnparsableSentinelsAreNotLegalValues(t *testing.T) {
	if FrameMirrorOff <= frameMirrorUnparsable {
		t.Fatal("FrameMirror 哨兵值不得落在合法值域内")
	}
	if AlarmReportOff <= alarmReportUnparsable {
		t.Fatal("AlarmReport 哨兵值不得落在合法值域内")
	}
	block := (&frameMirrorWire{Value: "abc"}).toBlock()
	if block == nil || !block.Unparsable() {
		t.Fatalf("非整数应答应标为 Unparsable: %+v", block)
	}
}

func intPtr(value int) *int { return &value }

// TestDeviceConfigBlocks_BlockMappingIsSingleSourced 钉住「ConfigType 名 ↔ 结构字段」
// 只有**一份**映射（DeviceConfigBlocks.Block），Has / PresentConfigTypes 都由它派生。
//
// ⛔ 为什么值得一条用例：这类"同一件事写两份 switch"的漂移在本仓已发生三次
// （最出名的一次是设备侧的 accepts() 与 handle() 两道门禁）。漂移的表现是
// **某个配置类型永远取不到值**，而两侧日志都不报错。
func TestDeviceConfigBlocks_BlockMappingIsSingleSourced(t *testing.T) {
	timeType := osdTimeTypeCNDate
	full := DeviceConfigBlocks{
		BasicParam:       &BasicParamBlock{},
		VideoParamOpt:    &VideoParamOptBlock{},
		VideoRecordPlan:  &VideoRecordPlanBlock{},
		VideoAlarmRecord: &VideoAlarmRecordBlock{},
		PictureMask:      &PictureMaskBlock{},
		FrameMirror:      &FrameMirrorBlock{},
		AlarmReport:      &AlarmReportBlock{},
		OSDConfig:        &OSDConfigBlock{TimeType: &timeType},
	}

	if got := strings.Join(full.PresentConfigTypes(), ","); got != strings.Join(ConfigTypeOrder, ",") {
		t.Fatalf("PresentConfigTypes 必须严格按 ConfigTypeOrder 排序\n got=%s\nwant=%s",
			got, strings.Join(ConfigTypeOrder, ","))
	}
	for _, configType := range ConfigTypeOrder {
		block, present := full.Block(configType)
		if !present || block == nil {
			t.Fatalf("Block(%q) 取不到结构字段 —— 映射漏了这一项", configType)
		}
		if !full.Has(configType) {
			t.Fatalf("Has(%q) 与 Block(%q) 不一致", configType, configType)
		}
	}

	// 只有 8 组（VideoParamAttribute 走 video_param.go 的独立通道）。
	if len(ConfigTypeOrder) != 8 {
		t.Fatalf("ConfigTypeOrder 应有 8 项，实际 %d 项: %v", len(ConfigTypeOrder), ConfigTypeOrder)
	}

	// ⛔ 未知类型必须判缺席。`VideoParamAttribute` 是**有意的**未知项：
	// 它有自己的通道，通用容器不该悄悄把它也认下来（那会变成两条通道都写它）。
	for _, unknown := range []string{"VideoParamAttribute", "SnapShotConfig", "", "  "} {
		if block, present := full.Block(unknown); present || block != nil {
			t.Fatalf("Block(%q) 不该认下不属于通用容器的类型: %+v", unknown, block)
		}
	}
}

// TestParseDeviceConfigReadResponse_MultipleStreamBlocks 多码流设备的一条应答里，
// `VideoRecordPlan` / `VideoAlarmRecord` 会**按码流各一块**（两块都带 StreamNumber）。
//
// ⛔ 这条用例钉住的是「不许静默覆盖」：用单指针接会让**后到的块盖掉先到的块**，
// 平台于是把子码流的录像计划当成主码流的展示出来，而两侧日志都不报错 —— 属于最难发现
// 的一类错。正确行为是选**最小 StreamNumber（主码流）**，并把剩余块数记进
// AdditionalStreams，让上层看得见「还有别的码流没展开」。
//
// ⛔⛔ 报文顺序必须是**主码流在前、子码流在后**：反过来写（子码流在前）会让"后者覆盖"
// 只有一种可能的结果，与正确实现同解 —— 用例会红绿不分。这条是变异自检抓出来的。
func TestParseDeviceConfigReadResponse_MultipleStreamBlocks(t *testing.T) {
	body := configReadResponse(9, testDeviceID, strings.Join([]string{
		// 主码流 0：先出现。任何"取最后一块"的实现都会把它丢掉。
		"<VideoRecordPlan><RecordEnable>0</RecordEnable><RecordScheduleSumNum>0</RecordScheduleSumNum>" +
			"<StreamNumber>0</StreamNumber></VideoRecordPlan>",
		// 子码流 1：后出现，内容与主码流不同。
		"<VideoRecordPlan><RecordEnable>1</RecordEnable><RecordScheduleSumNum>1</RecordScheduleSumNum>" +
			"<RecordSchedule><WeekDayNum>1</WeekDayNum><TimeSegmentSumNum>1</TimeSegmentSumNum>" +
			"<TimeSegment><StartHour>8</StartHour><StartMin>0</StartMin><StartSec>0</StartSec>" +
			"<StopHour>12</StopHour><StopMin>0</StopMin><StopSec>0</StopSec></TimeSegment>" +
			"</RecordSchedule><StreamNumber>1</StreamNumber></VideoRecordPlan>",
		"<VideoAlarmRecord><RecordEnable>0</RecordEnable><StreamNumber>0</StreamNumber></VideoAlarmRecord>",
		"<VideoAlarmRecord><RecordEnable>1</RecordEnable><RecordTime>30</RecordTime>" +
			"<StreamNumber>2</StreamNumber></VideoAlarmRecord>",
	}, ""))

	result, err := ParseDeviceConfigReadResponseFor(body, ConfigDownloadExpectation{SN: 9, DeviceID: testDeviceID})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}

	plan := result.Blocks.VideoRecordPlan
	if plan == nil {
		t.Fatal("VideoRecordPlan 在场却解析成 nil")
	}
	if plan.StreamNumber != 0 || plan.RecordEnable != AlarmReportOff || len(plan.Schedules) != 0 {
		t.Fatalf("应选中主码流（StreamNumber=0）那一块，实际取了: %+v", plan)
	}
	if plan.AdditionalStreams != 1 {
		t.Fatalf("另有 1 个码流的计划应被计数而不是丢弃: %+v", plan)
	}

	alarm := result.Blocks.VideoAlarmRecord
	if alarm == nil {
		t.Fatal("VideoAlarmRecord 在场却解析成 nil")
	}
	if alarm.StreamNumber != 0 || alarm.RecordEnable != AlarmReportOff || alarm.RecordTime != nil {
		t.Fatalf("应选中主码流（StreamNumber=0）那一块，实际取了: %+v", alarm)
	}
	if alarm.AdditionalStreams != 1 {
		t.Fatalf("另有 1 个码流的报警录像应被计数: %+v", alarm)
	}
}

// TestParseDeviceConfigReadResponse_SingleStreamBlockHasNoExtras 单块时不应虚报
// AdditionalStreams —— 否则前端会平白提示「还有别的码流未展开」。
func TestParseDeviceConfigReadResponse_SingleStreamBlockHasNoExtras(t *testing.T) {
	body := configReadResponse(10, testDeviceID,
		"<VideoRecordPlan><RecordEnable>1</RecordEnable><RecordScheduleSumNum>0</RecordScheduleSumNum>"+
			"<StreamNumber>0</StreamNumber></VideoRecordPlan>")
	result, err := ParseDeviceConfigReadResponse(body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if plan := result.Blocks.VideoRecordPlan; plan == nil || plan.AdditionalStreams != 0 {
		t.Fatalf("单块应答不应报 AdditionalStreams: %+v", plan)
	}
}
