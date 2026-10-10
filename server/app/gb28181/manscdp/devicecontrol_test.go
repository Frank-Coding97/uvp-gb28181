package manscdp

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestParseDeviceControlResponse(t *testing.T) {
	for _, test := range []struct {
		name    string
		charset string
		result  string
		want    DeviceControlResult
	}{
		{"UTF-8 OK", "UTF-8", " OK ", DeviceControlResultOK},
		{"GB2312 ERROR", "GB2312", "ERROR", DeviceControlResultError},
		{"GB18030 OK", "GB18030", "OK", DeviceControlResultOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := encodeDeviceControlResponseXML(t, test.charset, test.result)
			response, err := ParseDeviceControlResponse(body)
			if err != nil {
				t.Fatal(err)
			}
			if response.XMLName.Local != "Response" || response.CmdType != CmdDeviceControl || response.SN != 7 || response.DeviceID != "C" || response.Result != test.want {
				t.Fatalf("unexpected response: %+v", response)
			}
		})
	}

	t.Run("namespace and extensions", func(t *testing.T) {
		body := []byte(`<gb:Response xmlns:gb="urn:gb28181"><gb:CmdType>DeviceControl</gb:CmdType><gb:SN>8</gb:SN><gb:DeviceID>C</gb:DeviceID><gb:Result>OK</gb:Result><gb:VendorField>ignored</gb:VendorField></gb:Response>`)
		response, err := ParseDeviceControlResponse(body)
		if err != nil || response.Result != DeviceControlResultOK {
			t.Fatalf("namespace response did not parse by local name: %+v, err=%v", response, err)
		}
	})
}

func TestParseDeviceControlResponseRejectsInvalid(t *testing.T) {
	tests := map[string]string{
		"wrong root":       `<Notify><CmdType>DeviceControl</CmdType><SN>1</SN><DeviceID>C</DeviceID><Result>OK</Result></Notify>`,
		"wrong root case":  `<response><CmdType>DeviceControl</CmdType><SN>1</SN><DeviceID>C</DeviceID><Result>OK</Result></response>`,
		"wrong CmdType":    `<Response><CmdType>HomePositionQuery</CmdType><SN>1</SN><DeviceID>C</DeviceID><Result>OK</Result></Response>`,
		"zero SN":          `<Response><CmdType>DeviceControl</CmdType><SN>0</SN><DeviceID>C</DeviceID><Result>OK</Result></Response>`,
		"invalid SN":       `<Response><CmdType>DeviceControl</CmdType><SN>x</SN><DeviceID>C</DeviceID><Result>OK</Result></Response>`,
		"empty DeviceID":   `<Response><CmdType>DeviceControl</CmdType><SN>1</SN><DeviceID> </DeviceID><Result>OK</Result></Response>`,
		"missing Result":   `<Response><CmdType>DeviceControl</CmdType><SN>1</SN><DeviceID>C</DeviceID></Response>`,
		"empty Result":     `<Response><CmdType>DeviceControl</CmdType><SN>1</SN><DeviceID>C</DeviceID><Result> </Result></Response>`,
		"unknown Result":   `<Response><CmdType>DeviceControl</CmdType><SN>1</SN><DeviceID>C</DeviceID><Result>UNKNOWN</Result></Response>`,
		"substring Result": `<Response><CmdType>DeviceControl</CmdType><SN>1</SN><DeviceID>C</DeviceID><Result>NOT OK</Result></Response>`,
		"lowercase Result": `<Response><CmdType>DeviceControl</CmdType><SN>1</SN><DeviceID>C</DeviceID><Result>ok</Result></Response>`,
		"malformed XML":    `<Response><CmdType>DeviceControl</CmdType>`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			response, err := ParseDeviceControlResponse([]byte(body))
			if err == nil || response != nil {
				t.Fatalf("invalid response must fail: %+v, err=%v", response, err)
			}
			if !errors.Is(err, ErrInvalidResponse) {
				t.Fatalf("error must match ErrInvalidResponse: %v", err)
			}
			var parseError *ParseError
			if !errors.As(err, &parseError) {
				t.Fatalf("error must expose *ParseError: %T %v", err, err)
			}
		})
	}
}

