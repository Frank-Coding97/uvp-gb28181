package sipclient

import (
	"strings"
	"testing"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
	"uvplatform.com/uvp-gb28181/app/gb28181/protocol"
)

const audioPullSDP = "v=0\r\no=34020000002000000002 0 0 IN IP4 192.0.2.20\r\ns=Play\r\nc=IN IP4 192.0.2.20\r\nt=0 0\r\nm=audio 30000 TCP/RTP/AVP 8\r\na=setup:passive\r\na=connection:new\r\na=recvonly\r\na=rtpmap:8 PCMA/8000\r\ny=0100000001\r\nf=v/////a/1/8/1\r\n"

func TestAudioPullInviteUsesPlatformIdentityAndOriginalSource(t *testing.T) {
	for _, profile := range []protocol.Version{protocol.Version2016, protocol.Version2022} {
		t.Run(string(profile), func(t *testing.T) {
			identity := Identity{UpstreamServerID: "34020000002000000001", UpstreamDomain: "3402000000", Host: "192.0.2.10", Port: 5060, LocalDeviceID: "34020000002000000002", LocalDomain: "3402000000", LocalIP: "192.0.2.20", LocalPort: 5061, Transport: "UDP", Profile: profile}
			factory := mustFactory(t, identity)
			request, err := factory.BuildAudioPull("34020000001360000001", "34020000001370000012", []byte(audioPullSDP), "audio-pull-call")
			require.NoError(t, err)
			require.Equal(t, sip.INVITE, request.Method)
			require.Equal(t, sip.INVITE, request.CSeq().MethodName)
			require.Equal(t, "34020000001360000001", request.To().Address.User)
			assertIdentity(t, request, "34020000001360000001", identity.UpstreamDomain, identity.LocalDeviceID, identity.LocalDomain, identity.LocalIP, identity.LocalPort, "UDP", "192.0.2.10:5060")
			require.Equal(t, "application/sdp", request.GetHeader("Content-Type").Value())
			require.Equal(t, "34020000001360000001:0100000001,34020000001370000012:0", request.GetHeader("Subject").Value())
			require.Equal(t, []byte(audioPullSDP), request.Body())
			if profile == protocol.Version2022 {
				require.Equal(t, "3.0", request.GetHeader("X-GB-Ver").Value())
			} else {
				require.Nil(t, request.GetHeader("X-GB-Ver"))
			}
			secondIdentity := identity
			secondIdentity.LocalDeviceID = "44010000002000000002"
			secondIdentity.Host = "198.51.100.10"
			secondIdentity.LocalIP = "198.51.100.20"
			secondIdentity.Transport = "TCP"
			second := mustFactory(t, secondIdentity)
			secondSDP := strings.ReplaceAll(audioPullSDP, identity.LocalDeviceID, secondIdentity.LocalDeviceID)
			secondSDP = strings.ReplaceAll(secondSDP, identity.LocalIP, secondIdentity.LocalIP)
			other, err := second.BuildAudioPull("34020000001360000001", "44010000001370000012", []byte(secondSDP), "other-call")
			require.NoError(t, err)
			require.Equal(t, secondIdentity.LocalDeviceID, other.From().Address.User)
			require.Equal(t, secondIdentity.LocalIP, other.Contact().Address.Host)
			require.Equal(t, "198.51.100.10:5060", other.Destination())
			require.Equal(t, "TCP", other.Transport())
			require.Equal(t, []byte(secondSDP), other.Body())
			require.Equal(t, "34020000001360000001:0100000001,44010000001370000012:0", other.GetHeader("Subject").Value())
			require.EqualValues(t, 1, other.CSeq().SeqNo)
		})
	}
}

func TestAudioPullInviteRejectsInvalidAndNonPullSDP(t *testing.T) {
	factory := mustFactory(t, Identity{UpstreamServerID: "34020000002000000001", UpstreamDomain: "3402000000", Host: "192.0.2.10", Port: 5060, LocalDeviceID: "34020000002000000002", LocalDomain: "3402000000", LocalIP: "192.0.2.20", LocalPort: 5061, Transport: "UDP", Profile: protocol.Version2022})
	for name, body := range map[string]string{
		"Talk":         strings.ReplaceAll(audioPullSDP, "s=Play", "s=Talk"),
		"sendonly":     strings.ReplaceAll(audioPullSDP, "a=recvonly", "a=sendonly"),
		"sendrecv":     strings.ReplaceAll(audioPullSDP, "a=recvonly", "a=sendrecv"),
		"missing SSRC": strings.ReplaceAll(audioPullSDP, "y=0100000001\r\n", ""),
		"mixed media":  audioPullSDP + "m=video 30002 RTP/AVP 96\r\n",
		"wrong codec":  strings.ReplaceAll(audioPullSDP, "PCMA/8000", "PCMU/8000"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := factory.BuildAudioPull("34020000001360000001", "34020000001370000012", []byte(body), "audio-call")
			require.Error(t, err)
		})
	}
	for _, test := range []struct{ source, receiver, callID string }{{"bad", "34020000001370000012", "call"}, {"34020000001360000001", "bad", "call"}, {"34020000001360000001", "34020000001370000012", "injected\r\nHeader: bad"}} {
		_, err := factory.BuildAudioPull(test.source, test.receiver, []byte(audioPullSDP), test.callID)
		require.Error(t, err)
	}
}
