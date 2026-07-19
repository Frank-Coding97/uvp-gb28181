package manscdp

import (
	"strings"
	"testing"
)

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

func TestBuildPTZQueries(t *testing.T) {
	tests := []struct {
		name string
		build func() ([]byte, error)
		cmd string
	}{
		{"home", func() ([]byte, error) { return BuildHomePositionQuery("C", 1) }, CmdHomePositionQuery},
		{"track list", func() ([]byte, error) { return BuildCruiseTrackListQuery("C", 2) }, CmdCruiseTrackListQuery},
		{"track", func() ([]byte, error) { return BuildCruiseTrackQuery("C", 3, 7) }, CmdCruiseTrackQuery},
		{"status", func() ([]byte, error) { return BuildPTZPreciseStatusQuery("C", 4) }, CmdPTZPreciseStatusQuery},
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
		})
	}
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
	home, err := ParseHomePositionResponse([]byte(`<Response><CmdType>HomePositionQuery</CmdType><SN>1</SN><DeviceID>C</DeviceID><Enabled>true</Enabled></Response>`))
	if err != nil || home.Enabled == nil || !*home.Enabled {
		t.Fatalf("unexpected home response: %+v, err=%v", home, err)
	}
	list, err := ParseCruiseTrackListResponse([]byte(`<Response><CmdType>CruiseTrackListQuery</CmdType><SN>2</SN><DeviceID>C</DeviceID><TrackList><Track><TrackID>7</TrackID><Name>巡航一</Name></Track></TrackList></Response>`))
	if err != nil || len(list.Tracks) != 1 || list.Tracks[0].ID != 7 {
		t.Fatalf("unexpected track list: %+v, err=%v", list, err)
	}
	detail, err := ParseCruiseTrackResponse([]byte(`<Response><CmdType>CruiseTrackQuery</CmdType><SN>3</SN><DeviceID>C</DeviceID><Track><TrackID>7</TrackID><Name>巡航一</Name></Track></Response>`))
	if err != nil || detail.Track.ID != 7 {
		t.Fatalf("unexpected track detail: %+v, err=%v", detail, err)
	}
}
