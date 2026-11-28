package sdp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAnswerMediaTargetReadsVideoPort(t *testing.T) {
	body := []byte("v=0\r\no=34020000001320000002 0 0 IN IP4 192.168.10.203\r\ns=Play\r\n" +
		"c=IN IP4 192.168.10.203\r\nt=0 0\r\n" +
		"m=video 30000 TCP/RTP/AVP 96 97 98 99\r\na=recvonly\r\na=setup:passive\r\ny=0200000001\r\n")
	target, ok := ParseAnswerMediaTarget(body)
	require.True(t, ok)
	require.Equal(t, 30000, target.Port)
	require.Equal(t, "192.168.10.203", target.IP)
	require.Equal(t, "passive", target.Setup)
	require.Equal(t, "TCP/RTP/AVP", target.Transport)
	require.False(t, target.WantsConnectionFromPeer())
}

// 媒体级 c= 覆盖会话级 c=:多网口设备常在媒体段单独声明收流地址。
func TestParseAnswerMediaTargetPrefersMediaLevelAddress(t *testing.T) {
	body := []byte("v=0\r\ns=Play\r\nc=IN IP4 10.0.0.1\r\n" +
		"m=video 30000 TCP/RTP/AVP 96\r\nc=IN IP4 10.0.0.9\r\na=setup:active\r\n")
	target, ok := ParseAnswerMediaTarget(body)
	require.True(t, ok)
	require.Equal(t, "10.0.0.9", target.IP)
	require.True(t, target.WantsConnectionFromPeer())
}

// 音频段排在视频段前面时,不能被音频的 m= 和 c= 带偏。
func TestParseAnswerMediaTargetIgnoresAudioSection(t *testing.T) {
	body := []byte("v=0\r\ns=Play\r\nc=IN IP4 10.0.0.1\r\n" +
		"m=audio 40000 RTP/AVP 8\r\nc=IN IP4 10.0.0.7\r\n" +
		"m=video 30000 TCP/RTP/AVP 96\r\n")
	target, ok := ParseAnswerMediaTarget(body)
	require.True(t, ok)
	require.Equal(t, 30000, target.Port)
	require.Equal(t, "10.0.0.1", target.IP)
}

// 0.0.0.0 是「我在这里监听」的占位,不能拿去回连;置空交给调用方退回信令地址。
func TestParseAnswerMediaTargetDropsUnspecifiedAddress(t *testing.T) {
	body := []byte("v=0\r\ns=Play\r\nc=IN IP4 0.0.0.0\r\nm=video 30000 TCP/RTP/AVP 96\r\n")
	target, ok := ParseAnswerMediaTarget(body)
	require.True(t, ok)
	require.Equal(t, 30000, target.Port)
	require.Empty(t, target.IP)
}

// 端口 0 在 SDP 语义里是「本段禁用」,不能当成可回连目标。
func TestParseAnswerMediaTargetSkipsDisabledMedia(t *testing.T) {
	body := []byte("v=0\r\ns=Play\r\nc=IN IP4 10.0.0.1\r\nm=video 0 TCP/RTP/AVP 96\r\n")
	_, ok := ParseAnswerMediaTarget(body)
	require.False(t, ok)
}

func TestParseAnswerMediaTargetReportsMissingVideo(t *testing.T) {
	_, ok := ParseAnswerMediaTarget([]byte("v=0\r\ns=Play\r\nt=0 0\r\n"))
	require.False(t, ok)
	_, ok = ParseAnswerMediaTarget(nil)
	require.False(t, ok)
}
