package catalog

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestAcquireCapsConcurrentBatches 守住入库并发闸门。
//
// 背景（2026-11-09 万级压测事故）：ingest 内部**逐条 item 各开一个事务**，
// 128 台设备同时回目录把 maxopenconns=128 的连接池瞬间占满，128 条事务
// 悬挂在 gb_catalog_node 上永不释放，全平台 DB 饥饿。
// 这条测试钉住"同时在跑的入库批次不会超过闸门容量"。
func TestAcquireCapsConcurrentBatches(t *testing.T) {
	p := New(nil)

	var inflight, peak int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, release, err := p.acquire(context.Background())
			require.NoError(t, err)
			defer release()

			n := atomic.AddInt32(&inflight, 1)
			for {
				old := atomic.LoadInt32(&peak)
				if n <= old || atomic.CompareAndSwapInt32(&peak, old, n) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			atomic.AddInt32(&inflight, -1)
		}()
	}
	wg.Wait()

	require.LessOrEqual(t, peak, int32(defaultIngestConcurrency),
		"同时在跑的入库批次超过了闸门容量，会打爆 DB 连接池")
	require.Greater(t, peak, int32(1), "闸门不该把入库退化成完全串行")
}

// TestAcquireAppliesDeadline 确保即便上游 ctx 没有超时，入库也拿到 deadline。
//
// 上游 HandleCatalogResponse 的 ctx 来自 SIP 消息处理，没有 deadline；
// 没有 deadline 就意味着任何一次卡住都永不回头（实测悬挂 8 分钟以上）。
func TestAcquireAppliesDeadline(t *testing.T) {
	p := New(nil)

	ctx, release, err := p.acquire(context.Background())
	require.NoError(t, err)
	defer release()

	deadline, ok := ctx.Deadline()
	require.True(t, ok, "入库 ctx 必须带 deadline，否则卡住时没有任何机制能中断它")
	require.WithinDuration(t, time.Now().Add(IngestBatchTimeout), deadline, 5*time.Second)
}

// TestAcquireReleasesSlotWhenCallerCtxDone 确保调用方 ctx 已取消时闸门不泄漏。
func TestAcquireReleasesSlotWhenCallerCtxDone(t *testing.T) {
	p := New(nil)

	// 先占满所有槽位
	for i := 0; i < defaultIngestConcurrency; i++ {
		_, release, err := p.acquire(context.Background())
		require.NoError(t, err)
		defer release()
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := p.acquire(canceled)
	require.Error(t, err, "槽位已满且调用方已取消，必须立刻返回而不是死等")
	require.Len(t, p.gate(), defaultIngestConcurrency, "失败的抢占不该占用槽位")
}

// TestAcquireReleaseReturnsSlot 确保 release 后槽位确实归还。
func TestAcquireReleaseReturnsSlot(t *testing.T) {
	p := New(nil)

	_, release, err := p.acquire(context.Background())
	require.NoError(t, err)
	require.Len(t, p.gate(), 1)

	release()
	require.Empty(t, p.gate(), "release 必须把槽位还回去，否则池子会被慢慢吃光")
}
