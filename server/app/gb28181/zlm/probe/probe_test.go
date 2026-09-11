package probe_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/probe"
)

// fakeRegistry 实现 probe.Registry
type fakeRegistry struct {
	mu           sync.Mutex
	nodes        []*node.Node
	markCalls    map[int64]int
	markErr      map[int64]error
	markLastCall map[int64]time.Time
}

func newFakeRegistry(ns ...*node.Node) *fakeRegistry {
	return &fakeRegistry{
		nodes:        ns,
		markCalls:    map[int64]int{},
		markErr:      map[int64]error{},
		markLastCall: map[int64]time.Time{},
	}
}

func (f *fakeRegistry) List() []*node.Node {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*node.Node, len(f.nodes))
	copy(out, f.nodes)
	return out
}

func (f *fakeRegistry) MarkActive(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markCalls[id]++
	f.markLastCall[id] = time.Now()
	if err, ok := f.markErr[id]; ok {
		return err
	}
	// 模拟内存翻转,便于后续断言(生产 Registry 是真翻,fake 这里也刷 State)
	for _, n := range f.nodes {
		if n.ID == id {
			n.State = node.StateActive
		}
	}
	return nil
}

// fakeClient 实现 probe.Client
type fakeClient struct {
	cfg   map[string]string
	err   error
	delay time.Duration
	calls int64
}

func (c *fakeClient) GetServerConfig(ctx context.Context) (map[string]string, error) {
	atomic.AddInt64(&c.calls, 1)
	if c.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(c.delay):
		}
	}
	if c.err != nil {
		return nil, c.err
	}
	return c.cfg, nil
}

func TestProber_Run_SingleNodeSuccess_FlipsOfflineToActive(t *testing.T) {
	n := &node.Node{ID: 1, Name: "zlm-a", Host: "10.0.0.1", State: node.StateOffline}
	reg := newFakeRegistry(n)
	factory := func(_ *node.Node) probe.Client { return &fakeClient{cfg: map[string]string{"general.mediaServerId": "u"}} }

	p := probe.New(reg, factory, 500*time.Millisecond, nil)
	results := p.Run(context.Background())

	require.Len(t, results, 1)
	require.True(t, results[0].Pass)
	require.Equal(t, node.StateOffline, results[0].StateBefore)
	require.Equal(t, node.StateActive, results[0].StateAfter)
	require.Equal(t, 1, reg.markCalls[1])
}

func TestProber_Run_SingleNodeTimeout_KeepsState(t *testing.T) {
	n := &node.Node{ID: 1, Name: "zlm-a", State: node.StateOffline}
	reg := newFakeRegistry(n)
	factory := func(_ *node.Node) probe.Client { return &fakeClient{delay: 200 * time.Millisecond} }

	p := probe.New(reg, factory, 50*time.Millisecond, nil)
	results := p.Run(context.Background())

	require.Len(t, results, 1)
	require.False(t, results[0].Pass)
	require.Error(t, results[0].Err)
	require.Equal(t, node.StateOffline, results[0].StateAfter, "失败保持原状态")
	require.Equal(t, 0, reg.markCalls[1], "失败不应触发 MarkActive")
}

func TestProber_Run_SingleNodeHTTPError_KeepsState(t *testing.T) {
	n := &node.Node{ID: 1, Name: "zlm-a", State: node.StateOffline}
	reg := newFakeRegistry(n)
	sentinel := errors.New("ZLM 401 secret 错")
	factory := func(_ *node.Node) probe.Client { return &fakeClient{err: sentinel} }

	p := probe.New(reg, factory, 500*time.Millisecond, nil)
	results := p.Run(context.Background())

	require.Len(t, results, 1)
	require.False(t, results[0].Pass)
	require.ErrorIs(t, results[0].Err, sentinel)
	require.Equal(t, 0, reg.markCalls[1])
}