func encodeDeviceControlResponseXML(t *testing.T, charset, result string) []byte {
	t.Helper()
	source := `<?xml version="1.0" encoding="` + charset + `"?><Response><CmdType>DeviceControl</CmdType><SN>7</SN><DeviceID>C</DeviceID><Result>` + result + `</Result><VendorName>中文</VendorName></Response>`
	if charset == "UTF-8" {
		return []byte(source)
	}
	encoder := simplifiedchinese.GBK.NewEncoder()
	if charset == "GB18030" {
		encoder = simplifiedchinese.GB18030.NewEncoder()
	}
	body, err := encoder.Bytes([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestBuildPTZControl_LeftUp(t *testing.T) {
	body, err := BuildPTZControl("37011200001310000001", 7, PTZCommand{
		Action: PTZActionLeftUp,
		Speed:  8,
	})
	if err != nil {
		t.Fatalf("BuildPTZControl() error = %v", err)
	}
	text := string(body)
	if !strings.Contains(text, `<?xml version="1.0" encoding="GB2312"?>`) {
		t.Fatalf("missing GB2312 declaration: %s", text)
	}
	var control struct {
		CmdType  string `xml:"CmdType"`
		SN       int    `xml:"SN"`
		DeviceID string `xml:"DeviceID"`
		PTZCmd   string `xml:"PTZCmd"`
		Info     struct {
			Priority int `xml:"ControlPriority"`
		} `xml:"Info"`
	}
	if err := newDecoder(body).Decode(&control); err != nil {
		t.Fatalf("xml.Unmarshal() error = %v", err)
	}
	if control.CmdType != CmdDeviceControl || control.SN != 7 || control.DeviceID != "37011200001310000001" {
		t.Fatalf("unexpected control header: %+v", control)
	}
	if control.PTZCmd != "A50F010A080800CF" {
		t.Fatalf("unexpected PTZ command: %s", control.PTZCmd)
	}
	if control.Info.Priority != 5 {
		t.Fatalf("unexpected control priority: %d", control.Info.Priority)
	}
}

func TestParsePTZAction(t *testing.T) {
	tests := map[string]PTZAction{
		"left":     PTZActionLeft,
		"RIGHT-UP": PTZActionRightUp,
		"zoom_out": PTZActionZoomOut,
		"停止":       PTZActionStop,
	}
	for input, want := range tests {
		got, err := ParsePTZAction(input)
		if err != nil {
			t.Errorf("ParsePTZAction(%q) error = %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("ParsePTZAction(%q) = %q, want %q", input, got, want)
		}
	}
	if _, err := ParsePTZAction("invalid"); err == nil {
		t.Fatal("ParsePTZAction(invalid) should fail")
	}
}

func TestBuildPTZControlRejectsInvalidInput(t *testing.T) {
	if _, err := BuildPTZControl("", 1, PTZCommand{Action: PTZActionLeft, Speed: 8}); err == nil {
		t.Fatal("empty DeviceID should fail")
	}
	if _, err := BuildPTZControl("C", 1, PTZCommand{Action: PTZActionLeft, Speed: 0}); err == nil {
		t.Fatal("zero speed should fail")
	}
	if _, err := BuildPTZControl("C", 1, PTZCommand{Action: PTZActionLeft, Speed: 256}); err == nil {
		t.Fatal("speed > 255 should fail")
	}
}

func TestBuildExtendedPTZControl_UsesStandardInstructionAndParameterLayout(t *testing.T) {
	tests := []struct {
		name   string
		action PTZExtendedAction
		id     int
		value  int
		want   string
	}{
		{"set preset", PTZActionSetPreset, 3, 0, "A50F018100030039"},
		{"call preset", PTZActionCallPreset, 3, 0, "A50F01820003003A"},
		{"start cruise", PTZActionCruiseStart, 4, 0, "A50F018804000041"},
		{"delete cruise path", PTZActionCruiseDeletePath, 2, 0, "A50F01850200003C"},
		{"start scan", PTZActionScanStart, 5, 0, "A50F018905000043"},
		// 89H 的三个子动作都在**字节6**:00H 开始 / 01H 左边界 / 02H 右边界。
		{"set scan left bound", PTZActionScanSetLeft, 5, 0, "A50F018905010044"},
		{"set scan right bound", PTZActionScanSetRight, 5, 0, "A50F018905020045"},
		// 8AH 的速度是 12 位:低 8 位进字节6,高 4 位进字节7 的高半字节。
		{"set scan speed", PTZActionScanSetSpeed, 5, 1000, "A50F018A05E8305C"},
		// 辅助开关(A.3.7 表 A.11):编号在**数据1(字节5)**,字节6 不参与 ——
		// 开/关由指令码本身表达(`8CH` / `8DH`)。⛔ 与上一族 89H 的子动作位置不同。
		{"wiper on", PTZActionAuxOn, PTZAuxiliaryIDWiper, 0, "A50F018C01000042"},
		{"wiper off", PTZActionAuxOff, PTZAuxiliaryIDWiper, 0, "A50F018D01000043"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := BuildExtendedPTZControl("C", 9, PTZExtendedCommand{Action: tt.action, ID: tt.id, Speed: 8, Value16: tt.value})
			if err != nil {
				t.Fatal(err)
			}
			var control struct {
				PTZCmd string `xml:"PTZCmd"`
			}
			if err := newDecoder(body).Decode(&control); err != nil {
				t.Fatal(err)
			}
			if control.PTZCmd != tt.want {
				t.Fatalf("PTZCmd=%s, want %s", control.PTZCmd, tt.want)
			}
		})
	}
}

func TestBuildExtendedPTZControl_RejectsNonStandardCruiseActions(t *testing.T) {
	for _, action := range []PTZExtendedAction{PTZActionCruisePause, PTZActionCruiseResume} {
		if _, err := BuildExtendedPTZControl("C", 1, PTZExtendedCommand{Action: action, ID: 1}); err == nil {
			t.Fatalf("action %s should be rejected instead of sending a guessed instruction", action)
		}
	}
}

// 辅助开关的编号域是 00H~FFH,但 0 不是合法开关(标准注只给了 1 = 雨刷),
// 越界必须在编码层就拒掉,不能让它悄悄编出一条开关号为 0 的帧。
func TestBuildExtendedPTZControl_AuxSwitchRejectsOutOfRangeNumber(t *testing.T) {
	for _, id := range []int{0, -1, 256} {
		for _, action := range []PTZExtendedAction{PTZActionAuxOn, PTZActionAuxOff} {
			if _, err := BuildExtendedPTZControl("C", 1, PTZExtendedCommand{Action: action, ID: id}); err == nil {
				t.Fatalf("action %s 编号 %d 应被拒绝", action, id)
			}
		}
	}
}

// ⛔ 回归锚点:辅助开关是 `8CH` / `8DH`,**不是** `89H` / `8AH`(那是扫描)。
// 这两族曾被并成一行写过一次 —— 后果是平台点「开始扫描」,设备去开了雨刷;
// SIP 收发全正常、设备回 200 OK,只有画面纹丝不动。
func TestBuildExtendedPTZControl_AuxSwitchIsNotScanFamily(t *testing.T) {
	body, err := BuildExtendedPTZControl("C", 9, PTZExtendedCommand{Action: PTZActionAuxOn, ID: PTZAuxiliaryIDWiper})
	if err != nil {
		t.Fatal(err)
	}
	var control struct {
		PTZCmd string `xml:"PTZCmd"`
	}
	if err := newDecoder(body).Decode(&control); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(control.PTZCmd, "A50F018C") {
		t.Fatalf("雨刷开启必须是 8CH 指令码, got %s", control.PTZCmd)
	}
	for _, scanPrefix := range []string{"A50F0189", "A50F018A"} {
		if strings.HasPrefix(control.PTZCmd, scanPrefix) {
			t.Fatalf("雨刷帧落进了扫描族(%s): %s", scanPrefix, control.PTZCmd)
		}
	}
}

// GB/T 28181 Annex A.2.1 FI 子族:byte4 = 0x41/0x42/0x44/0x48 与 FI 停止 0x40。
// 速度字节是不对称的 —— 聚焦速度在数据1(字节5),光圈速度在数据2(字节6)。
func TestBuildPTZControl_LensInstructionAndSpeedByteLayout(t *testing.T) {
	tests := []struct {
		name   string
		action PTZAction
		speed  int
		want   string
	}{
		{"focus far", PTZActionFocusFar, 0x20, "A50F014120000016"},
		{"focus near", PTZActionFocusNear, 0x20, "A50F014220000017"},
		{"iris open", PTZActionIrisOpen, 0x20, "A50F014400200019"},
		{"iris close", PTZActionIrisClose, 0x20, "A50F01480020001D"},
		{"lens stop", PTZActionLensStop, 0, "A50F0140000000F5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := BuildPTZControl("C", 9, PTZCommand{Action: tt.action, Speed: tt.speed})
			if err != nil {
				t.Fatal(err)
			}
			var control struct {
				PTZCmd string `xml:"PTZCmd"`
			}
			if err := newDecoder(body).Decode(&control); err != nil {
				t.Fatal(err)
			}
			if control.PTZCmd != tt.want {
				t.Fatalf("PTZCmd=%s, want %s", control.PTZCmd, tt.want)
			}
		})
	}
}

// 镜头动作只在数据1 或 数据2 之一带速度,另一个必须留零 —— 两头都塞会被严格设备判为非法。
func TestBuildPTZControl_LensSpeedNeverLandsOnBothDataBytes(t *testing.T) {
	body, err := BuildPTZControl("C", 9, PTZCommand{Action: PTZActionFocusFar, Speed: 0x20})
	if err != nil {
		t.Fatal(err)
	}
	var control struct {
		PTZCmd string `xml:"PTZCmd"`
	}
	if err := newDecoder(body).Decode(&control); err != nil {
		t.Fatal(err)
	}
	decoded, err := hex.DecodeString(control.PTZCmd)
	if err != nil {
		t.Fatal(err)
	}
	if decoded[4] != 0x20 || decoded[5] != 0x00 {
		t.Fatalf("focus speed must sit in data1 only, got data1=0x%02X data2=0x%02X", decoded[4], decoded[5])
	}
}

// 停止指令不带速度,允许 speed=0;其余动作仍必须给出 1-255。
func TestBuildPTZControl_StopAllowsZeroSpeedButMovesDoNot(t *testing.T) {
	for _, action := range []PTZAction{PTZActionStop, PTZActionLensStop} {
		if _, err := BuildPTZControl("C", 9, PTZCommand{Action: action, Speed: 0}); err != nil {
			t.Fatalf("%s with speed 0 should build: %v", action, err)
		}
	}
	for _, action := range []PTZAction{PTZActionLeft, PTZActionFocusFar, PTZActionIrisOpen} {
		if _, err := BuildPTZControl("C", 9, PTZCommand{Action: action, Speed: 0}); err == nil {
			t.Fatalf("%s with speed 0 should be rejected", action)
		}
	}
}

// 镜头动作现在走 PTZAction 主干,扩展构造器不再接受它们。
func TestBuildExtendedPTZControl_RejectsLensActions(t *testing.T) {
	for _, action := range []PTZExtendedAction{"focus_far", "focus_near", "iris_open", "iris_close"} {
		if _, err := BuildExtendedPTZControl("C", 1, PTZExtendedCommand{Action: action, Speed: 8}); err == nil {
			t.Fatalf("lens action %s should not be built by the extended builder", action)
		}
	}
}

func TestParsePTZActionAcceptsLensVocabulary(t *testing.T) {
	for input, want := range map[string]PTZAction{
		"focus_far":  PTZActionFocusFar,
		"远焦":         PTZActionFocusFar,
		"focus_near": PTZActionFocusNear,
		"近焦":         PTZActionFocusNear,
		"iris_open":  PTZActionIrisOpen,
		"光圈+":        PTZActionIrisOpen,
		"iris_close": PTZActionIrisClose,
		"光圈-":        PTZActionIrisClose,
		"lens_stop":  PTZActionLensStop,
	} {
		got, err := ParsePTZAction(input)
		if err != nil {
			t.Fatalf("ParsePTZAction(%q) failed: %v", input, err)
		}
		if got != want {
			t.Fatalf("ParsePTZAction(%q)=%s, want %s", input, got, want)
		}
	}
}

// GB/T 28181-2022 A.3.5:0x84 增点、0x85 删点、0x86 速度、0x87 停留。
func TestBuildExtendedPTZControl_CruiseBuildingBlocks(t *testing.T) {
	tests := []struct {
		name    string
		command PTZExtendedCommand
		want    string
	}{
		// TrackID=2, PresetID=5:  A5 0F 01 84 02 05 00 + checksum (0x140 → 40)
		{"add stop", PTZExtendedCommand{Action: PTZActionCruiseAddStop, ID: 2, SubID: 5}, "A50F018402050040"},
		// 0x85 的预置位号为 0 时删除整条巡航路径。
		{"delete stop", PTZExtendedCommand{Action: PTZActionCruiseDeleteStop, ID: 2, SubID: 5}, "A50F018502050041"},
		{"delete path", PTZExtendedCommand{Action: PTZActionCruiseDeletePath, ID: 2}, "A50F01850200003C"},
		// 12 bit 值:低 8 位在 byte6,高 4 位在 byte7 的高半字节。
		{"set speed", PTZExtendedCommand{Action: PTZActionCruiseSetSpeed, ID: 2, Value16: 256}, "A50F01860200104D"},
		{"set speed max", PTZExtendedCommand{Action: PTZActionCruiseSetSpeed, ID: 2, Value16: 4095}, "A50F018602FFF02C"},
		{"set dwell", PTZExtendedCommand{Action: PTZActionCruiseSetDwell, ID: 2, Value16: 5}, "A50F018702050043"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := BuildExtendedPTZControl("C", 9, tt.command)
			if err != nil {
				t.Fatal(err)
			}
			var control struct {
				PTZCmd string `xml:"PTZCmd"`
			}
			if err := newDecoder(body).Decode(&control); err != nil {
				t.Fatal(err)
			}
			if control.PTZCmd != tt.want {
				t.Fatalf("PTZCmd=%s, want %s", control.PTZCmd, tt.want)
			}
		})
	}
}

func TestBuildExtendedPTZControl_CruiseBuildingBlocksRejectOutOfRange(t *testing.T) {
	cases := []struct {
		name    string
		command PTZExtendedCommand
	}{
		{"add stop 需 SubID", PTZExtendedCommand{Action: PTZActionCruiseAddStop, ID: 1}},
		{"add stop SubID > 255", PTZExtendedCommand{Action: PTZActionCruiseAddStop, ID: 1, SubID: 256}},
		{"delete stop SubID < 0", PTZExtendedCommand{Action: PTZActionCruiseDeleteStop, ID: 1, SubID: -1}},
		{"delete stop SubID > 255", PTZExtendedCommand{Action: PTZActionCruiseDeleteStop, ID: 1, SubID: 256}},
		{"speed = 0", PTZExtendedCommand{Action: PTZActionCruiseSetSpeed, ID: 1, Value16: 0}},
		{"speed > 4095", PTZExtendedCommand{Action: PTZActionCruiseSetSpeed, ID: 1, Value16: 4096}},
		{"speed < 0", PTZExtendedCommand{Action: PTZActionCruiseSetSpeed, ID: 1, Value16: -1}},
		{"dwell = 0", PTZExtendedCommand{Action: PTZActionCruiseSetDwell, ID: 1, Value16: 0}},
		{"dwell > 4095", PTZExtendedCommand{Action: PTZActionCruiseSetDwell, ID: 1, Value16: 4096}},
		{"dwell < 0", PTZExtendedCommand{Action: PTZActionCruiseSetDwell, ID: 1, Value16: -1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := BuildExtendedPTZControl("C", 1, tc.command); err == nil {
				t.Fatalf("%s should reject", tc.name)
			}
		})
	}
}
