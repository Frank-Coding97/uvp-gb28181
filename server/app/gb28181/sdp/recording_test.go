package sdp

import (
	"strings"
	"testing"
	"time"
)

func mustSDPTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02T15:04:05", value, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestBuildPlaybackSDP(t *testing.T) {
	body, err := BuildPlaybackSDP(RecordingParams{
		ServerID: "34020000002000000001", RecvIP: "192.168.1.10", RecvPort: 31000,
		SSRC: "1402000001", StartTime: mustSDPTime(t, "2026-07-19T08:00:00"), EndTime: mustSDPTime(t, "2026-07-19T09:00:00"),
	})
	if err != nil {
		t.Fatalf("BuildPlaybackSDP() error = %v", err)
	}
	for _, want := range []string{
		"s=Playback", "t=20260719T080000 20260719T090000", "m=video 31000 RTP/AVP 96 98 97", "y=1402000001",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Playback SDP missing %q:\n%s", want, body)
		}
	}
}

func TestBuildDownloadSDP(t *testing.T) {
	body, err := BuildDownloadSDP(RecordingParams{
		ServerID: "34020000002000000001", RecvIP: "192.168.1.10", RecvPort: 31000,
		SSRC: "1402000002", StartTime: mustSDPTime(t, "2026-07-19T08:00:00"), EndTime: mustSDPTime(t, "2026-07-19T09:00:00"),
		TCPMode: true, DownloadSpeed: 4,
	})
	if err != nil {
		t.Fatalf("BuildDownloadSDP() error = %v", err)
	}
	for _, want := range []string{
		"s=Download", "t=20260719T080000 20260719T090000", "a=setup:passive", "a=connection:new", "a=downloadspeed:4", "y=1402000002",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Download SDP missing %q:\n%s", want, body)
		}
	}
}

func TestBuildRecordingSDPRejectsInvalidInput(t *testing.T) {
	valid := RecordingParams{
		ServerID: "server", RecvIP: "192.168.1.10", RecvPort: 31000,
		SSRC: "1402000001", StartTime: mustSDPTime(t, "2026-07-19T08:00:00"), EndTime: mustSDPTime(t, "2026-07-19T09:00:00"),
	}
	cases := []RecordingParams{
		{ServerID: valid.ServerID, RecvIP: valid.RecvIP, RecvPort: valid.RecvPort, SSRC: valid.SSRC, StartTime: valid.EndTime, EndTime: valid.StartTime},
		{ServerID: valid.ServerID, RecvIP: valid.RecvIP, RecvPort: 0, SSRC: valid.SSRC, StartTime: valid.StartTime, EndTime: valid.EndTime},
		{ServerID: valid.ServerID, RecvIP: valid.RecvIP, RecvPort: valid.RecvPort, SSRC: "bad", StartTime: valid.StartTime, EndTime: valid.EndTime},
	}
	for _, input := range cases {
		if _, err := BuildPlaybackSDP(input); err == nil {
			t.Fatalf("BuildPlaybackSDP(%+v) should fail", input)
		}
	}
	invalidSpeed := valid
	invalidSpeed.DownloadSpeed = 3
	if _, err := BuildDownloadSDP(invalidSpeed); err == nil {
		t.Fatal("unsupported download speed should fail")
	}
}
