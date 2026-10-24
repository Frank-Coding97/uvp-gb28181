package mansrtsp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseRequestPlaybackControls(t *testing.T) {
	for _, test := range []struct {
		body, action    string
		position, scale float64
	}{
		{"PAUSE RTSP/1.0\r\nCSeq: 1\r\nPauseTime: now\r\n\r\n", "pause", 0, 0},
		{"PLAY RTSP/1.0\r\nCSeq: 1\r\nRange: npt=now-\r\n\r\n", "resume", 0, 0},
		{"PLAY RTSP/1.0\r\nCSeq: 1\r\nRange: npt=12.5-\r\n\r\n", "seek", 12.5, 0},
		{"PLAY RTSP/1.0\r\nCSeq: 1\r\nScale: 2.0\r\n\r\n", "scale", 0, 2},
		{"PLAY RTSP/1.0\r\nCSeq: 1\r\nScale: -1.0\r\n\r\n", "scale", 0, -1},
		{"TEARDOWN RTSP/1.0\r\nCSeq: 1\r\n\r\n", "stop", 0, 0},
	} {
		t.Run(test.action, func(t *testing.T) {
			request, err := ParseRequest([]byte(test.body))
			require.NoError(t, err)
			require.EqualValues(t, 1, request.CSeq)
			require.Equal(t, test.action, request.Action)
			require.Equal(t, test.position, request.PositionSeconds)
			require.Equal(t, test.scale, request.Scale)
		})
	}
	for _, body := range []string{
		"PLAY RTSP/1.0\r\nRange: npt=0-\r\n\r\n",
		"PLAY RTSP/1.0\r\nCSeq: 0\r\n\r\n",
		"PLAY RTSP/1.0\r\nCSeq: 1\r\nCSeq: 2\r\n\r\n",
		"PLAY RTSP/1.0\r\nCSeq: 1\r\nRange: npt=NaN-\r\n\r\n",
		"PLAY RTSP/1.0\r\nCSeq: 1\r\nRange: npt=-1-\r\n\r\n",
		"PLAY RTSP/1.0\r\nCSeq: 1\r\nScale: -2\r\n\r\n",
		"PLAY RTSP/1.0\r\nCSeq: 1\r\nScale: 8\r\n\r\n",
		"PLAY RTSP/1.0\r\nCSeq: 1\r\nScale: 2\r\nRange: npt=1-\r\n\r\n",
	} {
		_, err := ParseRequest([]byte(body))
		require.Error(t, err, body)
	}
}

func TestParseRequestPlaybackRangeFormats(t *testing.T) {
	for _, tc := range []struct {
		value string
		start float64
	}{
		{"npt=12.5-30", 12.5}, {"npt=00:01:02.5-00:02:00", 62.5},
		{"smpte=0:00:12:15-0:00:30:00", 12.5}, {"smpte=00:01:02-", 62},
	} {
		t.Run(tc.value, func(t *testing.T) {
			request, err := ParseRequest([]byte("PLAY RTSP/1.0\r\nCSeq: 2\r\nRange: " + tc.value + "\r\n\r\n"))
			require.NoError(t, err)
			require.Equal(t, "seek", request.Action)
			require.InDelta(t, tc.start, request.PositionSeconds, 0.000001)
		})
	}
}

func TestPlaybackRangeRejectsMalformedAndOutOfBounds(t *testing.T) {
	for _, value := range []string{
		"npt=NaN-", "npt=Inf-", "npt=-1-", "npt=1-0", "npt=1-1", "npt=1-2-3",
		"npt=1e4-", "npt=00:60:00-", "npt=00:00:60-", "npt=00:00:1-",
		"smpte=00:00:00:30-", "smpte=00:60:00-", "smpte=00:00:12:15.123-",
		"smpte=12-", "smpte=00:00:20-00:00:10", "smpte-25=00:00:00-",
		"npt=1-20\r\nInjected: value", "npt=999999999999999999999999-",
	} {
		_, err := ParseRange(value)
		require.Error(t, err, value)
	}
}

func TestBuildResponseWithRTPInfoHeader(t *testing.T) {
	body := BuildResponseWithHeaders(7, 200, "OK", RTPInfo{
		URL: "rtsp://192.0.2.20/rtp/source", Seq: 12, RTPTime: 3456,
	}.Header())
	require.Equal(t, "RTSP/1.0 200 OK\r\nCSeq: 7\r\nRTP-Info: url=rtsp://192.0.2.20/rtp/source;seq=12;rtptime=3456\r\n\r\n", string(body))
}
