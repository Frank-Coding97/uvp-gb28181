package manscdp

import (
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
		want   string
	}{
		{"set preset", PTZActionSetPreset, 3, "A50F018100030039"},
		{"call preset", PTZActionCallPreset, 3, "A50F01820003003A"},
		{"start cruise", PTZActionCruiseStart, 4, "A50F018804000041"},
		{"delete cruise path", PTZActionCruiseDeletePath, 2, "A50F01850200003C"},
		{"start scan", PTZActionScanStart, 5, "A50F018905000043"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := BuildExtendedPTZControl("C", 9, PTZExtendedCommand{Action: tt.action, ID: tt.id, Speed: 8})
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

func TestBuildExtendedPTZControl_RequiresProfileForLens(t *testing.T) {
	_, err := BuildExtendedPTZControl("C", 1, PTZExtendedCommand{Action: PTZActionFocusNear, Speed: 8})
	if err == nil || !strings.Contains(err.Error(), "profile") {
		t.Fatalf("expected profile error, got %v", err)
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
