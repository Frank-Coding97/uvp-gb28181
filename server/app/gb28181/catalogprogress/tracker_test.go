package catalogprogress

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTrackerTracksCatalogChunksAndPersistsTerminalState(t *testing.T) {
	tracker := NewTracker(time.Minute, time.Minute)
	started := tracker.Start("device-1")
	require.NotEmpty(t, started.Snapshot.OperationID)
	require.Equal(t, StatusWaiting, started.Snapshot.Status)

	tracker.Bind(started.Snapshot.OperationID, 7)
	tracker.Observe("device-1", 7, 2, 5, false)
	progress, ok := tracker.Get(started.Snapshot.OperationID)
	require.True(t, ok)
	require.Equal(t, StatusReceiving, progress.Status)
	require.Equal(t, 2, progress.ReceivedCount)
	require.NotNil(t, progress.TotalCount)
	require.Equal(t, 5, *progress.TotalCount)

	tracker.Observe("device-1", 7, 5, 5, true)
	progress, _ = tracker.Get(started.Snapshot.OperationID)
	require.Equal(t, StatusPersisting, progress.Status)
	tracker.Finish("device-1", 7, nil)
	progress, _ = tracker.Get(started.Snapshot.OperationID)
	require.Equal(t, StatusCompleted, progress.Status)
	require.NotNil(t, progress.CompletedAt)
}

func TestTrackerDeduplicatesActiveDeviceAndRejectsOtherSN(t *testing.T) {
	tracker := NewTracker(time.Minute, time.Minute)
	first := tracker.Start("device-1")
	second := tracker.Start("device-1")
	require.True(t, second.Deduplicated)
	require.Equal(t, first.Snapshot.OperationID, second.Snapshot.OperationID)

	tracker.Bind(first.Snapshot.OperationID, 7)
	tracker.Observe("device-1", 8, 4, 4, true)
	progress, _ := tracker.Get(first.Snapshot.OperationID)
	require.Equal(t, StatusWaiting, progress.Status)
}

func TestTrackerTimesOutWaitingOperation(t *testing.T) {
	tracker := NewTracker(10*time.Millisecond, time.Minute)
	started := tracker.Start("device-1")
	require.Eventually(t, func() bool {
		progress, ok := tracker.Get(started.Snapshot.OperationID)
		return ok && progress.Status == StatusTimeout
	}, time.Second, time.Millisecond)
}

func TestTrackerDoesNotReplaceTimeoutWithLateSendFailure(t *testing.T) {
	tracker := NewTracker(10*time.Millisecond, time.Minute)
	started := tracker.Start("device-1")
	require.Eventually(t, func() bool {
		progress, ok := tracker.Get(started.Snapshot.OperationID)
		return ok && progress.Status == StatusTimeout
	}, time.Second, time.Millisecond)

	tracker.Fail(started.Snapshot.OperationID, errors.New("late send failure"))
	progress, _ := tracker.Get(started.Snapshot.OperationID)
	require.Equal(t, StatusTimeout, progress.Status)
}

func TestTrackerRecordsFailureAfterPersist(t *testing.T) {
	tracker := NewTracker(time.Minute, time.Minute)
	started := tracker.Start("device-1")
	tracker.Bind(started.Snapshot.OperationID, 7)
	tracker.Observe("device-1", 7, 1, 1, true)
	tracker.Finish("device-1", 7, errors.New("persist failed"))
	progress, _ := tracker.Get(started.Snapshot.OperationID)
	require.Equal(t, StatusFailed, progress.Status)
	require.Equal(t, "persist failed", progress.ErrorMessage)
}

// 剪枝标记只对「手动刷新」生效，且必须按 deviceID + SN 对齐 ——
// 它是删除动作的前置开关，错一次就会删掉真实通道。
func TestTrackerPruneRequestRequiresMatchingActiveOperation(t *testing.T) {
	tracker := NewTracker(time.Minute, time.Minute)

	// 没有活跃任务时要求剪枝：不能凭空为某台设备打开开关
	tracker.RequestPrune("device-1")
	require.False(t, tracker.NeedPrune("device-1", 0))

	started := tracker.Start("device-1")
	tracker.Bind(started.Snapshot.OperationID, 7)
	// 注册成功后自动触发的 Catalog 不标记 ⇒ 默认不剪
	require.False(t, tracker.NeedPrune("device-1", 7))

	tracker.RequestPrune("device-1")
	require.True(t, tracker.NeedPrune("device-1", 7))
	// SN 对不上（这次应答属于另一次查询）不剪
	require.False(t, tracker.NeedPrune("device-1", 9))

	// 别的设备不受影响
	require.False(t, tracker.NeedPrune("device-2", 7))

	// Finish 之后任务摘除，开关随之失效
	tracker.Finish("device-1", 7, nil)
	require.False(t, tracker.NeedPrune("device-1", 7))
	require.NotEmpty(t, started.Snapshot.OperationID)
}
