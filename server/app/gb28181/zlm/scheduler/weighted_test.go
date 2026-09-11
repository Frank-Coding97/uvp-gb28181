package scheduler_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/scheduler"
)

// nodeSpec 测试用,描述一个待添加节点的关键字段
type nodeSpec struct {
	state  node.State
	weight int
}

// buildRegistryWeighted 按 nodeSpec 列表构造 Registry,节点 ID 顺序为 1..N
func buildRegistryWeighted(t *testing.T, specs ...nodeSpec) *node.Registry {
	t.Helper()
	reg := node.NewRegistry(newMemoryRepo())
	for i, sp := range specs {
		_, err := reg.Add(context.Background(), node.Node{
			Name:            string(rune('a' + i)),
			MediaServerUUID: "uuid-w-" + string(rune('a'+i)),
			State:           sp.state,
			Weight:          sp.weight,
		})
		require.NoError(t, err)
	}
	return reg
}

// ---------- Weighted ----------

func TestWeightedRoundRobin_Name(t *testing.T) {
	reg := buildRegistryWeighted(t, nodeSpec{node.StateActive, 50})
	w := scheduler.NewWeighted(reg)
	require.Equal(t, "weighted", w.Name())
}

func TestWeightedRoundRobin_WeightDistribution(t *testing.T) {
	// 3 节点 weight=50/30/20,Pick 1000 次,counts 大约 500/300/200(±15 容忍)
	reg := buildRegistryWeighted(t,
		nodeSpec{node.StateActive, 50},
		nodeSpec{node.StateActive, 30},
		nodeSpec{node.StateActive, 20},
	)
	w := scheduler.NewWeighted(reg)

	counts := map[int64]int{}
	for i := 0; i < 1000; i++ {
		n, err := w.Pick(context.Background(), scheduler.InviteContext{})
		require.NoError(t, err)
		require.NotNil(t, n)
		counts[n.ID]++
	}

	require.Len(t, counts, 3, "应命中全部 3 节点")
	// ID 升序:1=50, 2=30, 3=20
	require.InDeltaf(t, 500, counts[1], 15, "节点 1 (weight=50) 实际 %d", counts[1])
	require.InDeltaf(t, 300, counts[2], 15, "节点 2 (weight=30) 实际 %d", counts[2])
	require.InDeltaf(t, 200, counts[3], 15, "节点 3 (weight=20) 实际 %d", counts[3])
}

func TestWeightedRoundRobin_ZeroWeight_Skipped(t *testing.T) {
	// 某节点 weight=0,Pick 100 次都不应该选它
	reg := buildRegistryWeighted(t,
		nodeSpec{node.StateActive, 0},
		nodeSpec{node.StateActive, 50},
		nodeSpec{node.StateActive, 50},
	)
	w := scheduler.NewWeighted(reg)

	for i := 0; i < 100; i++ {
		n, err := w.Pick(context.Background(), scheduler.InviteContext{})
		require.NoError(t, err)
		require.NotNil(t, n)
		require.NotEqualf(t, int64(1), n.ID, "weight=0 节点不应被选中,第 %d 次", i)
	}
}

func TestWeightedRoundRobin_AllSameWeight_DegradeToRR(t *testing.T) {
	// 3 节点 weight 都 50,Pick 300 次大致 100/100/100(±3 容忍,平滑加权)
	reg := buildRegistryWeighted(t,
		nodeSpec{node.StateActive, 50},
		nodeSpec{node.StateActive, 50},
		nodeSpec{node.StateActive, 50},
	)
	w := scheduler.NewWeighted(reg)

	counts := map[int64]int{}
	for i := 0; i < 300; i++ {
		n, err := w.Pick(context.Background(), scheduler.InviteContext{})
		require.NoError(t, err)
		counts[n.ID]++
	}
	require.Len(t, counts, 3)
	for id, c := range counts {
		require.InDeltaf(t, 100, c, 3, "节点 %d 实际 %d", id, c)
	}
}

func TestWeightedRoundRobin_NoActiveNodes_ReturnsError(t *testing.T) {
	reg := buildRegistryWeighted(t,
		nodeSpec{node.StateOffline, 50},
		nodeSpec{node.StateOffline, 50},
	)
	w := scheduler.NewWeighted(reg)

	n, err := w.Pick(context.Background(), scheduler.InviteContext{})
	require.Nil(t, n)
	require.ErrorIs(t, err, scheduler.ErrNoActiveNode)
}

func TestWeightedRoundRobin_SkipsOfflineAndMaintenance(t *testing.T) {
	// 1 active + 1 maint + 1 offline,Pick 100 次全到 active
	reg := buildRegistryWeighted(t,
		nodeSpec{node.StateActive, 50},
		nodeSpec{node.StateMaintenance, 50},
		nodeSpec{node.StateOffline, 50},
	)
	w := scheduler.NewWeighted(reg)

	for i := 0; i < 100; i++ {
		n, err := w.Pick(context.Background(), scheduler.InviteContext{})
		require.NoError(t, err)
		require.NotNil(t, n)
		require.Equal(t, node.StateActive, n.State)
	}
}

func TestWeightedRoundRobin_Concurrent(t *testing.T) {
	reg := buildRegistryWeighted(t,
		nodeSpec{node.StateActive, 50},
		nodeSpec{node.StateActive, 30},
		nodeSpec{node.StateActive, 20},
	)
	w := scheduler.NewWeighted(reg)

	var wg sync.WaitGroup
	var ok atomic.Int64
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if _, err := w.Pick(context.Background(), scheduler.InviteContext{}); err == nil {
					ok.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int64(1000), ok.Load())
}

func TestWeightedRoundRobin_AllZeroWeight_ReturnsError(t *testing.T) {
	// 所有 active 节点 weight=0,返 ErrNoActiveNode(没人可选)
	reg := buildRegistryWeighted(t,
		nodeSpec{node.StateActive, 0},
		nodeSpec{node.StateActive, 0},
	)
	w := scheduler.NewWeighted(reg)

	n, err := w.Pick(context.Background(), scheduler.InviteContext{})
	require.Nil(t, n)
	require.ErrorIs(t, err, scheduler.ErrNoActiveNode)
}
