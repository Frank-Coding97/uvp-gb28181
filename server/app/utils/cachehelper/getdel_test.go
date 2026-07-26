package cachehelper

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// T0 — CacheInterf 原子 GetDel.
// 扫码回填 SIP 特性的一次性 token 依赖此原子性:持有二维码截图的人可以与合法扫码者
// 并发抢兑同一 token,Get→Del 两步无法保证"恰好一人成功"。
// spec: wiki/projects/uvp-gb28181/specs/qr-sip-provisioning.md (D7)

// 0.1 取出后 key 消失
func TestGetDel_RemovesKey(t *testing.T) {
	m := NewMemoryHelper()
	defer m.Close()
	ctx := context.Background()

	require.NoError(t, m.Set(ctx, "k", "v", time.Minute))

	got, err := m.GetDel(ctx, "k")
	require.NoError(t, err)
	require.Equal(t, "v", got)

	_, err = m.Get(ctx, "k")
	require.ErrorIs(t, err, app.ErrKeyNotFound, "GetDel 之后 key 必须已被删除")
}

// 0.2 key 不存在
func TestGetDel_MissingKey(t *testing.T) {
	m := NewMemoryHelper()
	defer m.Close()

	got, err := m.GetDel(context.Background(), "nope")
	require.ErrorIs(t, err, app.ErrKeyNotFound)
	require.Equal(t, "", got)
}

// 0.3 并发恰好一个成功(内存实现)
// 这是整个一次性 token 安全模型的地基 —— 必须带 -race 跑。
func TestGetDel_ConcurrentExactlyOnce(t *testing.T) {
	m := NewMemoryHelper()
	defer m.Close()
	ctx := context.Background()

	const n = 50
	// 用永不过期的 key（expiration < 0）:内存实现的后台清理 timer 存在既有的
	// 数据竞争(startCleanup 无锁读 m.cleanupTick vs resetCleanupTimer 锁内替换),
	// 带 TTL 会把那个竞争卷进来,淹没本用例真正要验的 GetDel 临界区。
	// 见 handoff 报告 F1。
	require.NoError(t, m.Set(ctx, "token", "payload", -1))

	var (
		mu        sync.Mutex
		successes int
		notFound  int
		others    []error
		start     = make(chan struct{})
		wg        sync.WaitGroup
	)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // 尽量让 n 个 goroutine 同时冲进 GetDel
			val, err := m.GetDel(ctx, "token")
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				successes++
				require.Equal(t, "payload", val)
			case errors.Is(err, app.ErrKeyNotFound):
				notFound++
			default:
				others = append(others, err)
			}
		}()
	}

	close(start)
	wg.Wait()

	require.Empty(t, others, "不应出现除 ErrKeyNotFound 以外的错误")
	require.Equal(t, 1, successes, "恰好 1 个 goroutine 能取到值")
	require.Equal(t, n-1, notFound, "其余全部 ErrKeyNotFound")
}

// 0.5 过期 key
func TestGetDel_ExpiredKey(t *testing.T) {
	m := NewMemoryHelper()
	defer m.Close()
	ctx := context.Background()

	require.NoError(t, m.Set(ctx, "k", "v", 10*time.Millisecond))
	time.Sleep(30 * time.Millisecond)

	_, err := m.GetDel(ctx, "k")
	require.ErrorIs(t, err, app.ErrKeyNotFound, "已过期的 key 视为不存在")
}

// 0.6 空值 key —— 区分"值为空串"与"键不存在"
func TestGetDel_EmptyValue(t *testing.T) {
	m := NewMemoryHelper()
	defer m.Close()
	ctx := context.Background()

	require.NoError(t, m.Set(ctx, "k", "", time.Minute))

	got, err := m.GetDel(ctx, "k")
	require.NoError(t, err, "值为空串不等于键不存在")
	require.Equal(t, "", got)

	_, err = m.Get(ctx, "k")
	require.ErrorIs(t, err, app.ErrKeyNotFound)
}

// GetDel 不能破坏过期堆:删掉带 TTL 的 key 后,其他 key 的过期清理仍须正常工作。
func TestGetDel_KeepsExpiryHeapConsistent(t *testing.T) {
	m := NewMemoryHelper()
	defer m.Close()
	ctx := context.Background()

	require.NoError(t, m.Set(ctx, "a", "1", time.Minute))
	require.NoError(t, m.Set(ctx, "b", "2", 10*time.Millisecond))

	_, err := m.GetDel(ctx, "a")
	require.NoError(t, err)

	time.Sleep(30 * time.Millisecond)
	_, err = m.Get(ctx, "b")
	require.ErrorIs(t, err, app.ErrKeyNotFound, "b 仍应按自己的 TTL 过期")

	cnt, err := m.Exists(ctx, "a", "b")
	require.NoError(t, err)
	require.Equal(t, int64(0), cnt)
}
