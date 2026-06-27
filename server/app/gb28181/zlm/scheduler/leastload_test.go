package scheduler_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/scheduler"
)

// ---------- LeastLoad ----------
//
// 综合负载 = NetThreadLoadAvg * 0.6 + WorkThreadLoadAvg * 0.4
// 网络 I/O 是 RTP 转发主要瓶颈,权重 0.6;工作线程权重 0.4。

// buildRegistryWithStats 构造一个带指定 Stats 的 Registry。
// states / stats 长度需一致;states[i] 是节点状态,stats[i] 是该节点 Stats。
//
// 注意:这里通过 Add 直接把 Stats 写进 Node,不走 UpdateStats —— 因为
// UpdateStats 内部有"offline 收到心跳自动恢复 active"的副作用(registry.go),
// 会让测试里的 offline 节点意外转 active。
func buildRegistryWithStats(t *testing.T, states []node.State, stats []node.Stats) *node.Registry {
	t.Helper()
	require.Equal(t, len(states), len(stats), "states 和 stats 长度需一致")
	reg := node.NewRegistry(newMemoryRepo())
	for i, st := range states {
		uuid := "uuid-" + string(rune('a'+i))
		_, err := reg.Add(context.Background(), node.Node{
			Name:            string(rune('a' + i)),
			MediaServerUUID: uuid,
			State:           st,
			Stats:           stats[i],
		})
		require.NoError(t, err)
	}
	return reg
}

// TestLeastLoad_PicksLowestLoadNode 3 节点不同负载,Pick 100 次都应选综合最低的节点 2。
func TestLeastLoad_PicksLowestLoadNode(t *testing.T) {
	reg := buildRegistryWithStats(t,
		[]node.State{node.StateActive, node.StateActive, node.StateActive},
		[]node.Stats{
			{NetThreadLoadAvg: 0.8, WorkThreadLoadAvg: 0.7}, // 综合 0.76
			{NetThreadLoadAvg: 0.3, WorkThreadLoadAvg: 0.2}, // 综合 0.26 ← 选
			{NetThreadLoadAvg: 0.5, WorkThreadLoadAvg: 0.5}, // 综合 0.50
		},
	)
	ll := scheduler.NewLeastLoad(reg)
	require.Equal(t, "leastload", ll.Name())

	for i := 0; i < 100; i++ {
		n, err := ll.Pick(context.Background(), scheduler.InviteContext{})
		require.NoError(t, err)
		require.NotNil(t, n)
		require.EqualValuesf(t, 2, n.ID, "应始终选综合负载最低的 node 2,第 %d 次返 node %d", i, n.ID)
	}
}

// TestLeastLoad_TieBreaker_FallbackRR 两节点综合负载相等(都 0.5)→ 应该轮询。
// Pick 200 次后两节点 counts 大约 100/100(±5 容忍)。
func TestLeastLoad_TieBreaker_FallbackRR(t *testing.T) {
	reg := buildRegistryWithStats(t,
		[]node.State{node.StateActive, node.StateActive},
		[]node.Stats{
			{NetThreadLoadAvg: 0.5, WorkThreadLoadAvg: 0.5}, // 综合 0.5
			{NetThreadLoadAvg: 0.5, WorkThreadLoadAvg: 0.5}, // 综合 0.5
		},
	)
	ll := scheduler.NewLeastLoad(reg)

	counts := map[int64]int{}
	for i := 0; i < 200; i++ {
		n, err := ll.Pick(context.Background(), scheduler.InviteContext{})
		require.NoError(t, err)
		counts[n.ID]++
	}
	require.Len(t, counts, 2, "平局时应轮询命中两节点")
	for id, c := range counts {
		require.InDeltaf(t, 100, c, 5, "平局 RR 节点 %d 期望 100±5,实际 %d", id, c)
	}
}

