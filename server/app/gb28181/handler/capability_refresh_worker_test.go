package handler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 2026-11-09 万级压测事故的防回归：能力刷新原来用**无超时**的
// `r.workers <- struct{}{}` 抢下行槽位。一旦槽位里的活卡住（发送队列堵、
// 数据库慢），4 个槽位占满后所有后续设备的刷新全部永久挂在这行上，
// 而设备级 running 标不释放 ⇒ 后续触发全被判"已在刷新"直接丢弃。
// 现场表现：10002 台里只有前 38 台读到配置，剩下 99.6% 静默消失，零告警。
func TestAcquireWorkerTimesOutWhenEverySlotIsBusy(t *testing.T) {
	workers := make(chan struct{}, 1)
	workers <- struct{}{}

	started := time.Now()
	acquired := acquireWorker(context.Background(), workers, 120*time.Millisecond)
	elapsed := time.Since(started)

	require.False(t, acquired, "槽位被占满时必须超时返回 false，不能永久阻塞")
	require.GreaterOrEqual(t, elapsed, 100*time.Millisecond, "必须真的等过一个等待窗口，而不是立刻放弃")
}

func TestAcquireWorkerTakesFreeSlotWithoutWaiting(t *testing.T) {
	workers := make(chan struct{}, 1)

	require.True(t, acquireWorker(context.Background(), workers, time.Second))
	require.Len(t, workers, 1, "拿到槽位要真的占住，交给调用方 defer 释放")
}

func TestAcquireWorkerStopsWhenRefreshBudgetExpires(t *testing.T) {
	workers := make(chan struct{}, 1)
	workers <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.False(t, acquireWorker(ctx, workers, time.Minute),
		"整体预算到期必须立刻放手，不能被等待窗口拖住")
}

// release 是"任务压根没被准入"时的收尾：只清状态，**绝不**触发补轮。
// 触发补轮会再次碰到已关闭的准入 ⇒ 再次 release ⇒ 无限递归（停机时就是死循环）。
func TestReleaseDropsStateWithoutSchedulingFollowUp(t *testing.T) {
	r := &deviceCapabilityRefresher{
		workers: make(chan struct{}, 1),
		running: make(map[string]struct{}),
		pending: make(map[string]struct{}),
	}
	require.True(t, r.begin("34020000001320000001"))
	require.False(t, r.begin("34020000001320000001"), "同一设备重复触发只登记待补")
	require.NotEmpty(t, r.pending)

	r.release("34020000001320000001")

	r.mu.Lock()
	defer r.mu.Unlock()
	require.Empty(t, r.running, "running 必须释放，否则该设备永远被判『正在刷新』")
	require.Empty(t, r.pending, "pending 也要清掉，否则下一轮仍会被当成待补")
}

func TestRefreshDropsTriggerWhenDeviceAlreadyRunning(t *testing.T) {
	r := &deviceCapabilityRefresher{
		workers: make(chan struct{}, 1),
		running: make(map[string]struct{}),
		pending: make(map[string]struct{}),
	}
	device := "34020000001320000002"
	r.running[device] = struct{}{}

	require.False(t, r.begin(device))
	r.mu.Lock()
	defer r.mu.Unlock()
	require.Contains(t, r.pending, device, "已在刷新时不能直接丢弃触发，要登记待补一轮（分批 Catalog 的后几批靠它）")
}

// 并发登记/取走不能丢标记，也不能出现 map 并发写。
func TestPendingConsumeIsRaceFree(t *testing.T) {
	r := &deviceCapabilityRefresher{running: make(map[string]struct{}), pending: make(map[string]struct{})}
	device := "34020000001320000003"
	r.running[device] = struct{}{}

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.begin(device)
			r.consumePending(device)
		}()
	}
	wg.Wait()
}
