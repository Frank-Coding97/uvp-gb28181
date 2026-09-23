package sdp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseBroadcastOfferTransportModes(t *testing.T) {
	tests := []struct {
		name  string
		proto string
		setup string
		want  BroadcastSenderMode
	}{
		{name: "udp", proto: "RTP/AVP", want: BroadcastSenderUDP},
		{name: "device active tcp", proto: "TCP/RTP/AVP", setup: "a=setup:active\r\n", want: BroadcastSenderTCPPassive},
		{name: "device passive tcp", proto: "TCP/RTP/AVP", setup: "a=setup:passive\r\n", want: BroadcastSenderTCPActive},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			offer := "v=0\r\no=device 0 0 IN IP4 192.0.2.20\r\ns=Play\r\nc=IN IP4 192.0.2.20\r\nt=0 0\r\nm=audio 30000 " + test.proto + " 8\r\n" + test.setup + "a=recvonly\r\na=rtpmap:8 PCMA/8000\r\ny=0100000001\r\n"
			media, err := ParseBroadcastOffer(offer)
			require.NoError(t, err)
			require.Equal(t, "192.0.2.20", media.RemoteIP)
			require.Equal(t, 30000, media.RemotePort)
			require.Equal(t, test.want, media.SenderMode)
			require.Equal(t, "0100000001", media.SSRC)
		})
	}
}

func TestParseBroadcastOfferRejectsWrongCodec(t *testing.T) {
	for _, offer := range []string{
		"v=0\r\nc=IN IP4 192.0.2.20\r\nm=video 30000 RTP/AVP 96\r\na=rtpmap:96 PS/90000\r\n",
		"v=0\r\nc=IN IP4 192.0.2.20\r\nm=audio 30000 RTP/AVP 111\r\na=rtpmap:111 opus/48000/2\r\n",
		"v=0\r\nc=IN IP4 192.0.2.20\r\nm=audio 0 RTP/AVP 8\r\na=rtpmap:8 PCMA/8000\r\n",
	} {
		_, err := ParseBroadcastOffer(offer)
		require.Error(t, err)
	}
}

func TestBuildBroadcastAnswerUsesNegotiatedMode(t *testing.T) {
	answer, err := BuildBroadcastAnswer(BroadcastAnswerParams{ServerID: "34020000002000000001", LocalIP: "192.0.2.10", LocalPort: 40000, SSRC: "0100000001", SenderMode: BroadcastSenderTCPPassive})
	require.NoError(t, err)
	require.Contains(t, answer, "m=audio 40000 TCP/RTP/AVP 8")
	require.Contains(t, answer, "a=setup:passive")
	require.Contains(t, answer, "a=sendonly")
	require.Contains(t, answer, "a=rtpmap:8 PCMA/8000")
}
