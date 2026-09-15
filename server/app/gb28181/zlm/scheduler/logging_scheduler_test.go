package scheduler_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm/scheduler"
	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// C07 契约：调度日志写入链路的两个「组件级」事件，判定结果**相反**，所以一起锁住。
//
//   - zlm.scheduler.persist_failed —— 写库失败。定位对象是「哪个节点、哪种算法的
//     那条决策没落库」，值就在 SchedulerLog 上，必须带上。
//   - zlm.scheduler.buffer_full —— 进程内队列满而丢弃。这**不是**某个节点的问题：
//     每 100 条才采样一行，如果打一个「碰巧路过的 entry」的 node_id，
//     就会把「所有节点的调度日志都在丢」谎报成「这个节点在丢」。
//     本断言锁住「将来不要顺手给它补 node_id」。
//
// 判据来源：日志内容治理三问（定位谁 / 看到它要做什么 / 别处说过没有）。
// 队列满的答案是「调大队列或查 DB 写入是否卡住」——动作在组件上，不在节点上。

func observeSchedulerLogs(t *testing.T) *observer.ObservedLogs {
	t.Helper()
	core, observed := observer.New(zap.DebugLevel)
	previous := app.ZapLog
	app.ZapLog = zap.New(core)
	t.Cleanup(func() { app.ZapLog = previous })
	return observed
}

func schedulerLogFields(observed *observer.ObservedLogs, event string) (map[string]interface{}, bool) {
	for _, entry := range observed.All() {
		if entry.ContextMap()["event"] == event {
			return entry.ContextMap(), true
		}
	}
	return nil, false
}

func TestSchedulerLoggingPersistFailureCarriesNodeIdentity(t *testing.T) {
	observed := observeSchedulerLogs(t)
	repo := newFakeSchedulerLogRepo()
	repo.insertErr = errors.New("insert scheduler log failed")

	svc := scheduler.NewLogService(repo, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	defer svc.Stop()

	svc.Emit(scheduler.SchedulerLog{
		HappenedAt: time.Now(),
		Algorithm:  "weighted",
		NodeID:     42,
		NodeName:   "zlm-a",
		StreamID:   "s-1",
	})

	require.Eventually(t, func() bool {
		_, ok := schedulerLogFields(observed, "zlm.scheduler.persist_failed")
		return ok
	}, 2*time.Second, 10*time.Millisecond, "写库失败必须留下 persist_failed")

	fields, _ := schedulerLogFields(observed, "zlm.scheduler.persist_failed")
	require.Equal(t, int64(42), fields["node_id"], "写库失败必须能定位到哪个节点")
	require.Equal(t, "weighted", fields["algorithm"], "算法名是这条决策的另一半身份")
	require.Contains(t, fields, "error", "错误必须走结构化 error 字段(class/type),不落原文")
}

func TestSchedulerLoggingBufferFullStaysComponentLevel(t *testing.T) {
	observed := observeSchedulerLogs(t)
	repo := newFakeSchedulerLogRepo()
	repo.sleepOnInsert = 50 * time.Millisecond // 让 worker 变慢,逼出丢弃

	svc := scheduler.NewLogService(repo, 5)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	defer svc.Stop()

	// 每条都带不同 node_id —— 正是为了让「碰巧打出来的那条」有可能误导
	for i := 0; i < 120; i++ {
		svc.Emit(scheduler.SchedulerLog{
			HappenedAt: time.Now(),
			Algorithm:  "roundrobin",
			NodeID:     int64(i + 1),
		})
	}

	require.Eventually(t, func() bool {
		_, ok := schedulerLogFields(observed, "zlm.scheduler.buffer_full")
		return ok
	}, 2*time.Second, 10*time.Millisecond, "buffer 满必须留下 buffer_full")

	fields, _ := schedulerLogFields(observed, "zlm.scheduler.buffer_full")
	require.Contains(t, fields, "dropped_count", "丢弃总量是这条日志唯一的信息")
	require.NotContains(t, fields, "node_id",
		"丢弃是队列级事件:带上某一跳的 node_id 会把「所有节点都在丢」谎报成「这个节点在丢」")
}
