package heartbeat_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/heartbeat"
	"uvplatform.com/uvp-gb28181/app/gb28181/zlm/node"
)

type fakeFetcher struct {
	mu        sync.Mutex
	netCalls  atomic.Int32
	workCalls atomic.Int32
	netReturn map[string]float64
	wrkReturn map[string]float64
	netErr    map[string]error
	wrkErr    map[string]error
}

func newFakeFetcher() *fakeFetcher {
	return &fakeFetcher{
		netReturn: map[string]float64{},
		wrkReturn: map[string]float64{},
		netErr:    map[string]error{},
		wrkErr:    map[string]error{},
	}
}

func (f *fakeFetcher) GetThreadsLoad(_ context.Context, n *node.Node) (float64, error) {
	f.netCalls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.netReturn[n.MediaServerUUID], f.netErr[n.MediaServerUUID]
}

func (f *fakeFetcher) GetWorkThreadsLoad(_ context.Context, n *node.Node) (float64, error) {
	f.workCalls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.wrkReturn[n.MediaServerUUID], f.wrkErr[n.MediaServerUUID]
}

func TestThreadLoadPoller_Tick_UpdatesStats(t *testing.T) {
	reg, _ := setupRegistry(t, "uuid-1")
	f := newFakeFetcher()
	f.netReturn["uuid-1"] = 0.35
	f.wrkReturn["uuid-1"] = 0.20

	p := heartbeat.NewThreadLoadPoller(reg, f, 30*time.Second)
	p.Tick(context.Background())

	// goroutine 异步,等 100ms
	time.Sleep(100 * time.Millisecond)

	got, _ := reg.GetByUUID("uuid-1")
	require.NotNil(t, got)
	require.InDelta(t, 0.35, got.Stats.NetThreadLoadAvg, 0.001)
	require.InDelta(t, 0.20, got.Stats.WorkThreadLoadAvg, 0.001)
}

func TestThreadLoadPoller_Tick_SkipsOfflineNode(t *testing.T) {
	reg, id := setupRegistry(t, "uuid-1")
	// 标 offline
	require.NoError(t, reg.MarkOffline(context.Background(), id))

	f := newFakeFetcher()
	f.netReturn["uuid-1"] = 0.5

	p := heartbeat.NewThreadLoadPoller(reg, f, 30*time.Second)
	p.Tick(context.Background())
	time.Sleep(50 * time.Millisecond)

	require.Zero(t, f.netCalls.Load(), "offline 节点不应被拉")
}

func TestThreadLoadPoller_Tick_FetcherErrorDoesNotPanic(t *testing.T) {
	reg, _ := setupRegistry(t, "uuid-1")
	f := newFakeFetcher()
	f.netErr["uuid-1"] = errors.New("connection refused")
	f.wrkErr["uuid-1"] = errors.New("connection refused")

	p := heartbeat.NewThreadLoadPoller(reg, f, 30*time.Second)
	p.Tick(context.Background())
	time.Sleep(50 * time.Millisecond)

	got, _ := reg.GetByUUID("uuid-1")
	require.NotNil(t, got)
	// 失败时 stats 不更新,保持原值(0)
	require.Equal(t, 0.0, got.Stats.NetThreadLoadAvg)
}

func TestThreadLoadPoller_Tick_PreservesHeartbeatStats(t *testing.T) {
	reg, _ := setupRegistry(t, "uuid-1")
	// Collector 先写心跳字段
	reg.UpdateStats("uuid-1", node.Stats{
		MediaSourceCount: 7,
		SessionCount:     5,
	})

	f := newFakeFetcher()
	f.netReturn["uuid-1"] = 0.4
	f.wrkReturn["uuid-1"] = 0.3

	p := heartbeat.NewThreadLoadPoller(reg, f, 30*time.Second)
	p.Tick(context.Background())
	time.Sleep(50 * time.Millisecond)

	got, _ := reg.GetByUUID("uuid-1")
	require.Equal(t, 7, got.Stats.MediaSourceCount, "Poller 不应覆盖 MediaSource")
	require.Equal(t, 5, got.Stats.SessionCount, "Poller 不应覆盖 Session")
	require.InDelta(t, 0.4, got.Stats.NetThreadLoadAvg, 0.001)
	require.InDelta(t, 0.3, got.Stats.WorkThreadLoadAvg, 0.001)
}

