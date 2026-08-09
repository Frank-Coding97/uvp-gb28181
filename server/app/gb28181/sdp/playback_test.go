package sdp

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBuildPlaybackSDP(t *testing.T) {
	got, err := BuildPlaybackSDP(PlaybackParams{
		ServerID:  "34020000002000000001",
		ChannelID: "34020000001320000001",
		RecvIP:    "192.0.2.10",
		RecvPort:  30000,
		SSRC:      "1402000001",
		Start:     time.Unix(1_000, 0),
		End:       time.Unix(2_000, 0),
		PlayFrom:  time.Unix(1_200, 0),
	})
	if err != nil {
		t.Fatalf("BuildPlaybackSDP() error = %v", err)
	}

	want := "v=0\r\n" +
		"o=34020000002000000001 0 0 IN IP4 192.0.2.10\r\n" +
		"s=Playback\r\n" +
		"u=34020000001320000001:0\r\n" +
		"c=IN IP4 192.0.2.10\r\n" +
		"t=1200 2000\r\n" +
		"m=video 30000 RTP/AVP 96 97 98 99\r\n" +
		"a=recvonly\r\n" +
		"a=rtpmap:96 PS/90000\r\n" +
		"a=rtpmap:97 MPEG4/90000\r\n" +
		"a=rtpmap:98 H264/90000\r\n" +
		"a=rtpmap:99 H265/90000\r\n" +
		"y=1402000001\r\n"
	if got != want {
		t.Fatalf("BuildPlaybackSDP() = %q, want %q", got, want)
	}
}

func TestBuildPlaybackSDPExtendedCompatibility(t *testing.T) {
	got, err := BuildPlaybackSDP(PlaybackParams{
		ServerID: "34020000002000000001", ChannelID: "34020000001320000001",
		RecvIP: "192.0.2.10", RecvPort: 30000, SSRC: "1402000001",
		Start: time.Unix(1_000, 0), End: time.Unix(2_000, 0), Extended: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"m=video 30000 RTP/AVP 96 126 125 99 34 98 97\r\n",
		"a=rtpmap:126 H264/90000\r\n",
		"a=rtpmap:125 H264S/90000\r\n",
		"a=rtpmap:99 H265/90000\r\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("扩展兼容回放 SDP 缺少 %q:\n%s", want, got)
		}
	}
}

func TestBuildPlaybackSDPDefaultsPlayFromToSegmentStart(t *testing.T) {
	got, err := BuildPlaybackSDP(PlaybackParams{
		ServerID:  "platform",
		ChannelID: "channel",
		RecvIP:    "192.0.2.10",
		RecvPort:  30000,
		SSRC:      "1402000001",
		Start:     time.Unix(1_000, 0),
		End:       time.Unix(2_000, 0),
	})
	if err != nil {
		t.Fatalf("BuildPlaybackSDP() error = %v", err)
	}
	if want := "t=1000 2000\r\n"; !strings.Contains(got, want) {
		t.Fatalf("BuildPlaybackSDP() missing default range %q: %q", want, got)
	}
}

func TestBuildPlaybackSDPRejectsInvalidTimeRange(t *testing.T) {
	start := time.Unix(1_000, 0)
	end := time.Unix(2_000, 0)
	tests := []struct {
		name     string
		start    time.Time
		end      time.Time
		playFrom time.Time
		field    string
	}{
		{name: "end equals start", start: start, end: start, field: "End"},
		{name: "end before start", start: end, end: start, field: "End"},
		{name: "playFrom before start", start: start, end: end, playFrom: start.Add(-time.Second), field: "PlayFrom"},
		{name: "playFrom at end", start: start, end: end, playFrom: end, field: "PlayFrom"},
		{name: "playFrom after end", start: start, end: end, playFrom: end.Add(time.Second), field: "PlayFrom"},
		{
			name:  "playFrom collapses to end at wire precision",
			start: start, end: end.Add(900 * time.Millisecond),
			playFrom: end.Add(100 * time.Millisecond), field: "PlayFrom",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildPlaybackSDP(PlaybackParams{
				ServerID: "platform", ChannelID: "channel", RecvIP: "192.0.2.10",
				RecvPort: 30000, SSRC: "1402000001",
				Start: tt.start, End: tt.end, PlayFrom: tt.playFrom,
			})
			if got != "" {
				t.Fatalf("BuildPlaybackSDP() body = %q on invalid input", got)
			}
			if !errors.Is(err, ErrInvalidPlaybackArgument) {
				t.Fatalf("BuildPlaybackSDP() error = %v, want ErrInvalidPlaybackArgument", err)
			}
			var argumentErr *PlaybackArgumentError
			if !errors.As(err, &argumentErr) || argumentErr.Field != tt.field {
				t.Fatalf("BuildPlaybackSDP() error = %#v, want field %q", err, tt.field)
			}
		})
	}
}
