package playback

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// exhaustingCleanupResources 模拟"设备始终不确认拆除"的资源:
// Teardown 永远失败,其余阶段本身可用。
type exhaustingCleanupResources struct {
	teardownErr error

	teardownCalls atomic.Int32
	closeCalls    atomic.Int32
	unbindCalls   atomic.Int32
	abandonCalls  atomic.Int32
}

func (r *exhaustingCleanupResources) Teardown(context.Context) error {
	r.teardownCalls.Add(1)
	return r.teardownErr
}
func (r *exhaustingCleanupResources) CloseRTP(context.Context) error {
	r.closeCalls.Add(1)
	return nil
}
func (r *exhaustingCleanupResources) Unbind(context.Context) error {
	r.unbindCalls.Add(1)
	return nil
}
func (r *exhaustingCleanupResources) AbandonDeviceTeardown(context.Context) error {
	r.abandonCalls.Add(1)
	return nil
}

// TestPlaybackCleanupExhaustionReleasesScope 钉住重试耗尽后的止损行为。
//
// 背景:清理失败时保留绑定、不释放占位是刻意的 fail-closed 设计,但设备可以
// 永远不应答 BYE/TEARDOWN。没有上限时该通道会被占位永久锁死(固定 429),
// 清扫器还会按秒重入。这里要求:达到上限后必须强制释放平台侧资源。
func TestPlaybackCleanupExhaustionReleasesScope(t *testing.T) {
	now := time.Unix(1700000000, 0)
	resources := &exhaustingCleanupResources{teardownErr: errors.New("device never confirms teardown")}
	metrics := &Metrics{}
	r := NewRegistry(RegistryConfig{Now: func() time.Time { return now }, Metrics: metrics})
	request := playbackRequest(now, "owner", "channel", "record")
	request.Resources = resources
	created, err := r.Create(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, 1, r.ActiveCount())

	// 上限之前:失败必须保持可重试,且占位不被释放(fail-closed 不被削弱)。
	for attempt := 1; attempt < playbackCleanupAttemptLimit; attempt++ {
		require.Error(t, r.Stop(context.Background(), created.Session.ID, "device transferred"))
		require.Equal(t, 1, r.ActiveCount(), "第 %d 次失败仍必须保留占位", attempt)
		// 必须让请求身份可区分(幂等键 + 设备标识),否则会命中"同一逻辑请求重放"
		// 的复用分支,测不到通道占位判定。
		probe := playbackRequest(now, "owner", "channel", fmt.Sprintf("probe-%d", attempt))
		probe.DeviceID = fmt.Sprintf("340200000020001000%02d", attempt)
		_, err := r.Create(context.Background(), probe)
		require.ErrorIs(t, err, ErrPlaybackBusy, "占位未释放时不得放行第二路回放")
	}

	// 达到上限:强制释放,会话按已终止结算。
	require.NoError(t, r.Stop(context.Background(), created.Session.ID, "device transferred"))

	session := r.MustGet(created.Session.ID)
	require.Equal(t, StateStopped, session.State, "重试耗尽后必须收敛到终态")
	require.Equal(t, 0, r.ActiveCount(), "重试耗尽后必须释放通道占位")
	require.Contains(t, session.EndReason, "强制释放", "降级事实必须留在 EndReason 里")
	require.EqualValues(t, 1, resources.abandonCalls.Load(), "必须解除设备侧确认前置条件")
	require.EqualValues(t, 1, resources.unbindCalls.Load(), "强制释放必须跑完 Unbind,不能沿用短路语义")
	require.EqualValues(t, 6, resources.teardownCalls.Load(), "4 次重试 + 第 5 次双跑(常规 + 强制)")
	require.EqualValues(t, 6, resources.closeCalls.Load())
	require.EqualValues(t, 1, metrics.CleanupAbandoned.Load())

	service := &Service{registry: r, config: ServiceConfig{Metrics: metrics}}
	require.EqualValues(t, 1, service.MetricsSnapshot().CleanupAbandoned)

	// 止损的意义:同一通道必须能立刻重新建会话,而不是被永久顶死。
	again, err := r.Create(context.Background(), playbackRequest(now, "owner", "channel", "record-2"))
	require.NoError(t, err)
	require.NotEqual(t, created.Session.ID, again.Session.ID)
}