func TestProber_Run_MaintenanceNodeSkipped(t *testing.T) {
	n := &node.Node{ID: 1, Name: "zlm-a", State: node.StateMaintenance}
	reg := newFakeRegistry(n)
	factory := func(_ *node.Node) probe.Client {
		t.Fatal("maintenance 节点不应触发 factory")
		return nil
	}

	p := probe.New(reg, factory, 500*time.Millisecond, nil)
	results := p.Run(context.Background())

	require.Len(t, results, 1)
	require.True(t, results[0].Skipped)
	require.False(t, results[0].Pass)
	require.Equal(t, node.StateMaintenance, results[0].StateAfter)
	require.Equal(t, 0, reg.markCalls[1])
}

func TestProber_Run_MultiNodeConcurrent(t *testing.T) {
	// 3 个节点都 sleep 100ms;串行 300ms,并发应 <= 150ms
	nodes := []*node.Node{
		{ID: 1, Name: "n1", State: node.StateOffline},
		{ID: 2, Name: "n2", State: node.StateOffline},
		{ID: 3, Name: "n3", State: node.StateOffline},
	}
	reg := newFakeRegistry(nodes...)
	factory := func(_ *node.Node) probe.Client {
		return &fakeClient{delay: 100 * time.Millisecond, cfg: map[string]string{"ok": "1"}}
	}

	p := probe.New(reg, factory, 500*time.Millisecond, nil)
	start := time.Now()
	results := p.Run(context.Background())
	elapsed := time.Since(start)

	require.Len(t, results, 3)
	for _, r := range results {
		require.True(t, r.Pass, "node %d 应 pass", r.NodeID)
	}
	require.Less(t, elapsed, 200*time.Millisecond, "并发应显著快于串行 300ms,实测 %s", elapsed)
	require.Equal(t, 1, reg.markCalls[1])
	require.Equal(t, 1, reg.markCalls[2])
	require.Equal(t, 1, reg.markCalls[3])
}

func TestProber_Run_MixedNodes(t *testing.T) {
	nodes := []*node.Node{
		{ID: 1, Name: "pass-offline", State: node.StateOffline},
		{ID: 2, Name: "fail", State: node.StateActive},
		{ID: 3, Name: "maint", State: node.StateMaintenance},
	}
	reg := newFakeRegistry(nodes...)
	sentinel := errors.New("boom")
	factory := func(n *node.Node) probe.Client {
		if n.ID == 2 {
			return &fakeClient{err: sentinel}
		}
		return &fakeClient{cfg: map[string]string{"ok": "1"}}
	}

	p := probe.New(reg, factory, 500*time.Millisecond, nil)
	results := p.Run(context.Background())
	require.Len(t, results, 3)

	// results 顺序保持跟 List() 一致
	require.True(t, results[0].Pass)
	require.Equal(t, node.StateOffline, results[0].StateBefore)
	require.Equal(t, node.StateActive, results[0].StateAfter)

	require.False(t, results[1].Pass)
	require.ErrorIs(t, results[1].Err, sentinel)

	require.True(t, results[2].Skipped)
}

func TestProber_Run_EmptyRegistry(t *testing.T) {
	reg := newFakeRegistry()
	factory := func(_ *node.Node) probe.Client {
		t.Fatal("空 registry 不应触发 factory")
		return nil
	}
	p := probe.New(reg, factory, 500*time.Millisecond, nil)
	results := p.Run(context.Background())
	require.Nil(t, results)
}

func TestProber_Run_MarkActiveError_StillCountsAsPass(t *testing.T) {
	n := &node.Node{ID: 1, Name: "a", State: node.StateOffline}
	reg := newFakeRegistry(n)
	reg.markErr[1] = errors.New("db down")
	factory := func(_ *node.Node) probe.Client { return &fakeClient{cfg: map[string]string{"ok": "1"}} }

	p := probe.New(reg, factory, 500*time.Millisecond, nil)
	results := p.Run(context.Background())
	require.Len(t, results, 1)
	require.True(t, results[0].Pass, "MarkActive 失败仍算 pass(内存已知节点活着,DB 写失败下次探活能重试)")
}
