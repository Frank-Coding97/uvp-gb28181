package sdp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
)

func TestParseCascadeVideoOffer(t *testing.T) {
	body := []byte("v=0\r\n" +
		"o=34020000002000000001 0 0 IN IP4 192.0.2.30\r\n" +
		"s=Play\r\n" +
		"c=IN IP4 192.0.2.30\r\n" +
		"t=0 0\r\n" +
		"m=video 30000 RTP/AVP 96\r\n" +
		"a=recvonly\r\n" +
		"a=rtpmap:96 PS/90000\r\n" +
		"y=0200000001\r\n")

	offer, err := ParseCascadeVideoOffer(body)
	require.NoError(t, err)
	require.Equal(t, CascadeVideoOffer{
		RemoteIP: "192.0.2.30", RemotePort: 30000, SSRC: "0200000001",
		PayloadType: 96, Transport: zlm.GBSendRTPUDP,
	}, offer)
}

func TestParseCascadeVideoOfferAcceptsPSFormatWithoutRTPMap(t *testing.T) {
	body := []byte("v=0\r\n" +
		"o=34020000002000000001 0 0 IN IP4 192.0.2.30\r\n" +
		"s=Play\r\n" +
		"c=IN IP4 192.0.2.30\r\n" +
		"t=0 0\r\n" +
		"m=video 30000 RTP/AVP 96\r\n" +
		"a=recvonly\r\n" +
		"y=0200000001\r\n" +
		"f=v/PS/1920x1080/25/CBR/4096a////\r\n")

	offer, err := ParseCascadeVideoOffer(body)
	require.NoError(t, err)
	require.Equal(t, 96, offer.PayloadType)
}

func TestParseCascadeVideoOfferTCPRoleMappingAndDirections(t *testing.T) {
	for _, test := range []struct {
		name      string
		setup     string
		direction string
		transport zlm.GBSendRTPTransport
	}{
		{name: "upstream active", setup: "active", direction: "recvonly", transport: zlm.GBSendRTPTCPPassive},
		{name: "upstream passive", setup: "passive", direction: "sendrecv", transport: zlm.GBSendRTPTCPActive},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := []byte("v=0\n" +
				"o=34020000002000000001 0 0 IN IP4 192.0.2.30\n" +
				"s=Play\n" +
				"c=IN IP4 192.0.2.30\n" +
				"t=0 0\n" +
				"m=video 30000 TCP/RTP/AVP 126\n" +
				"a=setup:" + test.setup + "\n" +
				"a=connection:new\n" +
				"a=" + test.direction + "\n" +
				"a=rtpmap:126 PS/90000\n" +
				"y=0200000002\n" +
				"f=v/PS/1920x1080/25/CBR/4096a////\n")

			offer, err := ParseCascadeVideoOffer(body)
			require.NoError(t, err)
			require.Equal(t, 126, offer.PayloadType)
			require.Equal(t, test.transport, offer.Transport)
		})
	}
}

func TestBuildCascadeVideoAnswer(t *testing.T) {
	answer, err := BuildCascadeVideoAnswer(CascadeVideoOffer{
		RemoteIP: "192.0.2.30", RemotePort: 30000, SSRC: "0200000001",
		PayloadType: 96, Transport: zlm.GBSendRTPUDP,
	}, "34020000001320000001", "192.0.2.10", 31000)
	require.NoError(t, err)
	require.Equal(t, "v=0\r\n"+
		"o=34020000001320000001 0 0 IN IP4 192.0.2.10\r\n"+
		"s=Play\r\n"+
		"c=IN IP4 192.0.2.10\r\n"+
		"t=0 0\r\n"+
		"m=video 31000 RTP/AVP 96\r\n"+
		"a=sendonly\r\n"+
		"a=rtpmap:96 PS/90000\r\n"+
		"y=0200000001\r\n", string(answer))
}

