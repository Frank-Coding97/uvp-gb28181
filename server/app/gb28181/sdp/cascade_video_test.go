package sdp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/zlm"
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

func TestParseCascadeVideoOfferIgnoresMediaAttributesBeforeFirstMedia(t *testing.T) {
	body := []byte("v=0\r\n" +
		"o=34020000002000000001 0 0 IN IP4 192.0.2.30\r\n" +
		"s=Play\r\n" +
		"c=IN IP4 192.0.2.30\r\n" +
		"t=0 0\r\n" +
		"a=recvonly\r\n" +
		"f=v/PS/1920x1080/25/CBR/4096a////\r\n" +
		"a=rtpmap:96 PS/90000\r\n" +
		"a=setup:active\r\n" +
		"m=video 30000 RTP/AVP 96\r\n" +
		"y=0200000001\r\n")

	offer, err := ParseCascadeVideoOffer(body)
	require.NoError(t, err)
	require.Equal(t, "192.0.2.30", offer.RemoteIP)
	require.Equal(t, 30000, offer.RemotePort)
	require.Equal(t, 96, offer.PayloadType)
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

func TestCascadePlaybackOfferPreservesUnixTimeRange(t *testing.T) {
	body := []byte("v=0\r\no=upper 0 0 IN IP4 192.0.2.30\r\ns=Playback\r\nc=IN IP4 192.0.2.30\r\n" +
		"t=1785632400 1785636000\r\nm=video 30000 RTP/AVP 96\r\na=recvonly\r\na=rtpmap:96 PS/90000\r\ny=1200000001\r\n")
	offer, err := ParseCascadeVideoOffer(body)
	require.NoError(t, err)
	require.True(t, offer.Playback)
	require.EqualValues(t, 1785632400, offer.Start.Unix())
	require.EqualValues(t, 1785636000, offer.End.Unix())
	answer, err := BuildCascadeVideoAnswer(offer, "34020000002000000002", "192.0.2.10", 31000)
	require.NoError(t, err)
	require.Contains(t, string(answer), "s=Playback\r\n")
	require.Contains(t, string(answer), "t=1785632400 1785636000\r\n")
	for _, replacement := range []string{"t=0 0", "t=1785632400 1785632400", "t=invalid 1785636000"} {
		_, err = ParseCascadeVideoOffer([]byte(strings.Replace(string(body), "t=1785632400 1785636000", replacement, 1)))
		require.Error(t, err)
	}
}

func TestCascadeDownloadOfferAndAnswerPreserveRangeAndSpeed(t *testing.T) {
	body := "v=0\r\no=upper 0 0 IN IP4 192.0.2.30\r\ns=Download\r\nu=34020000001320000001:0\r\nc=IN IP4 192.0.2.30\r\n" +
		"t=1785632400 1785636000\r\nm=video 30000 RTP/AVP 96\r\na=recvonly\r\na=rtpmap:96 PS/90000\r\na=downloadspeed:2\r\ny=1200000001\r\n"
	offer, err := ParseCascadeVideoOffer([]byte(body))
	require.NoError(t, err)
	require.True(t, offer.Download)
	require.False(t, offer.Playback)
	require.Equal(t, uint32(2), offer.DownloadSpeed)
	require.EqualValues(t, 1785632400, offer.Start.Unix())
	require.EqualValues(t, 1785636000, offer.End.Unix())
	answer, err := BuildCascadeVideoAnswer(offer, "34020000002000000002", "192.0.2.10", 31000)
	require.NoError(t, err)
	require.Contains(t, string(answer), "s=Download\r\n")
	require.Contains(t, string(answer), "t=1785632400 1785636000\r\n")
	require.Contains(t, string(answer), "a=downloadspeed:2\r\n")
	for _, speed := range []string{"0", "-1", "4294967296", "bad", "2\r\na=downloadspeed:4"} {
		_, err := ParseCascadeVideoOffer([]byte(strings.Replace(body, "a=downloadspeed:2", "a=downloadspeed:"+speed, 1)))
		require.Error(t, err, speed)
	}
	_, err = ParseCascadeVideoOffer([]byte(strings.Replace(body, "y=1200000001", "y=0200000001", 1)))
	require.Error(t, err)
	defaultOffer, err := ParseCascadeVideoOffer([]byte(strings.Replace(body, "a=downloadspeed:2\r\n", "", 1)))
	require.NoError(t, err)
	require.Equal(t, uint32(1), defaultOffer.DownloadSpeed)
	integerOffer, err := ParseCascadeVideoOffer([]byte(strings.Replace(body, "a=downloadspeed:2", "a=downloadspeed:3", 1)))
	require.NoError(t, err)
	require.Equal(t, uint32(3), integerOffer.DownloadSpeed)
}

func TestCascadeDownloadRecordingURI(t *testing.T) {
	body := "v=0\r\ns=Download\r\nu=34020000001320000001:2\r\nc=IN IP4 192.0.2.30\r\n" +
		"t=1785632400 1785636000\r\nm=video 30000 RTP/AVP 96\r\na=recvonly\r\na=rtpmap:96 PS/90000\r\ny=1200000001\r\n"
	for _, uri := range []string{"34020000001320000001:0", "34020000001320000001:1", "34020000001320000001:2", "34020000001320000001:3", "http://34020000001320000001/records/one.ps"} {
		offer, err := ParseCascadeVideoOffer([]byte(strings.Replace(body, "34020000001320000001:2", uri, 1)))
		require.NoError(t, err, uri)
		answer, err := BuildCascadeVideoAnswer(offer, "34020000002000000002", "192.0.2.10", 31000)
		require.NoError(t, err)
		require.Contains(t, string(answer), "u="+uri+"\r\n")
	}
	for _, uri := range []string{"", "short:0", "34020000001320000001:4", "34020000001320000001:-1", "34020000001320000001:1:2", "http://example.com/file.ps", "http://34020000001320000001/", "http://34020000001320000001/../file.ps", "http://34020000001320000001/%0Afile.ps", "34020000001320000001:0\r\nu=34020000001320000001:2"} {
		_, err := ParseCascadeVideoOffer([]byte(strings.Replace(body, "34020000001320000001:2", uri, 1)))
		require.Error(t, err, uri)
	}
	_, err := ParseCascadeVideoOffer([]byte(strings.Replace(body, "u=34020000001320000001:2\r\n", "", 1)))
	require.Error(t, err, "download must identify recording source")
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
