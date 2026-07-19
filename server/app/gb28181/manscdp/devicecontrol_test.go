package manscdp

import (
	"strings"
	"testing"
)

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
