package sdp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const cascadeAudioOffer = "v=0\r\no=34020000002000000001 0 0 IN IP4 192.0.2.20\r\ns=Talk\r\nc=IN IP4 192.0.2.20\r\nt=0 0\r\nm=audio 30000 TCP/RTP/AVP 8\r\na=setup:active\r\na=connection:new\r\na=sendrecv\r\na=rtpmap:8 PCMA/8000\r\ny=0100000001\r\n"

func TestCascadeAudioOfferAndAnswer(t *testing.T) {
	for _, test := range []struct{ name, proto, setup, direction, answerDirection, answerSetup string }{
		{"duplex TCP active", "TCP/RTP/AVP", "active", "sendrecv", "sendrecv", "passive"},
		{"duplex TCP passive", "TCP/RTP/AVP", "passive", "sendrecv", "sendrecv", "active"},
		{"duplex UDP", "RTP/AVP", "", "sendrecv", "sendrecv", ""},
		{"upstream sends broadcast", "RTP/AVP", "", "sendonly", "recvonly", ""},
		{"upstream receives broadcast", "RTP/AVP", "", "recvonly", "sendonly", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := strings.ReplaceAll(cascadeAudioOffer, "TCP/RTP/AVP", test.proto)
			raw = strings.ReplaceAll(raw, "a=sendrecv", "a="+test.direction)
			if test.setup == "" {
				raw = strings.ReplaceAll(raw, "a=setup:active\r\n", "")
			} else {
				raw = strings.ReplaceAll(raw, "a=setup:active", "a=setup:"+test.setup)
			}
			offer, err := ParseCascadeAudioOffer([]byte(raw))
			require.NoError(t, err)
			require.Equal(t, "192.0.2.20", offer.RemoteIP)
			require.Equal(t, 30000, offer.RemotePort)
			if test.proto == "RTP/AVP" {
				require.Equal(t, 30001, offer.RTCPPort)
				require.Equal(t, offer.RemoteIP, offer.RTCPIP)
			}
			require.Equal(t, "0100000001", offer.SSRC)
			require.Equal(t, test.direction, offer.Direction)
			answer, err := BuildCascadeAudioAnswer(offer, "34020000002000000002", "192.0.2.10", 40000)
			require.NoError(t, err)
			require.Contains(t, string(answer), "m=audio 40000 "+test.proto+" 8\r\n")
			require.Contains(t, string(answer), "a="+test.answerDirection+"\r\n")
			require.Contains(t, string(answer), "a=rtpmap:8 PCMA/8000\r\n")
			require.Contains(t, string(answer), "y=0100000001\r\n")
			if test.answerSetup != "" {
				require.Contains(t, string(answer), "a=setup:"+test.answerSetup+"\r\n")
			} else {
				require.NotContains(t, string(answer), "a=setup:")
			}
			if test.proto == "RTP/AVP" {
				require.Contains(t, string(answer), "a=rtcp:40001 IN IP4 192.0.2.10\r\n")
			} else {
				require.NotContains(t, string(answer), "a=rtcp:")
			}
		})
	}
}

func TestCascadeAudioOfferParsesExplicitRTCPEndpoint(t *testing.T) {
	raw := strings.Replace(cascadeAudioOffer, "TCP/RTP/AVP", "RTP/AVP", 1)
	raw = strings.Replace(raw, "a=setup:active\r\na=connection:new\r\n", "", 1)
	raw = strings.Replace(raw, "m=audio 30000 RTP/AVP 8\r\n", "m=audio 30000 RTP/AVP 8\r\na=rtcp:35001 IN IP4 192.0.2.21\r\n", 1)
	offer, err := ParseCascadeAudioOffer([]byte(raw))
	require.NoError(t, err)
	require.Equal(t, 35001, offer.RTCPPort)
	require.Equal(t, "192.0.2.21", offer.RTCPIP)
	_, err = ParseCascadeAudioOffer([]byte(raw + "a=rtcp:35002\r\n"))
	require.Error(t, err)
}

