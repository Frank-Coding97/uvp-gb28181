package manscdp

import (
	"strings"
	"testing"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

// 一条标准形态的手动跟踪命令：球机通道当 DeviceID，全景通道当 DeviceID2。
const (
	targetTrackDomeCode = "34020000001320000001"
	targetTrackPanoCode = "34020000001320000002"
)

func manualTargetTrackArea() DragZoomRegion {
	return DragZoomRegion{Length: 1920, Width: 1080, MidPointX: 960, MidPointY: 540, LengthX: 200, LengthY: 100}
}

// ⛔ 本组最重要的防回归锚点：DeviceID（SN 之后，必选）与 DeviceID2（可选）**不是同一个编码**。
//
// 标准尾注原文：「SN后面的目标设备编码（必选）指全景相机的球机通道」，
// 而 DeviceID2 的注释是「目标设备编码（可选），指全景相机中的全景通道ID」。
// 若实现把球机编码同时填进 DeviceID2，报文**看起来完全正常**（都是 20 位编码），
// 但设备会把全景通道当成球机 —— 现象是"框选位置永远偏"，排查方向完全错。
func TestBuildTargetTrackControl_DeviceID2IsNotTheDomeChannel(t *testing.T) {
	body, err := BuildTargetTrackControlWithProfile(protocol.ProfileFor(protocol.Version2022), targetTrackDomeCode, 42, TargetTrackCommand{
		Mode:      TargetTrackManual,
		DeviceID2: targetTrackPanoCode,
		Area:      func() *TargetTrackArea { a := manualTargetTrackArea(); return &a }(),
	})
	if err != nil {
		t.Fatalf("builder error = %v", err)
	}

	var control decodedAdvancedControl
	if err := newDecoder(body).Decode(&control); err != nil {
		t.Fatalf("round-trip decode error = %v", err)
	}
	if control.CmdType != CmdDeviceControl {
		t.Fatalf("CmdType=%q, want %q", control.CmdType, CmdDeviceControl)
	}
	if control.DeviceID != targetTrackDomeCode {
		t.Fatalf("DeviceID=%q, want 球机通道 %q", control.DeviceID, targetTrackDomeCode)
	}
	if control.DeviceID2 != targetTrackPanoCode {
		t.Fatalf("DeviceID2=%q, want 全景通道 %q", control.DeviceID2, targetTrackPanoCode)
	}
	if control.DeviceID == control.DeviceID2 {
		t.Fatalf("DeviceID 与 DeviceID2 被写成了同一个编码：%q", control.DeviceID)
	}
	if control.TargetTrack != string(TargetTrackManual) {
		t.Fatalf("TargetTrack=%q, want Manual", control.TargetTrack)
	}
	if control.TargetArea == nil || *control.TargetArea != manualTargetTrackArea() {
		t.Fatalf("TargetArea 六个子元素必须整组到达，实际 %+v", control.TargetArea)
	}
	// 目标跟踪是独立命令：同一报文里不该捎带其它控制字段（同 FormatSDCard 的口径）。
	if control.TeleBoot != "" || control.RecordCmd != "" || control.GuardCmd != "" || control.AlarmCmd != "" ||
		control.FormatSDCard != nil || control.DragZoomIn != nil || control.DragZoomOut != nil {
		t.Fatalf("目标跟踪报文不应捎带其它控制字段: %s", body)
	}
}

// 元素顺序按 A.2.3.1 的 `Control` 序列：TargetTrack / DeviceID2 / TargetArea 是该序列里最后三个。
// 顺序错了多数设备也能解析，但会与标准原文的 schema 不一致 —— 这条钉住的是"照标准排"。
func TestBuildTargetTrackControl_ElementOrderFollowsAppendixA(t *testing.T) {
	body, err := BuildTargetTrackControlWithProfile(protocol.ProfileFor(protocol.Version2022), targetTrackDomeCode, 7, TargetTrackCommand{
		Mode:      TargetTrackManual,
		DeviceID2: targetTrackPanoCode,
		Area:      func() *TargetTrackArea { a := manualTargetTrackArea(); return &a }(),
	})
	if err != nil {
		t.Fatalf("builder error = %v", err)
	}
	text := string(body)
	order := []string{"<TargetTrack>", "<DeviceID2>", "<TargetArea>"}
	last := -1
	for _, tag := range order {
		at := strings.Index(text, tag)
		if at < 0 {
			t.Fatalf("报文缺少 %s: %s", tag, text)
		}
		if at < last {
			t.Fatalf("元素顺序与 A.2.3.1 的 Control 序列不一致（%s 应排在前面）: %s", tag, text)
		}
		last = at
	}
}

func TestBuildTargetTrackControl_AutoAndStopCarryNoCoordinates(t *testing.T) {
	for _, mode := range []TargetTrackMode{TargetTrackAuto, TargetTrackStop} {
		t.Run(string(mode), func(t *testing.T) {
			body, err := BuildTargetTrackControlWithProfile(protocol.ProfileFor(protocol.Version2022), targetTrackDomeCode, 9, TargetTrackCommand{Mode: mode})
			if err != nil {
				t.Fatalf("builder error = %v", err)
			}
			text := string(body)
			if !strings.Contains(text, "<TargetTrack>"+string(mode)+"</TargetTrack>") {
				t.Fatalf("报文未携带 TargetTrack=%s: %s", mode, text)
			}
			// ⛔ 空结构体不能被序列化成 `<TargetArea></TargetArea>`：那会让设备以为
			// "平台给了一个 (0,0,0,0,0,0) 的框"，与"平台没给过框"完全不是一回事。
			if strings.Contains(text, "<TargetArea") {
				t.Fatalf("%s 不应携带 TargetArea: %s", mode, text)
			}
			if strings.Contains(text, "<DeviceID2") {
				t.Fatalf("%s 未指定全景通道时不应出现空的 DeviceID2: %s", mode, text)
			}
		})
	}
}

func TestBuildTargetTrackControl_RejectsMalformedCommands(t *testing.T) {
	area := manualTargetTrackArea
	tests := []struct {
		name    string
		command TargetTrackCommand
		want    string
	}{
		{
			name:    "manual without area",
			command: TargetTrackCommand{Mode: TargetTrackManual},
			want:    "必须携带 TargetArea",
		},
		{
			name: "stop with area",
			command: TargetTrackCommand{Mode: TargetTrackStop, Area: func() *TargetTrackArea {
				a := area()
				return &a
			}()},
			want: "不应携带 TargetArea",
		},
		{
			name:    "unknown mode",
			command: TargetTrackCommand{Mode: TargetTrackMode("Follow")},
			want:    "不是 Auto/Manual/Stop",
		},
		{
			// 窗口尺寸缺失时设备无法做比例换算 —— 标准明写「平台应提供画面大小」。
			name: "manual with zero window size",
			command: TargetTrackCommand{Mode: TargetTrackManual, Area: func() *TargetTrackArea {
				a := area()
				a.Length = 0
				return &a
			}()},
			want: "全景播放窗口尺寸必须为正数",
		},
		{
			name: "box center outside panorama window",
			command: TargetTrackCommand{Mode: TargetTrackManual, Area: func() *TargetTrackArea {
				a := area()
				a.MidPointX = a.Length + 1
				return &a
			}()},
			want: "中心横轴坐标超出",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := BuildTargetTrackControlWithProfile(protocol.ProfileFor(protocol.Version2022), targetTrackDomeCode, 3, tt.command); err == nil {
				t.Fatalf("期望被拒绝，实际构造成功")
			} else if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("错误信息 = %q, 期望包含 %q", err.Error(), tt.want)
			}
		})
	}
}

