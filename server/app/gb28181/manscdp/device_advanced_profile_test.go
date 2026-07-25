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
			return BuildAlarmResetControlWithProfile(profile, "C", 4, AlarmResetOptions{AlarmMethod: "1/5", AlarmType: "1"})
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

func TestAlarmResetBuilderValidatesStandardEnums(t *testing.T) {
	valid := []AlarmResetOptions{
		{},
		{AlarmMethod: "0"},
		{AlarmMethod: "1/5/7", AlarmType: "1"},
		{AlarmMethod: "4", AlarmType: "5"},
	}
	for _, options := range valid {
		if _, err := BuildAlarmResetControlWithProfile(protocol.ProfileFor(protocol.Version2016), "C", 1, options); err != nil {
			t.Errorf("valid options %+v rejected: %v", options, err)
		}
	}
	invalid := []AlarmResetOptions{
		{AlarmMethod: "8"},
		{AlarmMethod: "0/1"},
		{AlarmMethod: "1/1"},
		{AlarmMethod: "1//2"},
		{AlarmMethod: "abc"},
		{AlarmType: "0"},
		{AlarmType: "6"},
		{AlarmMethod: "1", AlarmType: "x"},
	}
	for _, options := range invalid {
		if body, err := BuildAlarmResetControlWithProfile(protocol.ProfileFor(protocol.Version2016), "C", 1, options); err == nil || body != nil {
			t.Errorf("invalid options %+v generated body=%q err=%v", options, body, err)
		}
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