func TestCascadeAudioOfferRejectsAmbiguousOrUnsupportedMedia(t *testing.T) {
	for name, body := range map[string]string{
		"duplicate audio":      cascadeAudioOffer + "m=audio 30001 RTP/AVP 8\r\na=recvonly\r\n",
		"video mixed":          cascadeAudioOffer + "m=video 30002 RTP/AVP 96\r\n",
		"application mixed":    cascadeAudioOffer + "m=application 30002 UDP/DTLS/SCTP webrtc-datachannel\r\n",
		"duplicate SSRC":       cascadeAudioOffer + "y=0100000002\r\n",
		"duplicate direction":  cascadeAudioOffer + "a=recvonly\r\n",
		"duplicate map":        cascadeAudioOffer + "a=rtpmap:8 PCMU/8000\r\n",
		"duplicate setup":      cascadeAudioOffer + "a=setup:passive\r\n",
		"duplicate connection": cascadeAudioOffer + "c=IN IP4 192.0.2.99\r\nc=IN IP4 192.0.2.98\r\n",
		"duplicate timing":     cascadeAudioOffer + "t=0 0\r\n",
		"duplicate name":       cascadeAudioOffer + "s=Play\r\n",
		"wrong codec":          strings.ReplaceAll(cascadeAudioOffer, "PCMA/8000", "PCMU/8000"),
		"stereo":               strings.ReplaceAll(cascadeAudioOffer, "PCMA/8000", "PCMA/8000/2"),
		"wrong payload":        strings.ReplaceAll(cascadeAudioOffer, "AVP 8", "AVP 96"),
		"wrong session":        strings.ReplaceAll(cascadeAudioOffer, "s=Talk", "s=Playback"),
		"inactive":             strings.ReplaceAll(cascadeAudioOffer, "a=sendrecv", "a=inactive"),
		"zero port":            strings.ReplaceAll(cascadeAudioOffer, "audio 30000", "audio 0"),
		"overflow port":        strings.ReplaceAll(cascadeAudioOffer, "audio 30000", "audio 65536"),
		"unspecified IP":       strings.ReplaceAll(cascadeAudioOffer, "192.0.2.20", "0.0.0.0"),
		"multicast IP":         strings.ReplaceAll(cascadeAudioOffer, "192.0.2.20", "239.0.0.1"),
		"missing SSRC":         strings.ReplaceAll(cascadeAudioOffer, "y=0100000001\r\n", ""),
		"nondecimal SSRC":      strings.ReplaceAll(cascadeAudioOffer, "0100000001", "010000000x"),
		"missing map":          strings.ReplaceAll(cascadeAudioOffer, "a=rtpmap:8 PCMA/8000\r\n", ""),
		"unsupported TCP role": strings.ReplaceAll(cascadeAudioOffer, "a=setup:active", "a=setup:actpass"),
		"scheduled media":      strings.ReplaceAll(cascadeAudioOffer, "t=0 0", "t=1 2"),
	} {
		t.Run(name, func(t *testing.T) { _, err := ParseCascadeAudioOffer([]byte(body)); require.Error(t, err) })
	}
}

func TestCascadeAudioOfferSessionDirectionAndMediaAddress(t *testing.T) {
	raw := strings.Replace(cascadeAudioOffer, "t=0 0\r\n", "t=0 0\r\na=sendonly\r\n", 1)
	raw = strings.Replace(raw, "a=sendrecv\r\n", "", 1)
	raw = strings.Replace(raw, "m=audio 30000 TCP/RTP/AVP 8\r\n", "m=audio 30000 TCP/RTP/AVP 8\r\nc=IN IP4 192.0.2.30\r\n", 1)
	offer, err := ParseCascadeAudioOffer([]byte(raw))
	require.NoError(t, err)
	require.Equal(t, "sendonly", offer.Direction)
	require.Equal(t, "192.0.2.30", offer.RemoteIP)
	_, err = BuildCascadeAudioAnswer(offer, "bad\r\ns=Injected", "192.0.2.10", 40000)
	require.Error(t, err)
	_, err = BuildCascadeAudioAnswer(offer, "34020000002000000002", "0.0.0.0", 40000)
	require.Error(t, err)
}

func TestCascadeAudioAnswerMustMatchOffer(t *testing.T) {
	offer, err := ParseCascadeAudioOffer([]byte(cascadeAudioOffer))
	require.NoError(t, err)
	answer, err := BuildCascadeAudioAnswer(offer, "34020000002000000002", "192.0.2.10", 40000)
	require.NoError(t, err)
	negotiated, err := ValidateCascadeAudioAnswer(offer, answer)
	require.NoError(t, err)
	require.Equal(t, "192.0.2.10", negotiated.RemoteIP)
	require.Equal(t, 40000, negotiated.RemotePort)
	for _, direction := range []string{"sendonly", "recvonly"} {
		narrowed := strings.ReplaceAll(string(answer), "a=sendrecv", "a="+direction)
		_, err := ValidateCascadeAudioAnswer(offer, []byte(narrowed))
		require.NoError(t, err, "a duplex offer may negotiate a single direction")
	}
	for name, changed := range map[string]string{
		"transport changed": strings.ReplaceAll(strings.ReplaceAll(string(answer), "TCP/RTP/AVP", "RTP/AVP"), "a=setup:passive\r\n", ""),
		"same TCP role":     strings.ReplaceAll(string(answer), "a=setup:passive", "a=setup:active"),
		"SSRC changed":      strings.ReplaceAll(string(answer), "0100000001", "0100000002"),
		"codec changed":     strings.ReplaceAll(string(answer), "PCMA/8000", "PCMU/8000"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ValidateCascadeAudioAnswer(offer, []byte(changed))
			require.Error(t, err)
		})
	}
	for _, direction := range []string{"sendonly", "recvonly"} {
		offer.Direction = direction
		answer, err := BuildCascadeAudioAnswer(offer, "34020000002000000002", "192.0.2.10", 40000)
		require.NoError(t, err)
		_, err = ValidateCascadeAudioAnswer(offer, answer)
		require.NoError(t, err)
		invalid := strings.ReplaceAll(string(answer), "a=sendonly", "a=sendrecv")
		invalid = strings.ReplaceAll(invalid, "a=recvonly", "a=sendrecv")
		_, err = ValidateCascadeAudioAnswer(offer, []byte(invalid))
		require.Error(t, err, "one-way audio cannot gain the reverse direction")
	}
}

