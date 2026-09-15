package manscdp

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"

	"uvplatform.cn/uvp-gb28181/app/gb28181/protocol"
)

func TestHomePositionProtocol(t *testing.T) {
	t.Run("optional fields may be omitted", func(t *testing.T) {
		body, err := BuildHomePositionControl("C", 1, HomePositionControl{Enabled: true})
		if err != nil {
			t.Fatalf("optional ResetTime and PresetIndex must be independently omittable: %v", err)
		}
		if strings.Contains(string(body), "ResetTime") || strings.Contains(string(body), "PresetIndex") {
			t.Fatalf("omitted optional fields must not be encoded: %s", body)
		}
	})

	t.Run("disabled ignores enable-only fields", func(t *testing.T) {
		body, err := BuildHomePositionControl("C", 2, HomePositionControl{
			Enabled: false, ResetTime: intPointer(-1), PresetIndex: intPointer(256),
		})
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		if !strings.Contains(text, "<Enabled>0</Enabled>") || strings.Contains(text, "ResetTime") || strings.Contains(text, "PresetIndex") {
			t.Fatalf("disabled control must only encode Enabled=0: %s", body)
		}
	})

	t.Run("zero and maximum values are preserved", func(t *testing.T) {
		body, err := BuildHomePositionControl("C", 3, HomePositionControl{
			Enabled: true, ResetTime: intPointer(0), PresetIndex: intPointer(0),
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"<Enabled>1</Enabled>", "<ResetTime>0</ResetTime>", "<PresetIndex>0</PresetIndex>"} {
			if !strings.Contains(string(body), want) {
				t.Fatalf("body missing %q: %s", want, body)
			}
		}

		body, err = BuildHomePositionControl("C", 4, HomePositionControl{Enabled: true, PresetIndex: intPointer(255)})
		if err != nil || !strings.Contains(string(body), "<PresetIndex>255</PresetIndex>") || strings.Contains(string(body), "ResetTime") {
			t.Fatalf("PresetIndex=255 must be preserved independently: %s, err=%v", body, err)
		}
	})

	for _, test := range []struct {
		name    string
		command HomePositionControl
	}{
		{"negative reset", HomePositionControl{Enabled: true, ResetTime: intPointer(-1)}},
		{"negative preset", HomePositionControl{Enabled: true, PresetIndex: intPointer(-1)}},
		{"preset above 255", HomePositionControl{Enabled: true, PresetIndex: intPointer(256)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := BuildHomePositionControl("C", 5, test.command); err == nil {
				t.Fatal("expected protocol argument error")
			}
		})
	}
}

func intPointer(value int) *int {
	return &value
}

func TestBuildPTZPreciseControl(t *testing.T) {
	pan, tilt, zoom := 12.5, -3.25, 4.0
	body, err := BuildPTZPreciseControl("C", 10, PTZPreciseControl{
		Pan: &pan, Tilt: &tilt, Zoom: &zoom, Speed: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{"<CmdType>PTZPreciseCtrl</CmdType>", "<SN>10</SN>", "<DeviceID>C</DeviceID>", "<Pan>12.5</Pan>", "<Tilt>-3.25</Tilt>", "<Zoom>4</Zoom>", "<Speed>8</Speed>"} {
		if !strings.Contains(text, want) {
			t.Fatalf("body missing %q: %s", want, text)
		}
	}
}

func TestBuildPTZPreciseDeviceControl2022(t *testing.T) {
	profile := protocol.ProfileFor(protocol.Version2022)
	pan, tilt, zoom := 12.5, -3.25, 4.0
	body, err := BuildPTZPreciseDeviceControlWithProfile(profile, "C", 10, PTZPreciseControl{
		Pan: &pan, Tilt: &tilt, Zoom: &zoom,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		"encoding=\"GB18030\"",
		"<CmdType>DeviceControl</CmdType>",
		"<SN>10</SN>",
		"<DeviceID>C</DeviceID>",
		"<PTZPreciseCtrl>",
		"<Pan>12.5</Pan>",
		"<Tilt>-3.25</Tilt>",
		"<Zoom>4</Zoom>",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("body missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "<Focus>") || strings.Contains(text, "<Iris>") || strings.Contains(text, "<Speed>") {
		t.Fatalf("2022 precise control must contain only Pan/Tilt/Zoom: %s", text)
	}

	focus := 1.0
	if _, err := BuildPTZPreciseDeviceControlWithProfile(profile, "C", 11, PTZPreciseControl{Focus: &focus}); err == nil {
		t.Fatal("2022 precise control must reject Focus")
	}
	if _, err := BuildPTZPreciseDeviceControlWithProfile(profile, "C", 12, PTZPreciseControl{Speed: 1}); err == nil {
		t.Fatal("2022 precise control must reject Speed")
	}
	if _, err := BuildPTZPreciseDeviceControlWithProfile(profile, "C", 13, PTZPreciseControl{}); err == nil {
		t.Fatal("2022 precise control must require at least one of Pan/Tilt/Zoom")
	}
}

func TestPTZPreciseStatusProfile2022(t *testing.T) {
	profile := protocol.ProfileFor(protocol.Version2022)
	body, err := BuildPTZPreciseStatusQueryWithProfile(profile, "C", 4)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "<CmdType>PTZPosition</CmdType>") {
		t.Fatalf("2022 precise status query must use PTZPosition: %s", body)
	}

	valid := []byte("<Response><CmdType>PTZPosition</CmdType><SN>4</SN><DeviceID>C</DeviceID><Pan>1</Pan></Response>")
	if _, err := ParsePTZPreciseStatusResponseWithProfile(profile, valid); err != nil {
		t.Fatalf("2022 PTZPosition response should parse: %v", err)
	}
	legacy := []byte("<Response><CmdType>PTZPreciseStatusQuery</CmdType><SN>4</SN><DeviceID>C</DeviceID><Pan>1</Pan></Response>")
	if _, err := ParsePTZPreciseStatusResponseWithProfile(profile, legacy); err == nil {
		t.Fatal("2022 parser must reject the legacy precise-status command")
	}
}

func TestBuildPTZQueries(t *testing.T) {
	tests := []struct {
		name  string
		build func() ([]byte, error)
		cmd   string
	}{
		{"home", func() ([]byte, error) { return BuildHomePositionQuery("C", 1) }, CmdHomePositionQuery},
		{"track list", func() ([]byte, error) { return BuildCruiseTrackListQuery("C", 2) }, CmdCruiseTrackListQuery},
		{"track", func() ([]byte, error) { return BuildCruiseTrackQuery("C", 3, 7) }, CmdCruiseTrackQuery},
		{"status", func() ([]byte, error) { return BuildPTZPreciseStatusQuery("C", 4) }, CmdPTZPreciseStatusQuery},
		{"preset", func() ([]byte, error) { return BuildPresetQuery("C", 5) }, CmdPresetQuery},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := tt.build()
			if err != nil {
				t.Fatal(err)
			}
			text := string(body)
			if !strings.Contains(text, "<CmdType>"+tt.cmd+"</CmdType>") || !strings.Contains(text, "<DeviceID>C</DeviceID>") {
				t.Fatalf("unexpected query: %s", text)
			}
			if tt.cmd == CmdCruiseTrackQuery && (!strings.Contains(text, "<Number>7</Number>") || strings.Contains(text, "<TrackID>")) {
				t.Fatalf("cruise track query must use Number: %s", text)
			}
		})
	}
}

func TestBuildHomePositionControl(t *testing.T) {
	body, err := BuildHomePositionControl("C", 6, HomePositionControl{Enabled: true, ResetTime: intPointer(30), PresetIndex: intPointer(4)})
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{"<CmdType>DeviceControl</CmdType>", "<HomePosition>", "<Enabled>1</Enabled>", "<ResetTime>30</ResetTime>", "<PresetIndex>4</PresetIndex>"} {
		if !strings.Contains(text, want) {
			t.Fatalf("body missing %q: %s", want, text)
		}
	}

	body, err = BuildHomePositionControl("C", 7, HomePositionControl{Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "ResetTime") || strings.Contains(string(body), "PresetIndex") {
		t.Fatalf("disabled home position must omit enable-only fields: %s", body)
	}
}

func TestParseHomePositionResponse(t *testing.T) {
	for _, encoding := range []string{"UTF-8", "GB2312", "GB18030"} {
		t.Run(encoding, func(t *testing.T) {
			body := encodeHomePositionXML(t, encoding)
			response, err := ParseHomePositionResponse(body, HomePositionParseOptions{})
			if err != nil {
				t.Fatal(err)
			}
			config := response.HomePosition
			if response.XMLName.Local != "Response" || response.DeviceID != "C" || response.SN != 9 || config == nil {
				t.Fatalf("unexpected response: %+v", response)
			}
			if !config.Enabled || config.ResetTime == nil || *config.ResetTime != 0 || config.PresetIndex == nil || *config.PresetIndex != 255 {
				t.Fatalf("nested values were not preserved: %+v", config)
			}
			if config.EnabledEncoding != HomePositionEnabledEncodingNumeric {
				t.Fatalf("unexpected Enabled encoding: %q", config.EnabledEncoding)
			}
		})
	}

	t.Run("namespace and extensions", func(t *testing.T) {
		body := []byte(`<gb:Response xmlns:gb="urn:gb28181"><gb:CmdType>HomePositionQuery</gb:CmdType><gb:SN>10</gb:SN><gb:DeviceID>C</gb:DeviceID><gb:HomePosition><gb:Enabled>0</gb:Enabled><gb:VendorField>ignored</gb:VendorField></gb:HomePosition><gb:Extension>ignored</gb:Extension></gb:Response>`)
		response, err := ParseHomePositionResponse(body, HomePositionParseOptions{})
		if err != nil || response.HomePosition == nil || response.HomePosition.Enabled {
			t.Fatalf("namespace response did not parse by local name: %+v, err=%v", response, err)
		}
	})

	t.Run("no data ignores legacy flat fields", func(t *testing.T) {
		body := []byte(`<Response><CmdType>HomePositionQuery</CmdType><SN>11</SN><DeviceID>C</DeviceID><Enabled>1</Enabled><Pan>1</Pan></Response>`)
		response, err := ParseHomePositionResponse(body, HomePositionParseOptions{})
		if err != nil || response.HomePosition != nil {
			t.Fatalf("response without nested HomePosition must be valid no-data: %+v, err=%v", response, err)
		}
	})

	t.Run("boolean compatibility is explicit", func(t *testing.T) {
		body := []byte(`<Response><CmdType>HomePositionQuery</CmdType><SN>12</SN><DeviceID>C</DeviceID><HomePosition><Enabled>true</Enabled></HomePosition></Response>`)
		if response, err := ParseHomePositionResponse(body, HomePositionParseOptions{}); err == nil || response != nil {
			t.Fatalf("boolean text must be rejected by default: %+v, err=%v", response, err)
		}
		response, err := ParseHomePositionResponse(body, HomePositionParseOptions{AllowBooleanEnabled: true})
		if err != nil || response.HomePosition == nil || !response.HomePosition.Enabled || response.HomePosition.EnabledEncoding != HomePositionEnabledEncodingCompatBooleanText {
			t.Fatalf("explicit boolean compatibility failed: %+v, err=%v", response, err)
		}

		body = []byte(`<Response><CmdType>HomePositionQuery</CmdType><SN>13</SN><DeviceID>C</DeviceID><HomePosition><Enabled>false</Enabled></HomePosition></Response>`)
		response, err = ParseHomePositionResponse(body, HomePositionParseOptions{AllowBooleanEnabled: true})
		if err != nil || response.HomePosition == nil || response.HomePosition.Enabled || response.HomePosition.EnabledEncoding != HomePositionEnabledEncodingCompatBooleanText {
			t.Fatalf("false compatibility failed: %+v, err=%v", response, err)
		}
	})
}

func TestParseHomePositionResponseRejectsInvalid(t *testing.T) {
	tests := map[string]string{
		"wrong root":            `<Notify><CmdType>HomePositionQuery</CmdType><SN>1</SN><DeviceID>C</DeviceID></Notify>`,
		"wrong root case":       `<response><CmdType>HomePositionQuery</CmdType><SN>1</SN><DeviceID>C</DeviceID></response>`,
		"wrong CmdType":         `<Response><CmdType>DeviceControl</CmdType><SN>1</SN><DeviceID>C</DeviceID></Response>`,
		"non-positive SN":       `<Response><CmdType>HomePositionQuery</CmdType><SN>0</SN><DeviceID>C</DeviceID></Response>`,
		"invalid SN":            `<Response><CmdType>HomePositionQuery</CmdType><SN>x</SN><DeviceID>C</DeviceID></Response>`,
		"empty DeviceID":        `<Response><CmdType>HomePositionQuery</CmdType><SN>1</SN><DeviceID> </DeviceID></Response>`,
		"missing Enabled":       `<Response><CmdType>HomePositionQuery</CmdType><SN>1</SN><DeviceID>C</DeviceID><HomePosition><ResetTime>1</ResetTime></HomePosition></Response>`,
		"empty Enabled":         `<Response><CmdType>HomePositionQuery</CmdType><SN>1</SN><DeviceID>C</DeviceID><HomePosition><Enabled> </Enabled></HomePosition></Response>`,
		"unknown Enabled":       `<Response><CmdType>HomePositionQuery</CmdType><SN>1</SN><DeviceID>C</DeviceID><HomePosition><Enabled>2</Enabled></HomePosition></Response>`,
		"boolean by default":    `<Response><CmdType>HomePositionQuery</CmdType><SN>1</SN><DeviceID>C</DeviceID><HomePosition><Enabled>true</Enabled></HomePosition></Response>`,
		"invalid ResetTime":     `<Response><CmdType>HomePositionQuery</CmdType><SN>1</SN><DeviceID>C</DeviceID><HomePosition><Enabled>1</Enabled><ResetTime>x</ResetTime></HomePosition></Response>`,
		"negative ResetTime":    `<Response><CmdType>HomePositionQuery</CmdType><SN>1</SN><DeviceID>C</DeviceID><HomePosition><Enabled>1</Enabled><ResetTime>-1</ResetTime></HomePosition></Response>`,
		"invalid PresetIndex":   `<Response><CmdType>HomePositionQuery</CmdType><SN>1</SN><DeviceID>C</DeviceID><HomePosition><Enabled>1</Enabled><PresetIndex>x</PresetIndex></HomePosition></Response>`,
		"negative PresetIndex":  `<Response><CmdType>HomePositionQuery</CmdType><SN>1</SN><DeviceID>C</DeviceID><HomePosition><Enabled>1</Enabled><PresetIndex>-1</PresetIndex></HomePosition></Response>`,
		"PresetIndex above 255": `<Response><CmdType>HomePositionQuery</CmdType><SN>1</SN><DeviceID>C</DeviceID><HomePosition><Enabled>1</Enabled><PresetIndex>256</PresetIndex></HomePosition></Response>`,
		"malformed XML":         `<Response><CmdType>HomePositionQuery</CmdType>`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			response, err := ParseHomePositionResponse([]byte(body), HomePositionParseOptions{})
			if err == nil || response != nil {
				t.Fatalf("invalid response must fail without partial config: %+v, err=%v", response, err)
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

func encodeHomePositionXML(t *testing.T, charset string) []byte {
	t.Helper()
	source := `<?xml version="1.0" encoding="` + charset + `"?><Response><CmdType>HomePositionQuery</CmdType><SN>9</SN><DeviceID>C</DeviceID><HomePosition><Enabled>1</Enabled><ResetTime>0</ResetTime><PresetIndex>255</PresetIndex><VendorName>中文</VendorName></HomePosition></Response>`
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

func TestParsePTZPreciseStatusResponse_GB2312(t *testing.T) {
	resp, err := ParsePTZPreciseStatusResponse([]byte(`<?xml version="1.0" encoding="GB2312"?><Response><CmdType>PTZPreciseStatusQuery</CmdType><SN>8</SN><DeviceID>C</DeviceID><Pan>12.5</Pan><Tilt>-3.25</Tilt><Zoom>4</Zoom><Focus>2</Focus><Iris>1</Iris></Response>`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.DeviceID != "C" || resp.SN != 8 || resp.Pan == nil || *resp.Pan != 12.5 || resp.Tilt == nil || *resp.Tilt != -3.25 {
		t.Fatalf("unexpected status: %+v", resp)
	}
}

func TestBuildPTZPreciseControlRejectsInvalid(t *testing.T) {
	bad := 1.23456789
	if _, err := BuildPTZPreciseControl("C", 1, PTZPreciseControl{Pan: &bad}); err == nil {
		t.Fatal("expected precision error")
	}
	if _, err := BuildPTZPreciseStatusQuery("", 1); err == nil {
		t.Fatal("expected empty device error")
	}
}

func TestParseHomeAndCruiseResponses(t *testing.T) {
	home, err := ParseHomePositionResponse([]byte(`<Response><CmdType>HomePositionQuery</CmdType><SN>1</SN><DeviceID>C</DeviceID><HomePosition><Enabled>1</Enabled></HomePosition></Response>`), HomePositionParseOptions{})
	if err != nil || home.HomePosition == nil || !home.HomePosition.Enabled {
		t.Fatalf("unexpected home response: %+v, err=%v", home, err)
	}
	list, err := ParseCruiseTrackListResponse([]byte(`<Response><CmdType>CruiseTrackListQuery</CmdType><SN>2</SN><DeviceID>C</DeviceID><SumNum>1</SumNum><CruiseTrackList Num="1"><CruiseTrack><Number>0</Number><Name>T0</Name></CruiseTrack></CruiseTrackList></Response>`))
	if err != nil || list.SumNum != 1 || list.List.Num != 1 || len(list.List.Tracks) != 1 || list.List.Tracks[0].ID != 0 {
		t.Fatalf("unexpected track list: %+v, err=%v", list, err)
	}
	detail, err := ParseCruiseTrackResponse([]byte(`<Response><CmdType>CruiseTrackQuery</CmdType><SN>3</SN><DeviceID>C</DeviceID><Number>0</Number><Name>T0</Name><SumNum>1</SumNum><CruisePointList Num="1"><CruisePoint><PresetIndex>3</PresetIndex><StayTime>5</StayTime><Speed>8</Speed></CruisePoint></CruisePointList></Response>`))
	if err != nil || detail.CruiseTrack.ID != 0 || detail.CruiseTrack.PointList.Num != 1 || len(detail.CruiseTrack.PointList.Points) != 1 || detail.CruiseTrack.PointList.Points[0].PresetIndex != 3 {
		t.Fatalf("unexpected track detail: %+v, err=%v", detail, err)
	}
}

func TestParsePresetResponse_PreservesCompleteList(t *testing.T) {
	var items strings.Builder
	for i := 1; i <= 20; i++ {
		items.WriteString("<Item><PresetID>" + strconv.Itoa(i) + "</PresetID><PresetName>P" + strconv.Itoa(i) + "</PresetName></Item>")
	}
	body := []byte(`<Response><CmdType>PresetQuery</CmdType><SN>5</SN><DeviceID>C</DeviceID><SumNum>20</SumNum><PresetList Num="20">` + items.String() + `</PresetList></Response>`)
	response, err := ParsePresetResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Presets) != 20 || response.Presets[19].ID != 20 {
		t.Fatalf("unexpected preset response: %+v", response)
	}
}
