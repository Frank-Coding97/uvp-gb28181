package manscdp

import (
	"encoding/json"
	"strings"
	"testing"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

// hikOSDConfigBlock 是**海康 DS-2DC2C040MY-DE（GB28181-2022）真机应答里的
// `<OSDConfig>` 原文**，2026-09-19 抓包解密所得，逐字节照抄 —— 别顺手"整理"缩进或省略
// 零值元素，这一段的每个 0 都是锚点的一部分。
//
// ⛔ 值得注意的是 `<TextEnable>0</TextEnable>`：元素**在场且为 0**。
// 它和"元素缺席"是两件事，而修复前平台把两者都改写成 1。
const hikOSDConfigBlock = `<OSDConfig>
<Length>704</Length>
<Width>576</Width>
<TimeX>0</TimeX>
<TimeY>32</TimeY>
<TimeEnable>1</TimeEnable>
<TimeType>1</TimeType>
<TextEnable>0</TextEnable>
<SumNum>0</SumNum>
</OSDConfig>
`

// TestOSDConfigKeepsReportedSwitchValues 真机原文里的开关值必须**原样**落到 block。
//
// 这是 2026-09-19 海康真机排查挖出的缺陷的回归锚点：`osdConfigWire` 的两个开关曾用
// 非指针 `int` 接、`toBlock` 又无条件赋 `osdSwitchDefault`，于是「设备报关」被判成「开」。
//
// ⛔ 为什么原有的 round-trip 用例没抓住：那条用例的开关值**恰好都是 1**
// （见 `device_config_test.go` 里 `TimeEnable: AlarmReportOn, TextEnable: AlarmReportOn`），
// 而 1 正好等于默认值 —— 缺陷只在"实到 0"时显形。**用等于默认值的数据测默认值，等于没测。**
func TestOSDConfigKeepsReportedSwitchValues(t *testing.T) {
	result, err := ParseDeviceConfigReadResponse(configReadResponse(940, testDeviceID, hikOSDConfigBlock))
	if err != nil {
		t.Fatalf("真机报文解析失败: %v", err)
	}
	block := result.Blocks.OSDConfig
	if block == nil {
		t.Fatal("OSDConfig 块缺失")
	}

	if block.Length != 704 || block.Width != 576 {
		t.Fatalf("窗口尺寸不符: Length=%d Width=%d", block.Length, block.Width)
	}
	if block.TimeX != 0 || block.TimeY != 32 {
		t.Fatalf("时间坐标不符: TimeX=%d TimeY=%d", block.TimeX, block.TimeY)
	}
	if block.TimeEnable != 1 {
		t.Fatalf("TimeEnable 应为实到的 1，实际 %d", block.TimeEnable)
	}
	// ⛔ 本用例的核心断言。
	if block.TextEnable != 0 {
		t.Fatalf("TextEnable 应保留设备实到的 0（元素在场），实际被改写成 %d —— "+
			"\"设备报关\"被静默篡改成\"开\"", block.TextEnable)
	}
	if block.TimeType == nil || *block.TimeType != osdTimeTypeCNDate {
		t.Fatalf("TimeType 不符: %v", block.TimeType)
	}

	// 落库形态（`gb_device_config.payload_json` 就是 block 的 json.Marshal）。
	// ⛔ 绑在 JSON 这一层是因为"能过对账"的判据用的是落库值，不是内存里的结构体。
	payload, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("payload 序列化失败: %v", err)
	}
	if !strings.Contains(string(payload), `"textEnable":0`) {
		t.Fatalf("落库 payload 里 TextEnable 不是 0: %s", payload)
	}
	if !strings.Contains(string(payload), `"timeEnable":1`) {
		t.Fatalf("落库 payload 里 TimeEnable 不是 1: %s", payload)
	}
}