// ---------------------------------------------------------------------------
// 2026-10-18 现场：192.168.10.220:18090 可达（外部 curl 秒回）却被永久判离线。
// 因为 Watcher 判 offline 之后，常规轮询不再覆盖它 ⇒ UpdateLivenessAt 这条
// "主动探测等价刷新存活"的通道永远轮不到 ⇒ 单向死循环。
//
// ⛔ 但修法不能是把离线节点直接塞进常规轮询 ——
//    TestThreadLoadPoller_Tick_SkipsOfflineNode 锁住的是"别每 30s 去敲失联节点"。
// 所以新增的是一条**慢通道**：每 N 次 Tick 才给离线节点一次复活机会。
// ---------------------------------------------------------------------------

func TestThreadLoadPoller_ReviveTick_ProbesOfflineNodeAndBringsItBack(t *testing.T) {
	reg, id := setupRegistry(t, "uuid-offline")
	require.NoError(t, reg.MarkOffline(context.Background(), id))

	f := newFakeFetcher()
	f.netReturn["uuid-offline"] = 0.5
	f.wrkReturn["uuid-offline"] = 0.2

	p := heartbeat.NewThreadLoadPoller(reg, f, 30*time.Second)
	p.SetReviveEvery(3)

	// 前两次都不该碰离线节点（慢通道还没到点）
	for i := 0; i < 2; i++ {
		p.Tick(context.Background())
	}
	time.Sleep(80 * time.Millisecond)
	require.Zero(t, f.netCalls.Load(), "没到复活轮次时，离线节点仍不应被拉")

	// 第 3 次轮到慢通道
	p.Tick(context.Background())
	require.Eventually(t, func() bool {
		got, ok := reg.GetByUUID("uuid-offline")
		return ok && got != nil && got.State == node.StateActive
	}, 2*time.Second, 10*time.Millisecond,
		"复活探测成功必须把可达的离线节点翻回 active，否则它就永久卡在离线")

	got, _ := reg.GetByUUID("uuid-offline")
	require.False(t, got.Stats.LastHeartbeatAt.IsZero(), "同��应刷新最后存活时间")
	require.InDelta(t, 0.5, got.Stats.NetThreadLoadAvg, 0.001)
}

func TestThreadLoadPoller_ReviveTick_KeepsOfflineWhenProbeFails(t *testing.T) {
	reg, id := setupRegistry(t, "uuid-dead")
	require.NoError(t, reg.MarkOffline(context.Background(), id))

	f := newFakeFetcher()
	f.netErr["uuid-dead"] = errors.New("connection refused")

	p := heartbeat.NewThreadLoadPoller(reg, f, 30*time.Second)
	p.SetReviveEvery(1)
	p.Tick(context.Background())
	time.Sleep(80 * time.Millisecond)

	got, _ := reg.GetByUUID("uuid-dead")
	require.Equal(t, node.StateOffline, got.State, "探不到就不许翻回在线")
}

func TestReviveCandidates_SkipsMaintenanceAndDisabledNodes(t *testing.T) {
	// ⛔ 运维意图不可被"探活"悄悄翻掉：maintenance 与 disabled 不进复活候选。
	// setupRegistry 每次都新建 Registry，所以这里手工在同一个 registry 里造三种节点。
	reg, _ := setupRegistry(t, "uuid-parked")
	for uuid, opts := range map[string]struct {
		state      node.State
		adminState string
	}{
		"uuid-offline":  {state: node.StateOffline},
		"uuid-maint":    {state: node.StateMaintenance},
		"uuid-disabled": {state: node.StateOffline, adminState: "disabled"},
	} {
		_, err := reg.Add(context.Background(), node.Node{
			Name: "n-" + uuid, MediaServerUUID: uuid, State: opts.state, AdminState: opts.adminState,
		})
		require.NoError(t, err)
	}

	var got []string
	for _, c := range reg.ListReviveCandidates() {
		got = append(got, c.MediaServerUUID)
	}
	require.Equal(t, []string{"uuid-offline"}, got,
		"只有「离线且未被人工移出服务体系」的节点允许进复活候选")
}