// ⛔ 贴着画面边缘的框**必须放行**（与 DragZoom 的"必须完整落在窗口内"刻意不同）。
//
// 拉框放大是平台自己算出来的区域，越界一定是我们算错了；而目标跟踪的框是操作员在画面上
// 拖出来的，被 UI 钳到边缘（中心恰好等于半个框宽）是正常操作。若照搬 DragZoom 的
// 完整包含规则，最靠边的目标会被平台自己挡在门外。
func TestValidateTargetTrackCommand_AllowsEdgeClampedBox(t *testing.T) {
	tests := []struct {
		name string
		area TargetTrackArea
	}{
		{
			name: "box flush to left edge",
			area: TargetTrackArea{Length: 1920, Width: 1080, MidPointX: 50, MidPointY: 540, LengthX: 100, LengthY: 100},
		},
		{
			name: "box flush to bottom-right corner",
			area: TargetTrackArea{Length: 1920, Width: 1080, MidPointX: 1870, MidPointY: 1030, LengthX: 100, LengthY: 100},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			box := tt.area
			if err := ValidateTargetTrackCommand(TargetTrackCommand{Mode: TargetTrackManual, Area: &box}); err != nil {
				t.Fatalf("贴边框被误拒: %v", err)
			}
		})
	}
}

func TestParseTargetTrackMode(t *testing.T) {
	tests := []struct {
		raw  string
		want TargetTrackMode
		ok   bool
	}{
		{raw: "auto", want: TargetTrackAuto, ok: true},
		{raw: " MANUAL ", want: TargetTrackManual, ok: true},
		{raw: "Stop", want: TargetTrackStop, ok: true},
		{raw: "AUTO", want: TargetTrackAuto, ok: true},
		{raw: "follow", ok: false},
		{raw: "", ok: false},
	}
	for _, tt := range tests {
		got, err := ParseTargetTrackMode(tt.raw)
		if tt.ok && err != nil {
			t.Errorf("ParseTargetTrackMode(%q) error = %v", tt.raw, err)
			continue
		}
		if !tt.ok {
			if err == nil {
				t.Errorf("ParseTargetTrackMode(%q) 期望报错", tt.raw)
			}
			continue
		}
		if got != tt.want {
			t.Errorf("ParseTargetTrackMode(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

// 落到报文里的永远是标准枚举的**原样拼写**，不是调用方给的大小写。
// 设备的 XML 解析多半是大小写敏感的字符串比较 —— 放宽输入不等于放宽输出。
func TestBuildTargetTrackControl_NormalizesModeSpelling(t *testing.T) {
	mode, err := ParseTargetTrackMode("manual")
	if err != nil {
		t.Fatalf("ParseTargetTrackMode error = %v", err)
	}
	box := manualTargetTrackArea()
	body, err := BuildTargetTrackControlWithProfile(protocol.ProfileFor(protocol.Version2022), targetTrackDomeCode, 1, TargetTrackCommand{Mode: mode, Area: &box})
	if err != nil {
		t.Fatalf("builder error = %v", err)
	}
	if !strings.Contains(string(body), "<TargetTrack>Manual</TargetTrack>") {
		t.Fatalf("报文里不是标准拼写 Manual: %s", body)
	}
}

// 2016 设备没有这个命令，但**不在报文层拒发**（同 FormatSDCard / SDCardStatus 的既有口径）：
// profile 只是"登记的说法"，一台被登记成 2016、实际按 2022 应答的设备，发出这一帧
// 是平台唯一能发现它的手段。
func TestBuildTargetTrackControl_NotGatedByProfileVersion(t *testing.T) {
	box := manualTargetTrackArea()
	body, err := BuildTargetTrackControlWithProfile(protocol.ProfileFor(protocol.Version2016), targetTrackDomeCode, 5, TargetTrackCommand{
		Mode: TargetTrackManual, Area: &box,
	})
	if err != nil {
		t.Fatalf("2016 profile 下不应拒发: %v", err)
	}
	if !strings.Contains(string(body), "<TargetTrack>Manual</TargetTrack>") {
		t.Fatalf("报文缺 TargetTrack: %s", body)
	}
}
