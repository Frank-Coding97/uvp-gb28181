package node_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
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
	n := node.Node{Host: "10.0.0.2", ReceiveHost: "  ", PlaybackHost: "play.example.com"}
	require.Equal(t, "10.0.0.2", n.EffectiveReceiveHost())
	require.Equal(t, "play.example.com", n.EffectivePlaybackHost())

	n.ReceiveHost = "203.0.113.10"
	require.Equal(t, "203.0.113.10", n.EffectiveReceiveHost())
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
