package ptz

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 回写预算必须可覆盖,且真正作用于派发路径派生出的 ctx。
// 原先硬编码 2s:一次回写要跑四趟往返,高延迟库链路(跨隧道单趟 80~150ms、
// 抖包后重传 200ms 起)上正常抖动就会被记成 persist_failed。
func TestSchedulerWritebackContextBudget(t *testing.T) {
	t.Run("默认预算", func(t *testing.T) {
		scheduler := NewScheduler(nil)
		require.Equal(t, defaultSchedulerPersistTimeout, scheduler.persistTimeout)

		startedAt := time.Now()
		ctx, cancel := scheduler.writebackContext(context.Background())
		defer cancel()
		deadline, ok := ctx.Deadline()
		require.True(t, ok, "回写 ctx 必须有截止时间,否则卡住的写库会永久占用派发槽位")
		require.WithinDuration(t, startedAt.Add(defaultSchedulerPersistTimeout), deadline, 2*time.Second)
	})

	t.Run("选项可覆盖", func(t *testing.T) {
		scheduler := NewScheduler(nil, WithSchedulerPersistTimeout(700*time.Millisecond))
		require.Equal(t, 700*time.Millisecond, scheduler.persistTimeout)

		ctx, cancel := scheduler.writebackContext(context.Background())
		defer cancel()
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.WithinDuration(t, time.Now().Add(700*time.Millisecond), deadline, time.Second)
	})

	t.Run("非法值回退默认", func(t *testing.T) {
		scheduler := NewScheduler(nil, WithSchedulerPersistTimeout(0))
		require.Equal(t, defaultSchedulerPersistTimeout, scheduler.persistTimeout)

		negative := NewScheduler(nil, WithSchedulerPersistTimeout(-time.Second))
		require.Equal(t, defaultSchedulerPersistTimeout, negative.persistTimeout)
		ctx, cancel := negative.writebackContext(context.Background())
		defer cancel()
		require.True(t, time.Until(mustDeadline(t, ctx)) > time.Second)
	})

	// 派发 ctx 在租约到期或停机时已被取消,但结果回写是既成事实,
	// 不能被取消信号切断 —— 否则已发出的指令会丢掉结果证据。
	t.Run("父 ctx 取消不传染", func(t *testing.T) {
		parent, cancelParent := context.WithCancel(context.Background())
		cancelParent()
		require.Error(t, parent.Err())

		scheduler := NewScheduler(nil)
		ctx, cancel := scheduler.writebackContext(parent)
		defer cancel()
		require.NoError(t, ctx.Err())
		require.True(t, time.Until(mustDeadline(t, ctx)) > 0)
	})
}

func mustDeadline(t *testing.T, ctx context.Context) time.Time {
	t.Helper()
	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	return deadline
}
