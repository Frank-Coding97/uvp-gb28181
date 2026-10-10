package manscdp

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestAdvancedControlBuilders(t *testing.T) {
	tests := []struct {
		name  string
		build func() ([]byte, error)
		check func(t *testing.T, control decodedAdvancedControl)
	}{
		{
			name:  "force key frame uses historical field spelling",
			build: func() ([]byte, error) { return BuildIFrameControl("C", 1, XMLCharsetGB2312) },
			check: func(t *testing.T, control decodedAdvancedControl) {
				if control.IFameCmd != "Send" {
					t.Fatalf("IFameCmd=%q, want Send", control.IFameCmd)
				}
			},
		},
		{
			name:  "start device recording",
			build: func() ([]byte, error) { return BuildRecordControl("C", 2, RecordStart, XMLCharsetGB2312) },
			check: func(t *testing.T, control decodedAdvancedControl) {
				if control.RecordCmd != "Record" {
					t.Fatalf("RecordCmd=%q, want Record", control.RecordCmd)
				}
			},
		},
		{
			name:  "stop device recording",
			build: func() ([]byte, error) { return BuildRecordControl("C", 3, RecordStop, XMLCharsetGB2312) },
			check: func(t *testing.T, control decodedAdvancedControl) {
				if control.RecordCmd != "StopRecord" {
					t.Fatalf("RecordCmd=%q, want StopRecord", control.RecordCmd)
				}
			},
		},
		{
			name:  "set guard",
			build: func() ([]byte, error) { return BuildGuardControl("C", 4, GuardSet, XMLCharsetGB2312) },
			check: func(t *testing.T, control decodedAdvancedControl) {
				if control.GuardCmd != "SetGuard" {
					t.Fatalf("GuardCmd=%q, want SetGuard", control.GuardCmd)
				}
			},
		},
		{
			name: "reset alarm without selectors",
			build: func() ([]byte, error) {
				return BuildAlarmResetControl("C", 5, AlarmResetOptions{}, XMLCharsetGB2312)
			},
			check: func(t *testing.T, control decodedAdvancedControl) {
				if control.AlarmCmd != "ResetAlarm" || control.Info != nil {
					t.Fatalf("unexpected alarm control: %+v", control)
				}
			},
		},
		{
			name:  "tele boot",
			build: func() ([]byte, error) { return BuildTeleBootControl("C", 6, true, XMLCharsetGB2312) },
			check: func(t *testing.T, control decodedAdvancedControl) {
				if control.TeleBoot != "Boot" {
					t.Fatalf("TeleBoot=%q, want Boot", control.TeleBoot)
				}
			},
		},
		{
			name:  "format sd card",
			build: func() ([]byte, error) { return BuildFormatSDCardControl("C", 10, 2, XMLCharsetGB2312) },
			check: func(t *testing.T, control decodedAdvancedControl) {
				if control.FormatSDCard == nil || *control.FormatSDCard != 2 {
					t.Fatalf("FormatSDCard=%v, want 2", control.FormatSDCard)
				}
				// 格式化是独立命令：同一报文里不该捎带录像/布防/重启等其它控制字段，
				// 否则设备可能按"最后一个命令"处理，行为不可预测。
				if control.TeleBoot != "" || control.RecordCmd != "" || control.GuardCmd != "" || control.AlarmCmd != "" {
					t.Fatalf("格式化报文不应捎带其它控制字段: %+v", control)
				}
			},
		},
		{
			name: "drag zoom in",
			build: func() ([]byte, error) {
				return BuildDragZoomControl("C", 7, DragZoomCommand{Direction: DragZoomIn, Region: DragZoomRegion{
					Length: 1920, Width: 1080, MidPointX: 960, MidPointY: 540, LengthX: 640, LengthY: 360,
				}}, XMLCharsetGB2312)
			},
			check: func(t *testing.T, control decodedAdvancedControl) {
				if control.DragZoomIn == nil || *control.DragZoomIn != (DragZoomRegion{Length: 1920, Width: 1080, MidPointX: 960, MidPointY: 540, LengthX: 640, LengthY: 360}) {
					t.Fatalf("unexpected DragZoomIn: %+v", control.DragZoomIn)
				}
			},
		},
		{
			name: "drag zoom out",
			build: func() ([]byte, error) {
				return BuildDragZoomControl("C", 8, DragZoomCommand{Direction: DragZoomOut, Region: DragZoomRegion{
					Length: 1280, Width: 720, MidPointX: 640, MidPointY: 360, LengthX: 320, LengthY: 180,
				}}, XMLCharsetGB2312)
			},
			check: func(t *testing.T, control decodedAdvancedControl) {
				if control.DragZoomOut == nil || control.DragZoomIn != nil {
					t.Fatalf("unexpected drag zoom fields: in=%+v out=%+v", control.DragZoomIn, control.DragZoomOut)
				}
			},
		},
		{
			name: "drag zoom exact window boundary",
			build: func() ([]byte, error) {
				return BuildDragZoomControl("C", 9, DragZoomCommand{Direction: DragZoomIn, Region: DragZoomRegion{
					Length: 100, Width: 80, MidPointX: 50, MidPointY: 40, LengthX: 100, LengthY: 80,
				}}, XMLCharsetGB2312)
			},
			check: func(t *testing.T, control decodedAdvancedControl) {
				if control.DragZoomIn == nil {
					t.Fatal("full-window boundary rectangle should be valid")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := tt.build()
			if err != nil {
				t.Fatalf("builder error = %v", err)
			}
			if !strings.HasPrefix(string(body), `<?xml version="1.0" encoding="GB2312"?>`) {
				t.Fatalf("unexpected declaration: %s", body)
			}
			if strings.Contains(string(body), "<IFrameCmd>") {
				t.Fatalf("standard spelling is incompatible with deployed devices: %s", body)
			}
			var control decodedAdvancedControl
			if err := newDecoder(body).Decode(&control); err != nil {
				t.Fatalf("round-trip decode error = %v", err)
			}
			if control.CmdType != CmdDeviceControl || control.DeviceID != "C" || control.SN < 1 {
				t.Fatalf("unexpected control header: %+v", control)
			}
			tt.check(t, control)
		})
	}
}

// ⛔⛔ 本次最重要的防回归锚点：卡编号 0 是**合法语义**，不是"空值"。
//
// 标准 A.2.3.1.13 原文：「SD 卡编号，从1开始编号。**该值0时，对所有存储卡进行格式化**」。
// 若有人把 `advancedDeviceControl.FormatSDCard` 从 `*int` 改回 `int`，
// `omitempty` 会把 0 **整个省略** ⇒ 报文里元素消失，设备收到一条不含卡编号的命令。
// 而单测若只用卡号 2 去测，**永远发现不了**这个退化 —— 所以这一条必须专门钉 0。
func TestBuildFormatSDCardControl_ZeroMeansAllCards(t *testing.T) {
	body, err := BuildFormatSDCardControl("C", 11, 0, XMLCharsetGB2312)
	if err != nil {
		t.Fatalf("builder error = %v", err)
	}
	if !strings.Contains(string(body), "<FormatSDCard>0</FormatSDCard>") {
		t.Fatalf("卡编号 0（= 格式化全部卡）必须原样出现在报文里，实际: %s", body)
	}
	var control decodedAdvancedControl
	if err := newDecoder(body).Decode(&control); err != nil {
		t.Fatalf("round-trip decode error = %v", err)
	}
	if control.FormatSDCard == nil || *control.FormatSDCard != 0 {
		t.Fatalf("回解后卡编号应为 0，实际 %v", control.FormatSDCard)
	}
}

// 报文形态锚点：`FormatSDCard` 是 DeviceControl 内与 SN/DeviceID **同级**的 integer 元素，
// 没有 `DiskNum` 之类的包装 —— 本仓曾有一处注释把 `DiskNum` 当标准字段引用，
// 而它在 2022 全文 / 2022 附录 A / 2016 附录 A 三处均 0 命中（2026-09-20 核）。
func TestBuildFormatSDCardControl_IsDirectChildWithoutWrapper(t *testing.T) {
	body, err := BuildFormatSDCardControl("C", 12, 1, XMLCharsetGB2312)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "<FormatSDCard>1</FormatSDCard>") {
		t.Fatalf("应为直接子元素形态，实际: %s", body)
	}
	if strings.Contains(string(body), "DiskNum") {
		t.Fatalf("DiskNum 不是标准元素，报文里不许出现: %s", body)
	}
}