func TestBuildCascadeVideoAnswerTCPRoleMapping(t *testing.T) {
	for _, test := range []struct {
		name      string
		transport zlm.GBSendRTPTransport
		setup     string
	}{
		{name: "passive sender", transport: zlm.GBSendRTPTCPPassive, setup: "passive"},
		{name: "active sender", transport: zlm.GBSendRTPTCPActive, setup: "active"},
	} {
		t.Run(test.name, func(t *testing.T) {
			answer, err := BuildCascadeVideoAnswer(CascadeVideoOffer{
				RemoteIP: "192.0.2.30", RemotePort: 30000, SSRC: "0200000001",
				PayloadType: 126, Transport: test.transport,
			}, "34020000001320000001", "192.0.2.10", 31000)
			require.NoError(t, err)
			text := string(answer)
			require.Contains(t, text, "m=video 31000 TCP/RTP/AVP 126\r\n")
			require.Contains(t, text, "a=setup:"+test.setup+"\r\n")
			require.Contains(t, text, "a=connection:new\r\n")
			require.Contains(t, text, "a=sendonly\r\n")
			require.Contains(t, text, "a=rtpmap:126 PS/90000\r\n")
		})
	}
}

func TestParseCascadeVideoOfferRejectsUnsupportedOrMalformedOffers(t *testing.T) {
	valid := "v=0\r\n" +
		"o=upstream 0 0 IN IP4 192.0.2.30\r\n" +
		"s=Play\r\n" +
		"c=IN IP4 192.0.2.30\r\n" +
		"t=0 0\r\n" +
		"m=video 30000 RTP/AVP 96\r\n" +
		"a=recvonly\r\n" +
		"a=rtpmap:96 PS/90000\r\n" +
		"y=0200000001\r\n"

	for _, test := range []struct {
		name string
		body string
	}{
		{name: "audio only", body: strings.Replace(valid, "m=video 30000 RTP/AVP 96", "m=audio 30000 RTP/AVP 96", 1)},
		{name: "playback", body: strings.Replace(valid, "s=Play", "s=Playback", 1)},
		{name: "invalid address", body: strings.Replace(valid, "192.0.2.30", "not-an-ip", 2)},
		{name: "zero port", body: strings.Replace(valid, "video 30000", "video 0", 1)},
		{name: "out of range port", body: strings.Replace(valid, "video 30000", "video 65536", 1)},
		{name: "missing SSRC", body: strings.Replace(valid, "y=0200000001\r\n", "", 1)},
		{name: "non PS payload", body: strings.Replace(valid, "PS/90000", "H264/90000", 1)},
		{name: "static payload", body: strings.Replace(valid, "96", "32", 1)},
		{name: "sendonly offer", body: strings.Replace(valid, "a=recvonly", "a=sendonly", 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseCascadeVideoOffer([]byte(test.body))
			require.Error(t, err)
		})
	}
}

func TestBuildCascadeVideoAnswerRejectsInvalidInput(t *testing.T) {
	valid := CascadeVideoOffer{
		RemoteIP: "192.0.2.30", RemotePort: 30000, SSRC: "0200000001",
		PayloadType: 96, Transport: zlm.GBSendRTPUDP,
	}

	for _, test := range []struct {
		name  string
		offer CascadeVideoOffer
		id    string
		ip    string
		port  int
	}{
		{name: "empty local id", offer: valid, id: "", ip: "192.0.2.10", port: 31000},
		{name: "invalid local address", offer: valid, id: "local", ip: "not-an-ip", port: 31000},
		{name: "zero local port", offer: valid, id: "local", ip: "192.0.2.10", port: 0},
		{name: "invalid payload", offer: CascadeVideoOffer{SSRC: "0200000001", PayloadType: 128, Transport: zlm.GBSendRTPUDP}, id: "local", ip: "192.0.2.10", port: 31000},
		{name: "missing SSRC", offer: CascadeVideoOffer{PayloadType: 96, Transport: zlm.GBSendRTPUDP}, id: "local", ip: "192.0.2.10", port: 31000},
		{name: "unknown transport", offer: CascadeVideoOffer{SSRC: "0200000001", PayloadType: 96, Transport: "invalid"}, id: "local", ip: "192.0.2.10", port: 31000},
	} {
		t.Run(test.name, func(t *testing.T) {
			answer, err := BuildCascadeVideoAnswer(test.offer, test.id, test.ip, test.port)
			require.Error(t, err)
			require.Empty(t, answer)
		})
	}
}
