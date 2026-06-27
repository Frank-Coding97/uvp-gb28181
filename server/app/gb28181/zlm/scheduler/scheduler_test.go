package scheduler_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/scheduler"
)

// memoryRepo 内存版 Repo,只用来给 Registry 喂数据
type memoryRepo struct {
	mu     sync.Mutex
	nextID int64
	rows   map[int64]node.Node
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{rows: make(map[int64]node.Node)}
}

func (r *memoryRepo) List(_ context.Context) ([]node.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]node.Node, 0, len(r.rows))
	for _, n := range r.rows {
		out = append(out, n)
	}
	return out, nil
}

func (r *memoryRepo) Get(_ context.Context, id int64) (*node.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n, ok := r.rows[id]; ok {
		nc := n
		return &nc, nil
	}
	return nil, nil
}

func (r *memoryRepo) Create(_ context.Context, n node.Node) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	n.ID = r.nextID
	r.rows[n.ID] = n
	return n.ID, nil
}

func (r *memoryRepo) Update(_ context.Context, n node.Node) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows[n.ID] = n
	return nil
}

func (r *memoryRepo) Delete(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.rows, id)
	return nil
}

// buildRegistry 创建一个带 N 个节点的 Registry,states 长度 = nodeCount
func buildRegistry(t *testing.T, states ...node.State) *node.Registry {
	t.Helper()
	reg := node.NewRegistry(newMemoryRepo())
	for i, st := range states {
		_, err := reg.Add(context.Background(), node.Node{
			Name:            string(rune('a' + i)),
			MediaServerUUID: "uuid-" + string(rune('a'+i)),
			State:           st,
		})
		require.NoError(t, err)
	}
	return reg
}

// ---------- RoundRobin ----------

func TestRoundRobin_DistributesEvenly(t *testing.T) {
	reg := buildRegistry(t, node.StateActive, node.StateActive, node.StateActive)
	rr := scheduler.NewRoundRobin(reg)
	require.Equal(t, "roundrobin", rr.Name())

	counts := map[int64]int{}
	for i := 0; i < 300; i++ {
		n, err := rr.Pick(context.Background(), scheduler.InviteContext{})
		require.NoError(t, err)
		require.NotNil(t, n)
		counts[n.ID]++
	}

	require.Len(t, counts, 3, "应命中全部 3 节点")
	for id, c := range counts {
		require.InDeltaf(t, 100, c, 1, "节点 %d 分配次数应在 100±1 内,实际 %d", id, c)
	}
}

func TestRoundRobin_SkipsOfflineAndMaintenance(t *testing.T) {
	reg := buildRegistry(t, node.StateActive, node.StateMaintenance, node.StateOffline)
	rr := scheduler.NewRoundRobin(reg)

	for i := 0; i < 100; i++ {
		n, err := rr.Pick(context.Background(), scheduler.InviteContext{})
		require.NoError(t, err)
		require.NotNil(t, n)
		require.Equal(t, node.StateActive, n.State, "只允许命中 active 节点")
	}
}

func TestRoundRobin_NoActiveNodes_ReturnsError(t *testing.T) {
	reg := buildRegistry(t, node.StateOffline, node.StateOffline)
	rr := scheduler.NewRoundRobin(reg)

	n, err := rr.Pick(context.Background(), scheduler.InviteContext{})
	require.Nil(t, n)
	require.ErrorIs(t, err, scheduler.ErrNoActiveNode)
}

func TestRoundRobin_Concurrent(t *testing.T) {
	reg := buildRegistry(t, node.StateActive, node.StateActive, node.StateActive)
	rr := scheduler.NewRoundRobin(reg)

	var wg sync.WaitGroup
	var ok atomic.Int64
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if _, err := rr.Pick(context.Background(), scheduler.InviteContext{}); err == nil {
					ok.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int64(1000), ok.Load())
}

// ---------- Manager + Factory ----------

func TestManager_SwitchAlgorithm(t *testing.T) {
	reg := buildRegistry(t, node.StateActive)
	factory := scheduler.NewFactory(reg)
	m := scheduler.NewManager(factory)

	// 默认未设置,Pick 应返错
	_, err := m.Pick(context.Background(), scheduler.InviteContext{})
	require.Error(t, err)

	// 切到 roundrobin OK
	require.NoError(t, m.Switch("roundrobin"))
	require.Equal(t, "roundrobin", m.CurrentName())

	picked, err := m.Pick(context.Background(), scheduler.InviteContext{})
	require.NoError(t, err)
	require.NotNil(t, picked)

	// 切到不存在的算法应返错,且 current 不变
	err = m.Switch("nonexistent")
	require.Error(t, err)
	require.Equal(t, "roundrobin", m.CurrentName())
}

func TestFactory_Build(t *testing.T) {
	reg := buildRegistry(t, node.StateActive)
	factory := scheduler.NewFactory(reg)

	s, err := factory.Build("roundrobin")
	require.NoError(t, err)
	require.NotNil(t, s)
	require.Equal(t, "roundrobin", s.Name())

	// weighted 已 M3 T3.1 实装
	sw, err := factory.Build("weighted")
	require.NoError(t, err)
	require.NotNil(t, sw)
	require.Equal(t, "weighted", sw.Name())

	_, err = factory.Build("leastload")
	require.Error(t, err)
	require.Contains(t, err.Error(), "M3")

	_, err = factory.Build("unknown")
	require.Error(t, err)
}

// 防止 errors 被未用警告
var _ = errors.New
