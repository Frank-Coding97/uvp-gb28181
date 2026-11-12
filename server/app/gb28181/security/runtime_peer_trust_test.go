package security

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/stretchr/testify/require"
)

// cascadeInvite 是一份「上级平台级联点播」的最小 INVITE:被叫是发布出去的
// 通道编号(本机设备表里不存在),From 是上级平台编号。
func cascadeInvite() []byte {
	return []byte("INVITE sip:34020000001320000025@3402000000 SIP/2.0\r\n" +
		"From: <sip:37010000002000000001@3701000000>;tag=abc\r\n" +
		"To: <sip:34020000001320000025@3402000000>\r\n" +
		"Call-ID: uac-1\r\n" +
		"CSeq: 1 INVITE\r\n" +
		"Content-Length: 0\r\n\r\n")
}

func cascadeInviteProps() sip.TransportReadProps {
	return sip.TransportReadProps{
		Transport:  "udp",
		RemoteAddr: &net.UDPAddr{IP: net.ParseIP("203.0.113.7"), Port: 51064},
	}
}

func newPeerTrustRuntime() *Runtime {
	return NewRuntime(DefaultPolicy(), &fakeClock{now: time.Unix(100, 0)}, nil, nil)
}

// 回归:级联上级(公网 IP、From 是平台编号、被叫是发布通道)的 INVITE 曾被准入层
// 判成陌生扫描**静默丢弃** —— 因为 trusted 只认「被叫是已认证设备」。
// 实测症状:上级 6 次重传、我方零响应、只有安全事件表一行 unknown_invite_rate。
func TestAdmissionDropsCascadeInviteWithoutPeerTrust(t *testing.T) {
	runtime := newPeerTrustRuntime()

	out, err := runtime.Admission().Filter(cascadeInviteProps(), cascadeInvite())
	require.NoError(t, err)
	require.Empty(t, out)

	events := runtime.Events()
	require.Len(t, events, 1)
	require.Equal(t, ReasonInviteRate, events[0].Reason)
	require.Equal(t, "INVITE", events[0].Method)
	require.Equal(t, "203.0.113.7", events[0].SourceIP)
	require.Equal(t, int64(1), events[0].Count)
}

func TestAdmissionLetsCascadePeerInviteThrough(t *testing.T) {
	runtime := newPeerTrustRuntime()
	runtime.SetPeerTrustResolver(func(sourceIP, transport string) bool {
		return sourceIP == "203.0.113.7" && transport == "UDP"
	})

	out, err := runtime.Admission().Filter(cascadeInviteProps(), cascadeInvite())
	require.NoError(t, err)
	require.Equal(t, cascadeInvite(), out)
	require.Empty(t, runtime.Events())
}

// 放宽只针对登记过的对端地址,别的公网来源照样要丢。
func TestAdmissionStillDropsInviteFromOtherSources(t *testing.T) {
	runtime := newPeerTrustRuntime()
	runtime.SetPeerTrustResolver(func(sourceIP, transport string) bool {
		return sourceIP == "198.51.100.99"
	})

	out, err := runtime.Admission().Filter(cascadeInviteProps(), cascadeInvite())
	require.NoError(t, err)
	require.Empty(t, out)
}

// 传输层口径要参与判定:平台登记的是 TCP 时,UDP 来的 INVITE 不算可信对端。
func TestAdmissionPeerTrustHonoursTransport(t *testing.T) {
	runtime := newPeerTrustRuntime()
	runtime.SetPeerTrustResolver(func(sourceIP, transport string) bool {
		return strings.EqualFold(transport, "tcp")
	})

	out, err := runtime.Admission().Filter(cascadeInviteProps(), cascadeInvite())
	require.NoError(t, err)
	require.Empty(t, out)
}

// 没有装配判定器时不能 panic,也不能把请求放行。
func TestRuntimeWithoutPeerTrustResolverKeepsDropping(t *testing.T) {
	runtime := newPeerTrustRuntime()
	require.False(t, runtime.peerTrusted("203.0.113.7", "udp"))
}