// TestOSDConfigSwitchMatrix 覆盖「两个开关 × {实到 0 / 实到 1 / 缺席}」的组合。
//
// 这条防的是"修反了"：只把 0 当成缺席（`if value == 0 { return 1 }`）同样错，
// 只是把"篡改"换了方向。缺席必须是**元素不存在**，不是**值为 0**。
func TestOSDConfigSwitchMatrix(t *testing.T) {
	cases := []struct {
		name     string
		switches string
		wantTime int
		wantText int
	}{
		{"两开关都在场且为 0", "<TimeEnable>0</TimeEnable><TextEnable>0</TextEnable>", 0, 0},
		{"时间关文字开", "<TimeEnable>0</TimeEnable><TextEnable>1</TextEnable>", 0, 1},
		{"时间开文字关", "<TimeEnable>1</TimeEnable><TextEnable>0</TextEnable>", 1, 0},
		{"两开关都在场且为 1", "<TimeEnable>1</TimeEnable><TextEnable>1</TextEnable>", 1, 1},
		{"两开关都缺席 → XSD default=\"1\"", "", osdSwitchDefault, osdSwitchDefault},
		{"仅 TimeEnable 缺席", "<TextEnable>0</TextEnable>", osdSwitchDefault, 0},
		{"仅 TextEnable 缺席", "<TimeEnable>0</TimeEnable>", 0, osdSwitchDefault},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			blockXML := "<OSDConfig><Length>1920</Length><Width>1080</Width>" +
				"<TimeX>0</TimeX><TimeY>0</TimeY>" + testCase.switches +
				"<SumNum>0</SumNum></OSDConfig>"
			result, err := ParseDeviceConfigReadResponse(configReadResponse(941, testDeviceID, blockXML))
			if err != nil {
				t.Fatalf("解析失败: %v\n%s", err, blockXML)
			}
			osd := result.Blocks.OSDConfig
			if osd == nil {
				t.Fatal("OSDConfig 块缺失")
			}
			if osd.TimeEnable != testCase.wantTime {
				t.Fatalf("TimeEnable: got=%d want=%d", osd.TimeEnable, testCase.wantTime)
			}
			if osd.TextEnable != testCase.wantText {
				t.Fatalf("TextEnable: got=%d want=%d", osd.TextEnable, testCase.wantText)
			}
		})
	}
}

// TestOSDConfigRoundTripPreservesSwitchZero 下发方向也不能把 0 改成 1。
//
// 修复前 `wire()` 把 block 的 `int` 直传给非指针字段，两个方向**共享**同一个缺陷：
// 读回来是 1、发出去也是 1。这条钉住"平台下发给设备的报文里，操作员设的 0 就是 0"。
//
// ⛔ 顺带钉住两个开关**一定出现在报文里**（虽然 XSD 有 default）：把 wire 字段改成指针后
// 若忘了在 `wire()` 里赋值，元素会整个消失，对端只能靠 XSD 默认猜 —— 那是静默的行为变化。
func TestOSDConfigRoundTripPreservesSwitchZero(t *testing.T) {
	blocks := DeviceConfigBlocks{OSDConfig: &OSDConfigBlock{
		Length: 704, Width: 576, TimeX: 0, TimeY: 32,
		TimeEnable: 1, TextEnable: 0,
	}}

	body, err := BuildDeviceConfigBlocksWithProfile(
		protocol.ProfileFor(protocol.Version2016), testDeviceID, 942, blocks)
	if err != nil {
		t.Fatalf("构建下发报文失败: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "<TextEnable>0</TextEnable>") {
		t.Fatalf("下发报文里 TextEnable 丢了或被改写:\n%s", text)
	}
	if !strings.Contains(text, "<TimeEnable>1</TimeEnable>") {
		t.Fatalf("下发报文里 TimeEnable 丢了或被改写:\n%s", text)
	}

	// 再把这条报文当应答读回来（同一个解析入口），确认两端口径一致。
	declaration, blocksXML := controlMessageParts(t, body)
	parsed, err := ParseDeviceConfigReadResponse(
		configReadResponseWithDeclaration(declaration, 942, testDeviceID, blocksXML))
	if err != nil {
		t.Fatalf("round-trip 解析失败: %v\n%s", err, text)
	}
	osd := parsed.Blocks.OSDConfig
	if osd == nil {
		t.Fatal("round-trip 后 OSDConfig 缺失")
	}
	if osd.TimeEnable != 1 || osd.TextEnable != 0 {
		t.Fatalf("round-trip 后开关值不符: TimeEnable=%d TextEnable=%d", osd.TimeEnable, osd.TextEnable)
	}
}
