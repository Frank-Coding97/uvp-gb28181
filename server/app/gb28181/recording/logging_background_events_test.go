package recording

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"uvplatform.cn/uvp-gb28181/app/global/app"
)

// observeRecordingFailure 跑一次失败记录并把 observer 交回给断言
func observeRecordingFailure(t *testing.T, record func(s *CatalogReconcileScheduler)) (*CatalogReconcileScheduler, *observer.ObservedLogs) {
	t.Helper()
	core, entries := observer.New(zap.DebugLevel)
	previous := app.ZapLog
	app.ZapLog = zap.New(core)
	t.Cleanup(func() { app.ZapLog = previous })

	scheduler := &CatalogReconcileScheduler{}
	record(scheduler)
	return scheduler, entries
}

// ⭐ 本测试的核心断言是 `node_id` **是独立字段**。
//
// 旧写法把节点主键拼进描述字符串（`"run-node:node=" + strconv.FormatInt(nodeID, 10)`），
// 日志里只有一个 `scope="run-node:node=7"` —— 人眼能看懂，**按 node_id 聚合做不到**。
// 而这恰恰是这类日志唯一的排障用法（"7 号节点的录像目录对账是不是一直失败"）。
// 所以这条断言是**防回归的**，不是装饰：谁把它改回字符串拼接，测试就红。
func TestLoggingBackgroundEvents(t *testing.T) {
	scheduler, entries := observeRecordingFailure(t, func(s *CatalogReconcileScheduler) {
		s.recordNodeFailure("run_node", 7, errors.New("catalog failure"), context.Background())
	})

	require.Len(t, entries.All(), 1)
	entry := entries.All()[0]
	require.Equal(t, "recording.catalog", entry.LoggerName)
	require.Equal(t, "recording.catalog.reconcile_failed", entry.ContextMap()["event"])
	require.EqualValues(t, 7, entry.ContextMap()["node_id"])
	require.Equal(t, "run_node", entry.ContextMap()["phase"])
	require.NotContains(t, entry.Message, "catalog failure")

	count, last := scheduler.FailureStats()
	require.EqualValues(t, 1, count)
	require.Contains(t, last, "node=7")
}

// 没有具体节点时（`Enqueue` 自己失败）**不得**塞一个 `node_id=0` 进来 ——
// 那会把"调度器入队失败"谎报成"0 号节点失败"，比缺字段更糟。
func TestLoggingSchedulerFailureOmitsNodeID(t *testing.T) {
	_, entries := observeRecordingFailure(t, func(s *CatalogReconcileScheduler) {
		s.recordSchedulerFailure("enqueue_scheduled", errors.New("enqueue failure"), context.Background())
	})

	require.Len(t, entries.All(), 1)
	entry := entries.All()[0]
	require.Equal(t, "recording.catalog.reconcile_failed", entry.ContextMap()["event"])
	require.Equal(t, "enqueue_scheduled", entry.ContextMap()["phase"])
	require.NotContains(t, entry.ContextMap(), "node_id")
}