func TestBuildFormatSDCardControl_RejectsNegativeCardIndex(t *testing.T) {
	body, err := BuildFormatSDCardControl("C", 13, -1, XMLCharsetGB2312)
	if err == nil || body != nil {
		t.Fatalf("负卡号必须拒发, body=%q err=%v", body, err)
	}
}

func TestLegacyAlarmResetBuilderRejectsSelectors(t *testing.T) {
	body, err := BuildAlarmResetControl("C", 5, AlarmResetOptions{AlarmMethod: "4", AlarmType: "1"}, XMLCharsetGB2312)
	if err == nil || body != nil {
		t.Fatalf("2016 charset-only builder must reject selectors, body=%q err=%v", body, err)
	}
}

func TestAdvancedControlBuilders_UTF8DeclarationAndRoundTrip(t *testing.T) {
	body, err := BuildIFrameControl("通道-C", 9, XMLCharsetUTF8)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(body), `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Fatalf("unexpected declaration: %s", body)
	}
	var control decodedAdvancedControl
	if err := xml.Unmarshal(body, &control); err != nil {
		t.Fatalf("UTF-8 round-trip decode error = %v", err)
	}
	if control.DeviceID != "通道-C" || control.IFameCmd != "Send" {
		t.Fatalf("unexpected control: %+v", control)
	}
}

func TestAdvancedControlBuildersRejectInvalidCommands(t *testing.T) {
	tests := []struct {
		name  string
		build func() ([]byte, error)
	}{
		{"invalid record action", func() ([]byte, error) { return BuildRecordControl("C", 1, RecordAction("pause"), XMLCharsetGB2312) }},
		{"invalid guard action", func() ([]byte, error) { return BuildGuardControl("C", 1, GuardAction("toggle"), XMLCharsetGB2312) }},
		{"tele boot without confirmation", func() ([]byte, error) { return BuildTeleBootControl("C", 1, false, XMLCharsetGB2312) }},
		{"invalid xml charset", func() ([]byte, error) { return BuildIFrameControl("C", 1, XMLCharset("GBK")) }},
		{"empty device id", func() ([]byte, error) { return BuildIFrameControl(" ", 1, XMLCharsetGB2312) }},
		{"invalid sn", func() ([]byte, error) { return BuildIFrameControl("C", 0, XMLCharsetGB2312) }},
		{"invalid drag direction", func() ([]byte, error) {
			return BuildDragZoomControl("C", 1, DragZoomCommand{Direction: DragZoomDirection("left"), Region: DragZoomRegion{Length: 100, Width: 80, MidPointX: 50, MidPointY: 40, LengthX: 20, LengthY: 20}}, XMLCharsetGB2312)
		}},
		{"zero window", func() ([]byte, error) {
			return BuildDragZoomControl("C", 1, DragZoomCommand{Direction: DragZoomIn, Region: DragZoomRegion{}}, XMLCharsetGB2312)
		}},
		{"outside window", func() ([]byte, error) {
			return BuildDragZoomControl("C", 1, DragZoomCommand{Direction: DragZoomIn, Region: DragZoomRegion{Length: 100, Width: 80, MidPointX: 95, MidPointY: 40, LengthX: 20, LengthY: 20}}, XMLCharsetGB2312)
		}},
		{"reversed rectangle", func() ([]byte, error) {
			return BuildDragZoomControl("C", 1, DragZoomCommand{Direction: DragZoomOut, Region: DragZoomRegion{Length: 100, Width: 80, MidPointX: 50, MidPointY: 40, LengthX: -20, LengthY: 20}}, XMLCharsetGB2312)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := tt.build()
			if err == nil {
				t.Fatalf("expected error, got body %s", body)
			}
			if body != nil {
				t.Fatalf("invalid command generated a body: %s", body)
			}
		})
	}
}

func TestParseControlCapabilitiesThreeState(t *testing.T) {
	t.Run("missing report leaves advanced controls unknown", func(t *testing.T) {
		got := ParseControlCapabilities(nil, 1)
		if got.BasicPTZ.State != CapabilitySupported {
			t.Fatalf("BasicPTZ state=%s, want supported", got.BasicPTZ.State)
		}
		for name, capability := range got.advanced() {
			if capability.State != CapabilityUnknown || capability.Reason == "" {
				t.Errorf("%s=%+v, want unknown with reason", name, capability)
			}
		}
	})

	t.Run("explicit false is unsupported and true is supported", func(t *testing.T) {
		raw := `{"iframe":true,"recording":false,"guard":true,"alarm_reset":false,"teleboot":true,"drag_zoom":false,"target_track":true}`
		got := ParseControlCapabilities(&raw, 0)
		want := map[string]CapabilityState{
			"iFrame": CapabilitySupported, "record": CapabilityUnsupported, "guard": CapabilitySupported,
			"alarmReset": CapabilityUnsupported, "teleBoot": CapabilitySupported, "dragZoom": CapabilityUnsupported,
			"targetTrack": CapabilitySupported,
		}
		for name, capability := range got.advanced() {
			if capability.State != want[name] || capability.Reason == "" {
				t.Errorf("%s=%+v, want state %s with reason", name, capability, want[name])
			}
		}
		if got.BasicPTZ.State != CapabilityUnknown {
			t.Fatalf("BasicPTZ state=%s, want unknown", got.BasicPTZ.State)
		}
	})

	t.Run("PTZType never promotes missing advanced capability", func(t *testing.T) {
		raw := `{"recording":true}`
		got := ParseControlCapabilities(&raw, 2)
		if got.Record.State != CapabilitySupported {
			t.Fatalf("Record=%+v, want supported", got.Record)
		}
		if got.IFrame.State != CapabilityUnknown || got.Guard.State != CapabilityUnknown || got.DragZoom.State != CapabilityUnknown || got.TargetTrack.State != CapabilityUnknown {
			t.Fatalf("advanced capabilities were inferred from PTZType: %+v", got)
		}
	})

	t.Run("invalid capability JSON stays unknown", func(t *testing.T) {
		raw := `{invalid`
		got := ParseControlCapabilities(&raw, 3)
		if got.BasicPTZ.State != CapabilityUnsupported {
			t.Fatalf("fixed camera BasicPTZ=%+v, want unsupported", got.BasicPTZ)
		}
		for name, capability := range got.advanced() {
			if capability.State != CapabilityUnknown || !strings.Contains(capability.Reason, "JSON") {
				t.Errorf("%s=%+v, want invalid JSON reason", name, capability)
			}
		}
	})
}

type decodedAdvancedControl struct {
	XMLName     xml.Name           `xml:"Control"`
	CmdType     string             `xml:"CmdType"`
	SN          int                `xml:"SN"`
	DeviceID    string             `xml:"DeviceID"`
	IFameCmd    string             `xml:"IFameCmd"`
	RecordCmd   string             `xml:"RecordCmd"`
	GuardCmd    string             `xml:"GuardCmd"`
	AlarmCmd    string             `xml:"AlarmCmd"`
	TeleBoot    string             `xml:"TeleBoot"`
	Info        *AlarmResetOptions `xml:"Info"`
	DragZoomIn  *DragZoomRegion    `xml:"DragZoomIn"`
	DragZoomOut *DragZoomRegion    `xml:"DragZoomOut"`
	// 用指针回解，才能区分「元素缺席」（nil）与「元素值为 0」（合法：格式化全部卡）。
	FormatSDCard *int `xml:"FormatSDCard"`
	// A.2.3.1.14 目标跟踪三元素。TargetArea 必须能回解出**完整六个子元素**，
	// 否则"消息发出去了但坐标丢了"这种退化在单测里看不出来。
	TargetTrack string          `xml:"TargetTrack"`
	DeviceID2   string          `xml:"DeviceID2"`
	TargetArea  *DragZoomRegion `xml:"TargetArea"`
}

func (capabilities ControlCapabilities) advanced() map[string]ControlCapability {
	return map[string]ControlCapability{
		"iFrame": capabilities.IFrame, "record": capabilities.Record, "guard": capabilities.Guard,
		"alarmReset": capabilities.AlarmReset, "teleBoot": capabilities.TeleBoot, "dragZoom": capabilities.DragZoom,
		"targetTrack": capabilities.TargetTrack,
	}
}
