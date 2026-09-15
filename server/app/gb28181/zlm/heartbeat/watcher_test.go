package heartbeat_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/heartbeat"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/node"
)

const (
	tCheckInterval    = 30 * time.Second
	tOfflineThreshold = 90 * time.Second
)

// 构造 registry + Watcher + fakeClock,注入心跳节点
func newWatcherFixture(t *testing.T) (*node.Registry, *memoryRepo, *fakeClock, *heartbeat.Watcher, int64) {
	t.Helper()
	repo := newMemoryRepo()
	reg := node.NewRegistry(repo)
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	clock := newFakeClock(start)

	added, err := reg.Add(context.Background(), node.Node{
		Name:            "zlm-a",
		MediaServerUUID: "uuid-a",
		State:           node.StateActive,
	})
	require.NoError(t, err)

	w := heartbeat.NewWatcher(reg, clock, tCheckInterval, tOfflineThreshold)
	return reg, repo, clock, w, added.ID
}

func TestWatcher_MarksOfflineAfter90s(t *testing.T) {
	reg, repo, clock, w, id := newWatcherFixture(t)

	// 节点 now 有过心跳
	reg.UpdateStats("uuid-a", node.Stats{LastHeartbeatAt: clock.Now()})
	baseUpdates := repo.UpdateCount()

	// 推进 91s,超过 90s 阈值
	clock.Advance(91 * time.Second)
	w.Tick()

	got, ok := reg.Get(id)
	require.True(t, ok)
	require.Equal(t, node.StateOffline, got.State, "超阈值应标 offline")
	require.Greater(t, repo.UpdateCount(), baseUpdates, "MarkOffline 应触发 DB.Update")
}

func TestWatcher_RecentHeartbeat_StaysActive(t *testing.T) {
	reg, repo, clock, w, id := newWatcherFixture(t)

	reg.UpdateStats("uuid-a", node.Stats{LastHeartbeatAt: clock.Now()})
	baseUpdates := repo.UpdateCount()

	// 推进 60s,< 90s 阈值
	clock.Advance(60 * time.Second)
	w.Tick()

	got, ok := reg.Get(id)
	require.True(t, ok)
	require.Equal(t, node.StateActive, got.State, "未超阈值应保持 active")
	require.Equal(t, baseUpdates, repo.UpdateCount(), "未越界不应写 DB")
}

func TestWatcher_OfflineNode_StaysOffline(t *testing.T) {
	reg, repo, clock, w, id := newWatcherFixture(t)

	// 心跳设在 100s 前,然后先 Tick 一次将节点标 offline
	reg.UpdateStats("uuid-a", node.Stats{LastHeartbeatAt: clock.Now()})
	clock.Advance(91 * time.Second)
	w.Tick()
	got, _ := reg.Get(id)
	require.Equal(t, node.StateOffline, got.State)

	updatesAfterFirstTick := repo.UpdateCount()

	// 再推进 + 再 Tick:已 offline 不应重复写 DB(幂等)
	clock.Advance(60 * time.Second)
	w.Tick()
	require.Equal(t, updatesAfterFirstTick, repo.UpdateCount(), "已 offline 不应重复 MarkOffline")
}

func TestWatcher_NoHeartbeatYet_StaysActive(t *testing.T) {
	// 刚 Add 的节点 LastHeartbeatAt 为 zero — Watcher 不应误标 offline
	reg, repo, clock, w, id := newWatcherFixture(t)
	baseUpdates := repo.UpdateCount()

	clock.Advance(91 * time.Second)
	w.Tick()

	got, _ := reg.Get(id)
	require.Equal(t, node.StateActive, got.State, "从未心跳过的节点 Watcher 不应误标")
	require.Equal(t, baseUpdates, repo.UpdateCount())
}

func TestWatcher_MaintenanceNode_NotTouched(t *testing.T) {
	repo := newMemoryRepo()
	reg := node.NewRegistry(repo)
	clock := newFakeClock(time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC))
	added, err := reg.Add(context.Background(), node.Node{
		Name:            "zlm-m",
		MediaServerUUID: "uuid-m",
		State:           node.StateMaintenance,
	})
	require.NoError(t, err)

	w := heartbeat.NewWatcher(reg, clock, tCheckInterval, tOfflineThreshold)
	reg.UpdateStats("uuid-m", node.Stats{LastHeartbeatAt: clock.Now()})
	baseUpdates := repo.UpdateCount()

	clock.Advance(120 * time.Second)
	w.Tick()

	got, _ := reg.Get(added.ID)
	require.Equal(t, node.StateMaintenance, got.State, "维护态节点不归 Watcher 管")
	require.Equal(t, baseUpdates, repo.UpdateCount())
}
