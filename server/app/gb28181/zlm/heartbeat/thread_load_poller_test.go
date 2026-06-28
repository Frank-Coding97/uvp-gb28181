package heartbeat_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/heartbeat"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
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
