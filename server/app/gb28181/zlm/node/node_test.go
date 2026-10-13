package node_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
)

func TestState_Constants(t *testing.T) {
	require.Equal(t, node.State("active"), node.StateActive)
	require.Equal(t, node.State("maintenance"), node.StateMaintenance)
	require.Equal(t, node.State("offline"), node.StateOffline)
}

func TestNode_HTTPEndpoint(t *testing.T) {
	n := node.Node{Host: "1.2.3.4", APIPort: 18080}
	require.Equal(t, "http://1.2.3.4:18080/index/api", n.HTTPEndpoint())
}

func TestNode_EffectiveMediaHostsFallbackAndOverride(t *testing.T) {
	n := node.Node{Host: "10.0.0.2", ReceiveHost: "  ", PlaybackHost: "play.example.com", PlatformPlaybackHost: "platform.example.com"}
	require.Equal(t, "10.0.0.2", n.EffectiveReceiveHost())
	require.Equal(t, "play.example.com", n.EffectivePlaybackHost())

	n.PlaybackHost = ""
	require.Equal(t, "platform.example.com", n.EffectivePlaybackHost())

	n.PlatformPlaybackHost = ""
	require.Equal(t, "10.0.0.2", n.EffectivePlaybackHost())

	n.ReceiveHost = "203.0.113.10"
	require.Equal(t, "203.0.113.10", n.EffectiveReceiveHost())
}

func TestNode_EffectivePlaybackHostSkipsLoopbackOverrideWhenPlatformHostExists(t *testing.T) {
	n := node.Node{Host: "192.168.10.220", PlaybackHost: "127.0.0.1", PlatformPlaybackHost: "192.168.10.220"}
	require.Equal(t, "192.168.10.220", n.EffectivePlaybackHost())

	n.PlaybackHost = "localhost"
	require.Equal(t, "192.168.10.220", n.EffectivePlaybackHost())

	n.PlatformPlaybackHost = ""
	require.Equal(t, "192.168.10.220", n.EffectivePlaybackHost())
}

// The SDP resolution order is the contract that keeps "signalling succeeds but
// no picture" from happening: node sdp_ip → node receive_host → platform sdp_ip
// → platform stream_ip. It must NEVER fall back to Host, because a single-host
// deployment has Host == 127.0.0.1.
func TestNode_EffectiveSDPIPResolutionOrder(t *testing.T) {
	n := node.Node{Host: "127.0.0.1", PlatformSDPIP: "203.0.113.30", PlatformPlaybackHost: "198.51.100.9"}
	require.Equal(t, "203.0.113.30", n.EffectiveSDPIP(), "platform sdp_ip wins over playback host")
	require.True(t, n.HasUsableSDPIP())

	n.SDPIP = "192.0.2.77"
	require.Equal(t, "192.0.2.77", n.EffectiveSDPIP(), "node sdp_ip is authoritative")

	n.SDPIP = ""
	n.ReceiveHost = "192.0.2.88"
	require.Equal(t, "192.0.2.88", n.EffectiveSDPIP(), "legacy receive_host still honoured")

	n.ReceiveHost = "  "
	require.Equal(t, "203.0.113.30", n.EffectiveSDPIP(), "blank receive_host must fall through")

	n.PlatformSDPIP = ""
	require.Equal(t, "198.51.100.9", n.EffectiveSDPIP(), "platform stream_ip is the last resort")
}

// 节点播放地址是运维显式填的对外可达地址，可以兜底（wvp 同样用 ip 兜底 sdpIp）；
// 但 ZLM API host 是系统推导的，绝不能当答案。
func TestNode_EffectiveSDPIPFallsBackToNodePlaybackHostOnly(t *testing.T) {
	n := node.Node{Host: "127.0.0.1", PlaybackHost: "192.168.10.222"}
	require.Equal(t, "192.168.10.222", n.EffectiveSDPIP())

	// 只有 API host 时必须交白卷，让调用方显式失败。
	bare := node.Node{Host: "192.168.10.222"}
	require.Empty(t, bare.EffectiveSDPIP())
	require.False(t, bare.HasUsableSDPIP())
}

// With nothing configured there is no safe answer: EffectiveSDPIP returns empty
// and the caller must fail loudly instead of emitting c=IN IP4 127.0.0.1.
func TestNode_EffectiveSDPIPNeverFallsBackToAPIHost(t *testing.T) {
	n := node.Node{Host: "127.0.0.1"}
	require.Empty(t, n.EffectiveSDPIP())
	require.False(t, n.HasUsableSDPIP())

	n = node.Node{Host: "10.0.0.5"}
	require.Empty(t, n.EffectiveSDPIP(), "a routable API host is still not a device-reachable SDP address")
	require.False(t, n.HasUsableSDPIP())
}

func TestNode_IsActive(t *testing.T) {
	cases := []struct {
		state  node.State
		active bool
	}{
		{node.StateActive, true},
		{node.StateMaintenance, false},
		{node.StateOffline, false},
	}
	for _, c := range cases {
		n := node.Node{State: c.state}
		require.Equal(t, c.active, n.IsActive(), "state=%s", c.state)
	}
}

func TestNode_SchedulableSeparatesOperatorIntentFromHealth(t *testing.T) {
	require.True(t, (node.Node{State: node.StateActive}).IsSchedulable())
	require.False(t, (node.Node{State: node.StateActive, AdminState: "disabled"}).IsSchedulable())
	require.False(t, (node.Node{State: node.StateOffline}).IsSchedulable())
	require.False(t, (node.Node{State: node.StateMaintenance}).IsEnabled(), "legacy maintenance rows stay disabled")
}
