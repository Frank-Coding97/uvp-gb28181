package manscdp

import (
	"errors"
	"strings"
	"testing"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

func TestAdvancedControlBuildersWithProfileUseVersionedIFrameElement(t *testing.T) {
	for _, test := range []struct {
		name        string
		profile     protocol.Profile
		element     string
		declaration string
	}{
		{"2016", protocol.ProfileFor(protocol.Version2016), "IFameCmd", "GB2312"},
		{"2022", protocol.ProfileFor(protocol.Version2022), "IFrameCmd", "GB18030"},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, err := BuildIFrameControlWithProfile(test.profile, "通道", 1)
			if err != nil {
				t.Fatal(err)
			}
			text := string(body)
			if !strings.HasPrefix(text, `<?xml version="1.0" encoding="`+test.declaration+`"?>`) {
				t.Fatalf("declaration=%q, body=%q", test.declaration, text)
			}
			if !strings.Contains(text, "<"+test.element+">Send</"+test.element+">") {
				t.Fatalf("body does not contain profile element %s: %s", test.element, text)
			}
			for _, other := range []string{"IFameCmd", "IFrameCmd"} {
				if other != test.element && strings.Contains(text, "<"+other+">") {
					t.Fatalf("body contains non-profile element %s: %s", other, text)
				}
			}
		})
	}
}

func TestAdvancedControlBuildersWithProfileShareCodecAndTarget(t *testing.T) {
	profile := protocol.ProfileFor(protocol.Version2022)
	builders := []struct {
		name  string
		build func() ([]byte, error)
		want  string
	}{
		{"record", func() ([]byte, error) {
			return BuildRecordControlWithProfile(profile, "C", 2, RecordStart)
		}, "<RecordCmd>Record</RecordCmd>"},
		{"guard", func() ([]byte, error) {
			return BuildGuardControlWithProfile(profile, "C", 3, GuardReset)
		}, "<GuardCmd>ResetGuard</GuardCmd>"},
		{"alarm", func() ([]byte, error) {
			return BuildAlarmResetControlWithProfile(profile, "C", 4, AlarmResetOptions{AlarmMethod: "5", AlarmType: "1"})
		}, "<AlarmCmd>ResetAlarm</AlarmCmd>"},
		{"teleboot", func() ([]byte, error) {
			return BuildTeleBootControlWithProfile(profile, "C", 5, true)
		}, "<TeleBoot>Boot</TeleBoot>"},
		{"drag", func() ([]byte, error) {
			return BuildDragZoomControlWithProfile(profile, "C", 6, DragZoomCommand{Direction: DragZoomOut, Region: DragZoomRegion{Length: 800, Width: 450, MidPointX: 400, MidPointY: 225, LengthX: 200, LengthY: 100}})
		}, "<DragZoomOut>"},
	}
	for _, test := range builders {
		t.Run(test.name, func(t *testing.T) {
			body, err := test.build()
			if err != nil {
				t.Fatal(err)
			}
			text := string(body)
			if !strings.Contains(text, "<DeviceID>C</DeviceID>") || !strings.Contains(text, test.want) {
				t.Fatalf("unexpected body: %s", text)
			}
			if !strings.HasPrefix(text, `<?xml version="1.0" encoding="GB18030"?>`) {
				t.Fatalf("profile charset not used: %s", text)
			}
		})
	}
}

func TestRecordControlWithProfileUsesVersionedStreamNumber(t *testing.T) {
	for _, test := range []struct {
		name             string
		profile          protocol.Profile
		action           RecordAction
		wantStreamNumber bool
	}{
		{name: "2016 start", profile: protocol.ProfileFor(protocol.Version2016), action: RecordStart},
		{name: "2016 stop", profile: protocol.ProfileFor(protocol.Version2016), action: RecordStop},
		{name: "2022 start", profile: protocol.ProfileFor(protocol.Version2022), action: RecordStart, wantStreamNumber: true},
		{name: "2022 stop", profile: protocol.ProfileFor(protocol.Version2022), action: RecordStop, wantStreamNumber: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, err := BuildRecordControlWithProfile(test.profile, "C", 1, test.action)
			if err != nil {
				t.Fatal(err)
			}
			hasStreamNumber := strings.Contains(string(body), "<StreamNumber>0</StreamNumber>")
			if hasStreamNumber != test.wantStreamNumber {
				t.Fatalf("StreamNumber presence=%t, want %t: %s", hasStreamNumber, test.wantStreamNumber, body)
			}
		})
	}
}