func TestCascadeAudioPreservesSessionName(t *testing.T) {
	for _, name := range []string{"Play", "Talk"} {
		raw := strings.ReplaceAll(cascadeAudioOffer, "s=Talk", "s="+name)
		offer, err := ParseCascadeAudioOffer([]byte(raw))
		require.NoError(t, err)
		answer, err := BuildCascadeAudioAnswer(offer, "34020000002000000002", "192.0.2.10", 40000)
		require.NoError(t, err)
		require.Contains(t, string(answer), "s="+name+"\r\n")
		_, err = ValidateCascadeAudioAnswer(offer, answer)
		require.NoError(t, err)
		changedName := "Play"
		if name == "Play" {
			changedName = "Talk"
		}
		_, err = ValidateCascadeAudioAnswer(offer, []byte(strings.ReplaceAll(string(answer), "s="+name, "s="+changedName)))
		require.Error(t, err, "answer must not switch the negotiated session kind")
	}
}

func TestCascadeBroadcastPullAllowsOnlyInitialReceiverToOmitSSRC(t *testing.T) {
	raw := strings.ReplaceAll(cascadeAudioOffer, "s=Talk", "s=Play")
	raw = strings.ReplaceAll(raw, "a=sendrecv", "a=recvonly")
	raw = strings.ReplaceAll(raw, "y=0100000001\r\n", "")
	offer, err := ParseCascadeBroadcastPullOffer([]byte(raw))
	require.NoError(t, err)
	require.Empty(t, offer.SSRC)
	_, err = ParseCascadeAudioOffer([]byte(raw))
	require.Error(t, err, "generic audio offer/answer still requires SSRC")
	_, err = BuildCascadeAudioAnswer(offer, "34020000002000000002", "192.0.2.10", 40000)
	require.Error(t, err, "runtime must allocate SSRC before answering")
	offer.SSRC = "0100000002"
	answer, err := BuildCascadeAudioAnswer(offer, "34020000002000000002", "192.0.2.10", 40000)
	require.NoError(t, err)
	require.Contains(t, string(answer), "s=Play\r\n")
	require.Contains(t, string(answer), "a=sendonly\r\n")
	require.Contains(t, string(answer), "y=0100000002\r\n")
	for name, invalid := range map[string]string{
		"Talk":           strings.ReplaceAll(raw, "s=Play", "s=Talk"),
		"sendrecv":       strings.ReplaceAll(raw, "a=recvonly", "a=sendrecv"),
		"sendonly":       strings.ReplaceAll(raw, "a=recvonly", "a=sendonly"),
		"malformed SSRC": raw + "y=bad\r\n",
		"overflow SSRC":  raw + "y=9999999999\r\n",
		"zero SSRC":      raw + "y=0000000000\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseCascadeBroadcastPullOffer([]byte(invalid))
			require.Error(t, err)
		})
	}
}

func TestBuildCascadeAudioPullOfferAdvertisesPreparedReceiver(t *testing.T) {
	for _, test := range []struct{ transport, setup string }{{"RTP/AVP", ""}, {"TCP/RTP/AVP", "active"}, {"TCP/RTP/AVP", "passive"}} {
		body, err := BuildCascadeAudioPullOffer("34020000002000000002", "192.0.2.20", 40123, test.transport, test.setup, "0100000002")
		require.NoError(t, err)
		offer, err := ParseCascadeAudioOffer(body)
		require.NoError(t, err)
		require.Equal(t, "Play", offer.SessionName)
		require.Equal(t, "192.0.2.20", offer.RemoteIP)
		require.Equal(t, 40123, offer.RemotePort)
		require.Equal(t, test.transport, offer.Transport)
		if test.transport == "RTP/AVP" {
			require.Equal(t, 40124, offer.RTCPPort)
			require.Equal(t, "192.0.2.20", offer.RTCPIP)
		}
		if test.setup == "active" {
			require.Equal(t, "passive", offer.Setup)
		} else if test.setup == "passive" {
			require.Equal(t, "active", offer.Setup)
		}
		if test.setup != "" {
			require.Contains(t, string(body), "a=setup:"+test.setup+"\r\n")
		}
		require.Equal(t, "recvonly", offer.Direction)
		require.Equal(t, "0100000002", offer.SSRC)
	}
	_, err := BuildCascadeAudioPullOffer("34020000002000000002", "192.0.2.20", 40123, "RTP/AVP", "active", "0100000002")
	require.Error(t, err)
}