// TestLeastLoad_NoStats_FallbackToRR 全部节点 Stats 零值 → 退化为轮询。
// Pick 300 次,3 节点 counts 大约 100/100/100(±5 容忍)。
func TestLeastLoad_NoStats_FallbackToRR(t *testing.T) {
	reg := buildRegistryWithStats(t,
		[]node.State{node.StateActive, node.StateActive, node.StateActive},
		[]node.Stats{{}, {}, {}}, // 全零
	)
	ll := scheduler.NewLeastLoad(reg)

	counts := map[int64]int{}
	for i := 0; i < 300; i++ {
		n, err := ll.Pick(context.Background(), scheduler.InviteContext{})
		require.NoError(t, err)
		counts[n.ID]++
	}
	require.Len(t, counts, 3, "全零 fallback RR 应命中全部 3 节点")
	for id, c := range counts {
		require.InDeltaf(t, 100, c, 5, "fallback RR 节点 %d 期望 100±5,实际 %d", id, c)
	}
}

// TestLeastLoad_NoActiveNodes_ReturnsError registry 全 offline,Pick 返 ErrNoActiveNode。
func TestLeastLoad_NoActiveNodes_ReturnsError(t *testing.T) {
	reg := buildRegistryWithStats(t,
		[]node.State{node.StateOffline, node.StateOffline},
		[]node.Stats{
			{NetThreadLoadAvg: 0.1, WorkThreadLoadAvg: 0.1},
			{NetThreadLoadAvg: 0.2, WorkThreadLoadAvg: 0.2},
		},
	)
	ll := scheduler.NewLeastLoad(reg)
	n, err := ll.Pick(context.Background(), scheduler.InviteContext{})
	require.Nil(t, n)
	require.ErrorIs(t, err, scheduler.ErrNoActiveNode)
}

// TestLeastLoad_SkipsOfflineAndMaintenance 3 节点 1 active + 1 maint + 1 offline,
// Pick 100 次全到 active 节点(ListActive 自动过滤)。
func TestLeastLoad_SkipsOfflineAndMaintenance(t *testing.T) {
	reg := buildRegistryWithStats(t,
		[]node.State{node.StateActive, node.StateMaintenance, node.StateOffline},
		[]node.Stats{
			{NetThreadLoadAvg: 0.9, WorkThreadLoadAvg: 0.9}, // active 但负载高
			{NetThreadLoadAvg: 0.1, WorkThreadLoadAvg: 0.1}, // maint 负载低也不能选
			{NetThreadLoadAvg: 0.1, WorkThreadLoadAvg: 0.1}, // offline 负载低也不能选
		},
	)
	ll := scheduler.NewLeastLoad(reg)

	for i := 0; i < 100; i++ {
		n, err := ll.Pick(context.Background(), scheduler.InviteContext{})
		require.NoError(t, err)
		require.NotNil(t, n)
		require.Equal(t, node.StateActive, n.State, "只允许命中 active 节点,实际 %s", n.State)
	}
}

// TestLeastLoad_Concurrent 并发 Pick 不崩溃,所有结果都有效。
func TestLeastLoad_Concurrent(t *testing.T) {
	reg := buildRegistryWithStats(t,
		[]node.State{node.StateActive, node.StateActive, node.StateActive},
		[]node.Stats{
			{NetThreadLoadAvg: 0.5, WorkThreadLoadAvg: 0.5},
			{NetThreadLoadAvg: 0.5, WorkThreadLoadAvg: 0.5},
			{NetThreadLoadAvg: 0.5, WorkThreadLoadAvg: 0.5},
		},
	)
	ll := scheduler.NewLeastLoad(reg)

	var wg sync.WaitGroup
	errCh := make(chan error, 1000)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if _, err := ll.Pick(context.Background(), scheduler.InviteContext{}); err != nil {
					errCh <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("并发 Pick 不应返错:%v", err)
	}
}

// TestLeastLoad_FactoryBuilds Factory.Build("leastload") 应返实际实现,而非 not-implemented。
func TestLeastLoad_FactoryBuilds(t *testing.T) {
	reg := buildRegistryWithStats(t,
		[]node.State{node.StateActive},
		[]node.Stats{{NetThreadLoadAvg: 0.1, WorkThreadLoadAvg: 0.1}},
	)
	factory := scheduler.NewFactory(reg)

	s, err := factory.Build("leastload")
	require.NoError(t, err)
	require.NotNil(t, s)
	require.Equal(t, "leastload", s.Name())
}