func TestAlarmResetBuilderValidatesStandardEnums(t *testing.T) {
	valid2022 := []AlarmResetOptions{
		{},
		{AlarmMethod: "0"},
		{AlarmMethod: "2"},
		{AlarmMethod: "2", AlarmType: "1"},
		{AlarmMethod: "2", AlarmType: "5"},
		{AlarmMethod: "5"},
		{AlarmMethod: "5", AlarmType: "1"},
		{AlarmMethod: "5", AlarmType: "13"},
		{AlarmMethod: "6"},
		{AlarmMethod: "6", AlarmType: "1"},
		{AlarmMethod: "6", AlarmType: "2"},
		{AlarmMethod: "2/5/6", AlarmType: "1"},
	}
	for _, options := range valid2022 {
		if _, err := BuildAlarmResetControlWithProfile(protocol.ProfileFor(protocol.Version2022), "C", 1, options); err != nil {
			t.Errorf("valid 2022 options %+v rejected: %v", options, err)
		}
	}
	if _, err := BuildAlarmResetControlWithProfile(protocol.ProfileFor(protocol.Version2016), "C", 1, AlarmResetOptions{}); err != nil {
		t.Fatalf("empty 2016 options rejected: %v", err)
	}
	invalid := []AlarmResetOptions{
		{AlarmMethod: "8"},
		{AlarmMethod: "0/1"},
		{AlarmMethod: "1/1"},
		{AlarmMethod: "1//2"},
		{AlarmMethod: "abc"},
		{AlarmMethod: "0", AlarmType: "1"},
		{AlarmMethod: "1", AlarmType: "1"},
		{AlarmMethod: "3", AlarmType: "1"},
		{AlarmMethod: "4", AlarmType: "1"},
		{AlarmMethod: "7", AlarmType: "1"},
		{AlarmMethod: "2", AlarmType: "6"},
		{AlarmMethod: "5", AlarmType: "14"},
		{AlarmMethod: "6", AlarmType: "3"},
		{AlarmMethod: "1/5", AlarmType: "1"},
		{AlarmMethod: "2/5", AlarmType: "13"},
		{AlarmType: "1"},
		{AlarmType: "0"},
		{AlarmType: "14"},
		{AlarmMethod: "5", AlarmType: "x"},
	}
	for _, options := range invalid {
		if body, err := BuildAlarmResetControlWithProfile(protocol.ProfileFor(protocol.Version2022), "C", 1, options); err == nil || body != nil {
			t.Errorf("invalid 2022 options %+v generated body=%q err=%v", options, body, err)
		}
		if body, err := BuildAlarmResetControlWithProfile(protocol.ProfileFor(protocol.Version2016), "C", 1, options); err == nil || body != nil {
			t.Errorf("invalid 2016 options %+v generated body=%q err=%v", options, body, err)
		}
	}
}

func TestAlarmResetBuilderEmitsInfoOnlyFor2022Selectors(t *testing.T) {
	legacy, err := BuildAlarmResetControlWithProfile(protocol.ProfileFor(protocol.Version2016), "C", 1, AlarmResetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(legacy), "<Info>") {
		t.Fatalf("empty 2016 reset unexpectedly emitted Info: %s", legacy)
	}

	modern, err := BuildAlarmResetControlWithProfile(protocol.ProfileFor(protocol.Version2022), "C", 1, AlarmResetOptions{AlarmMethod: "5", AlarmType: "13"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(modern)
	if !strings.Contains(text, "<Info>") || !strings.Contains(text, "<AlarmMethod>5</AlarmMethod>") || !strings.Contains(text, "<AlarmType>13</AlarmType>") {
		t.Fatalf("2022 reset did not emit selector Info: %s", modern)
	}

	if body, err := BuildAlarmResetControlWithProfile(protocol.ProfileFor(protocol.Version2016), "C", 1, AlarmResetOptions{AlarmMethod: "5", AlarmType: "13"}); err == nil || body != nil {
		t.Fatalf("2016 selectors must be rejected, body=%q err=%v", body, err)
	}
}

func TestAdvancedControlResponseParserIsStrictAndProfileAware(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="GB18030"?><Response><CmdType>DeviceControl</CmdType><SN>7</SN><DeviceID>C</DeviceID><Result> OK </Result></Response>`)
	response, err := ParseAdvancedControlResponseWithProfile(protocol.ProfileFor(protocol.Version2022), body)
	if err != nil {
		t.Fatal(err)
	}
	if response.Result != DeviceControlResultOK || response.SN != 7 || response.DeviceID != "C" {
		t.Fatalf("unexpected response: %+v", response)
	}
	for _, invalid := range []string{
		`<Notify><CmdType>DeviceControl</CmdType><SN>1</SN><DeviceID>C</DeviceID><Result>OK</Result></Notify>`,
		`<Response><CmdType>Other</CmdType><SN>1</SN><DeviceID>C</DeviceID><Result>OK</Result></Response>`,
		`<Response><CmdType>DeviceControl</CmdType><SN>1</SN><DeviceID>C</DeviceID><Result>NOT OK</Result></Response>`,
	} {
		got, parseErr := ParseAdvancedControlResponseWithProfile(protocol.ProfileFor(protocol.Version2016), []byte(invalid))
		if got != nil || parseErr == nil || !errors.Is(parseErr, ErrInvalidResponse) {
			t.Errorf("invalid response accepted: response=%+v err=%v", got, parseErr)
		}
	}
}
