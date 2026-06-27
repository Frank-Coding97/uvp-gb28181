package node_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

func TestNode_PortUsage(t *testing.T) {
	n := node.Node{RTPPortStart: 30000, RTPPortEnd: 31000, Stats: node.Stats{MediaSourceCount: 500}}
	require.InDelta(t, 0.5, n.PortUsage(), 0.001)
}

func TestNode_PortUsage_ZeroRange_ReturnsZero(t *testing.T) {
	n := node.Node{RTPPortStart: 30000, RTPPortEnd: 30000, Stats: node.Stats{MediaSourceCount: 5}}
	require.Equal(t, 0.0, n.PortUsage())
}

func TestNode_CPULoad_Weighted(t *testing.T) {
	n := node.Node{Stats: node.Stats{NetThreadLoadAvg: 0.5, WorkThreadLoadAvg: 0.5}}
	require.InDelta(t, 0.5, n.CPULoad(), 0.001)

	n2 := node.Node{Stats: node.Stats{NetThreadLoadAvg: 1.0, WorkThreadLoadAvg: 0.0}}
	require.InDelta(t, 0.6, n2.CPULoad(), 0.001)
}

func TestNode_IsNearCapacity_PortHigh(t *testing.T) {
	// 1001 端口,使用 850 = 84.9%
	n := node.Node{RTPPortStart: 30000, RTPPortEnd: 31000, Stats: node.Stats{MediaSourceCount: 850}}
	require.True(t, n.IsNearCapacity())
}

func TestNode_IsNearCapacity_CPUHigh(t *testing.T) {
	n := node.Node{
		RTPPortStart: 30000, RTPPortEnd: 35000,
		Stats: node.Stats{NetThreadLoadAvg: 0.9, WorkThreadLoadAvg: 0.7}, // 0.9*0.6+0.7*0.4=0.82
	}
	require.True(t, n.IsNearCapacity())
}

func TestNode_IsNearCapacity_Healthy(t *testing.T) {
	n := node.Node{
		RTPPortStart: 30000, RTPPortEnd: 35000,
		Stats: node.Stats{MediaSourceCount: 100, NetThreadLoadAvg: 0.3, WorkThreadLoadAvg: 0.2},
	}
	require.False(t, n.IsNearCapacity())
}
