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
