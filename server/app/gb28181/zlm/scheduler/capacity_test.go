package scheduler_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/scheduler"
)

// addNode 在 Registry 加一个节点(scheduler_test.go buildRegistry 只接 states,
// 容量测试需要 Stats / RTPPort 字段,本地 helper 满足)
func addNode(t *testing.T, reg *node.Registry, n node.Node) {
	t.Helper()
	_, err := reg.Add(context.Background(), n)
	require.NoError(t, err)
}

// T3.4: 容量预警过滤测试 — 3 个算法都用 ListSchedulable,所以一处生效全处生效。
// 这里用 RoundRobin 代表 3 算法验证。

func TestScheduler_T34_SkipsNode_PortUsageOver80(t *testing.T) {
	reg := node.NewRegistry(newMemoryRepo())
	// N1: 端口 1000 个,用 900(90%) → 应跳过
	addNode(t, reg, node.Node{Name: "n1", MediaServerUUID: "u1", State: node.StateActive,
		RTPPortStart: 30000, RTPPortEnd: 31000, Stats: node.Stats{MediaSourceCount: 900}})
	// N2: 端口 5000,用 100(2%) → 正常
	addNode(t, reg, node.Node{Name: "n2", MediaServerUUID: "u2", State: node.StateActive,
		RTPPortStart: 30000, RTPPortEnd: 35000, Stats: node.Stats{MediaSourceCount: 100}})

	rr := scheduler.NewRoundRobin(reg)
	for i := 0; i < 100; i++ {
		n, err := rr.Pick(context.Background(), scheduler.InviteContext{})
		require.NoError(t, err)
		require.Equal(t, "n2", n.Name, "iteration %d should hit n2 (n1 near capacity)", i)
	}
}

func TestScheduler_T34_SkipsNode_CPUOver80(t *testing.T) {
	reg := node.NewRegistry(newMemoryRepo())
	// N1: CPU 0.9*0.6+0.7*0.4=0.82 → 跳过
	addNode(t, reg, node.Node{Name: "n1", MediaServerUUID: "u1", State: node.StateActive,
		RTPPortStart: 30000, RTPPortEnd: 35000,
		Stats: node.Stats{NetThreadLoadAvg: 0.9, WorkThreadLoadAvg: 0.7}})
	// N2: CPU 0.3*0.6+0.2*0.4=0.26 → 正常
	addNode(t, reg, node.Node{Name: "n2", MediaServerUUID: "u2", State: node.StateActive,
		RTPPortStart: 30000, RTPPortEnd: 35000,
		Stats: node.Stats{NetThreadLoadAvg: 0.3, WorkThreadLoadAvg: 0.2}})

	rr := scheduler.NewRoundRobin(reg)
	for i := 0; i < 100; i++ {
		n, err := rr.Pick(context.Background(), scheduler.InviteContext{})
		require.NoError(t, err)
		require.Equal(t, "n2", n.Name, "iteration %d should hit n2", i)
	}
}

func TestScheduler_T34_AllNearCapacity_ReturnsError(t *testing.T) {
	reg := node.NewRegistry(newMemoryRepo())
	addNode(t, reg, node.Node{Name: "n1", MediaServerUUID: "u1", State: node.StateActive,
		RTPPortStart: 30000, RTPPortEnd: 31000, Stats: node.Stats{MediaSourceCount: 900}})

	rr := scheduler.NewRoundRobin(reg)
	_, err := rr.Pick(context.Background(), scheduler.InviteContext{})
	require.ErrorIs(t, err, scheduler.ErrNoActiveNode)
}

// 顺手对 weighted / leastload 各跑一次,确保 3 算法都接到了
func TestScheduler_T34_Weighted_SkipsNearCapacity(t *testing.T) {
	reg := node.NewRegistry(newMemoryRepo())
	addNode(t, reg, node.Node{Name: "n1", MediaServerUUID: "u1", State: node.StateActive, Weight: 50,
		RTPPortStart: 30000, RTPPortEnd: 31000, Stats: node.Stats{MediaSourceCount: 900}}) // 跳过
	addNode(t, reg, node.Node{Name: "n2", MediaServerUUID: "u2", State: node.StateActive, Weight: 50,
		RTPPortStart: 30000, RTPPortEnd: 35000, Stats: node.Stats{MediaSourceCount: 50}})

	w := scheduler.NewWeighted(reg)
	for i := 0; i < 50; i++ {
		n, _ := w.Pick(context.Background(), scheduler.InviteContext{})
		require.Equal(t, "n2", n.Name)
	}
}

func TestScheduler_T34_LeastLoad_SkipsNearCapacity(t *testing.T) {
	reg := node.NewRegistry(newMemoryRepo())
	// N1: 端口接近容量但 CPU 低
	addNode(t, reg, node.Node{Name: "n1", MediaServerUUID: "u1", State: node.StateActive,
		RTPPortStart: 30000, RTPPortEnd: 31000,
		Stats: node.Stats{MediaSourceCount: 900, NetThreadLoadAvg: 0.1, WorkThreadLoadAvg: 0.1}}) // 跳过
	addNode(t, reg, node.Node{Name: "n2", MediaServerUUID: "u2", State: node.StateActive,
		RTPPortStart: 30000, RTPPortEnd: 35000,
		Stats: node.Stats{MediaSourceCount: 50, NetThreadLoadAvg: 0.3, WorkThreadLoadAvg: 0.3}}) // CPU 0.3 但仍最低

	l := scheduler.NewLeastLoad(reg)
	for i := 0; i < 50; i++ {
		n, _ := l.Pick(context.Background(), scheduler.InviteContext{})
		require.Equal(t, "n2", n.Name)
	}
}

// suppress unused
