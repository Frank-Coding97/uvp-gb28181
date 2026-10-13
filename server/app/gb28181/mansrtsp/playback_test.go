package mansrtsp

import (
	"errors"
	"testing"
	"time"
)

func TestMANSRTSPCommandBuilders(t *testing.T) {
	tests := []struct {
		name  string
		build func() ([]byte, error)
		want  string
	}{
		{
			name:  "play",
			build: func() ([]byte, error) { return BuildPlay(1) },
			want:  "PLAY RTSP/1.0\r\nCSeq: 1\r\n\r\n",
		},
		{
			name:  "resume",
			build: func() ([]byte, error) { return BuildResume(2) },
			want:  "PLAY RTSP/1.0\r\nCSeq: 2\r\nRange: npt=now-\r\n\r\n",
		},
		{
			name:  "pause",
			build: func() ([]byte, error) { return BuildPause(3) },
			want:  "PAUSE RTSP/1.0\r\nCSeq: 3\r\nPauseTime: now\r\n\r\n",
		},
		{
			name:  "seek",
			build: func() ([]byte, error) { return BuildSeek(4, 90*time.Second+500*time.Millisecond, 10*time.Minute) },
			want:  "PLAY RTSP/1.0\r\nCSeq: 4\r\nRange: npt=90.5-\r\n\r\n",
		},
		{
			name:  "scale",
			build: func() ([]byte, error) { return BuildScale(5, 0.25) },
			want:  "PLAY RTSP/1.0\r\nCSeq: 5\r\nScale: 0.25\r\n\r\n",
		},
		{
			name:  "teardown",
			build: func() ([]byte, error) { return BuildTeardown(6) },
			want:  "TEARDOWN RTSP/1.0\r\nCSeq: 6\r\n\r\n",
		},
	}

	if ContentType != "Application/MANSRTSP" {
		t.Fatalf("ContentType = %q", ContentType)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.build()
			if err != nil {
				t.Fatalf("builder error = %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("body = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMANSRTSPSeekRejectsPositionOutsideSegment(t *testing.T) {
	tests := []struct {
		name     string
		position time.Duration
		duration time.Duration
	}{
		{name: "negative", position: -time.Second, duration: time.Minute},
		{name: "at segment end", position: time.Minute, duration: time.Minute},
		{name: "after segment end", position: 61 * time.Second, duration: time.Minute},
		{name: "empty segment", position: 0, duration: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := BuildSeek(1, tt.position, tt.duration)
			if body != nil {
				t.Fatalf("BuildSeek() body = %q on invalid input", body)
			}
			assertInvalidArgument(t, err, "position")
		})
	}
}

func TestMANSRTSPScaleAllowsOnlyProductRates(t *testing.T) {
	wants := map[float64]string{
		0.25: "0.25",
		0.5:  "0.5",
		1:    "1.0",
		2:    "2.0",
		4:    "4.0",
	}
	for scale, wire := range wants {
		body, err := BuildScale(1, scale)
		if err != nil {
			t.Fatalf("BuildScale(%g) error = %v", scale, err)
		}
		want := "PLAY RTSP/1.0\r\nCSeq: 1\r\nScale: " + wire + "\r\n\r\n"
		if string(body) != want {
			t.Fatalf("BuildScale(%g) = %q, want %q", scale, body, want)
		}
	}
	for _, scale := range []float64{-1, 0, 3} {
		body, err := BuildScale(1, scale)
		if body != nil {
			t.Fatalf("BuildScale(%g) body = %q on invalid input", scale, body)
		}
		assertInvalidArgument(t, err, "scale")
	}
}

func TestMANSRTSPBuildersRejectInvalidCSeq(t *testing.T) {
	body, err := BuildPlay(0)
	if body != nil {
		t.Fatalf("BuildPlay(0) body = %q", body)
	}
	assertInvalidArgument(t, err, "cseq")
}

func TestParseMANSRTSPResponse(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus ResultStatus
		wantCode   int
		wantReason string
		wantCSeq   uint32
	}{
		{
			name:       "accepted",
			body:       "RTSP/1.0 200 OK\r\nCSeq: 12\r\n\r\n",
			wantStatus: ResultAccepted, wantCode: 200, wantReason: "OK", wantCSeq: 12,
		},
		{
			name:       "accepted without reason",
			body:       "RTSP/1.0 204\r\nCSeq: 13\r\n\r\n",
			wantStatus: ResultAccepted, wantCode: 204, wantCSeq: 13,
		},
		{
			name:       "rejected",
			body:       "RTSP/1.0 455 Method Not Valid in This State\r\nCSeq: 14\r\n\r\n",
			wantStatus: ResultRejected, wantCode: 455, wantReason: "Method Not Valid in This State", wantCSeq: 14,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseResponse([]byte(tt.body))
			if err != nil {
				t.Fatalf("ParseResponse() error = %v", err)
			}
			if got.Status != tt.wantStatus || got.StatusCode != tt.wantCode || got.Reason != tt.wantReason || got.CSeq != tt.wantCSeq {
				t.Fatalf("ParseResponse() = %#v", got)
			}
		})
	}
}

func TestParseMANSRTSPResponseRejectsMalformedBody(t *testing.T) {
	for _, body := range []string{
		"",
		"RTSP/1.0 OK\r\nCSeq: 1\r\n\r\n",
		"MANSRTSP/1.0 200 OK\r\nCSeq: 1\r\n\r\n",
		"RTSP/1.0 200 OK\r\n\r\n",
		"RTSP/1.0 200 OK\r\nCSeq: nope\r\n\r\n",
	} {
		t.Run(body, func(t *testing.T) {
			got, err := ParseResponse([]byte(body))
			if got != (Result{}) {
				t.Fatalf("ParseResponse() = %#v on malformed body", got)
			}
			if !errors.Is(err, ErrProtocol) {
				t.Fatalf("ParseResponse() error = %v, want ErrProtocol", err)
			}
			var protocolErr *ProtocolError
			if !errors.As(err, &protocolErr) {
				t.Fatalf("ParseResponse() error type = %T", err)
			}
		})
	}
}

func assertInvalidArgument(t *testing.T, err error, field string) {
	t.Helper()
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("error = %v, want ErrInvalidArgument", err)
	}
	var argumentErr *ArgumentError
	if !errors.As(err, &argumentErr) || argumentErr.Field != field {
		t.Fatalf("error = %#v, want field %q", err, field)
	}
}