// TestPlaybackCleanupBackoffDefersSweeperRetry 钉住清扫器的退避闸门。
//
// 清扫间隔是 1 秒,而单次拆除可能有秒级超时;没有闸门时一次失败会被放大成
// 每秒一轮的重复拆除(实测 13 分钟 1387 条 BYE)。
func TestPlaybackCleanupBackoffDefersSweeperRetry(t *testing.T) {
	now := time.Unix(1700000000, 0)
	clock := now
	resources := &exhaustingCleanupResources{teardownErr: errors.New("device never confirms teardown")}
	r := NewRegistry(RegistryConfig{Now: func() time.Time { return clock }, IdleTimeout: 100 * time.Millisecond})
	request := playbackRequest(now, "owner", "channel", "record")
	request.Resources = resources
	created, err := r.Create(context.Background(), request)
	require.NoError(t, err)

	require.Error(t, r.Stop(context.Background(), created.Session.ID, "device transferred"))
	require.EqualValues(t, 1, resources.teardownCalls.Load())

	// 会话已过期,但仍处在退避窗口内 ⇒ 清扫器根本不得发起重试。
	clock = now.Add(500 * time.Millisecond)
	count, err := r.SweepOnce(context.Background(), clock)
	require.NoError(t, err)
	require.Zero(t, count)
	require.EqualValues(t, 1, resources.teardownCalls.Load(), "退避窗口内不得发起第二次拆除")
	require.Equal(t, 1, r.ActiveCount(), "退避窗口内占位仍保持")

	// 窗口结束后才允许重试。
	clock = now.Add(cleanupBackoff(1) + time.Millisecond)
	_, _ = r.SweepOnce(context.Background(), clock)
	require.EqualValues(t, 2, resources.teardownCalls.Load(), "退避到期后必须重试")
}

// TestPlaybackCleanupRetryIsNotStarvedByStatusPolling 钉住"待拆除态不被状态轮询饿死"。
//
// 背景:GET 会话状态会走到 Registry.Touch 续租 IdleDeadline,而清扫器原先要求
// IdleDeadline 过期才重试。回放页面每 800ms 轮询一次,于是首次拆除失败之后清扫器
// 永远等不到重试窗口 —— 实测该通道一直返回 429「当前通道已有回放会话」,直到
// 进程重启才恢复。待拆除的会话不是"在用"的会话,唯一的推进机制是清扫器重试。
func TestPlaybackCleanupRetryIsNotStarvedByStatusPolling(t *testing.T) {
	now := time.Unix(1700000000, 0)
	clock := now
	resources := &exhaustingCleanupResources{teardownErr: errors.New("device never confirms teardown")}
	r := NewRegistry(RegistryConfig{
		Now:         func() time.Time { return clock },
		IdleTimeout: time.Minute,
		MaxSession:  24 * time.Hour,
	})
	request := playbackRequest(now, "owner", "channel", "record")
	request.Resources = resources
	created, err := r.Create(context.Background(), request)
	require.NoError(t, err)

	require.Error(t, r.Stop(context.Background(), created.Session.ID, "user stopped"))
	require.EqualValues(t, 1, resources.teardownCalls.Load())
	require.Equal(t, 1, r.ActiveCount(), "失败必须保留占位(fail-closed 不削弱)")

	// 第一项:待拆除态不接受续租。
	before := r.MustGet(created.Session.ID).IdleDeadline
	clock = clock.Add(time.Minute)
	require.NoError(t, r.Touch(created.Session.ID, clock))
	require.Equal(t, before, r.MustGet(created.Session.ID).IdleDeadline, "待拆除态不得被状态轮询续租")

	// 第二项:前端持续轮询的同时,清扫器仍必须能靠退避闸门推进。
	for i := 0; i < 20; i++ {
		clock = clock.Add(800 * time.Millisecond)
		require.NoError(t, r.Touch(created.Session.ID, clock))
	}
	clock = clock.Add(cleanupBackoff(1) + time.Millisecond)
	// SweepOnce 的返回值只统计"成功收敛"的会话,重试本身仍然失败时返回 0,
	// 所以这里用实际下发次数判定是否真的发起了重试。
	_, _ = r.SweepOnce(context.Background(), clock)
	require.EqualValues(t, 2, resources.teardownCalls.Load(), "待拆除态必须由退避闸门推进,不能等 IdleDeadline")
	require.Equal(t, 1, r.ActiveCount(), "重试仍失败时占位继续保留(fail-closed 不削弱)")
}
