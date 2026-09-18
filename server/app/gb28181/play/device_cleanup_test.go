package play

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/playauth"
)

type cleanupTestLease struct {
	ctx   context.Context
	epoch int64
	rel   atomic.Int32
}

func (l *cleanupTestLease) Context() context.Context { return l.ctx }
func (l *cleanupTestLease) OperationEpoch() int64    { return l.epoch }
func (l *cleanupTestLease) Release()                 { l.rel.Add(1) }

func knownCleanupCoordinator(t *testing.T, stop StopFunc, epoch func(Request) int64) *Coordinator {
	t.Helper()
	c := NewCoordinatorWithStop(func(_ context.Context, req Request) (*Result, error) {
		return &Result{StreamID: "stream-" + req.DeviceID + "-" + req.ChannelID, SSRC: "ssrc", Generation: 1}, nil
	}, stop)
	c.beginOperation = func(ctx context.Context, req Request) (playauth.DeviceOperationLease, error) {
		return &cleanupTestLease{ctx: ctx, epoch: epoch(req)}, nil
	}
	return c
}

func ensureCleanupEntry(t *testing.T, c *Coordinator, req Request) *Result {
	t.Helper()
	result, err := c.EnsureLive(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, result)
	return result
}

func TestCoordinatorClearDeviceBeforeStopsOnlyOlderKnownEntry(t *testing.T) {
	var mu sync.Mutex
	var stopped []string
	c := knownCleanupCoordinator(t, func(context.Context, *Result) error {
		mu.Lock()
		stopped = append(stopped, "stopped")
		mu.Unlock()
		return nil
	}, func(req Request) int64 {
		if req.DeviceID == "D" {
			return 1
		}
		return 1
	})

	ensureCleanupEntry(t, c, Request{DeviceID: "D", ChannelID: "ch", DeviceEpoch: 0})
	ensureCleanupEntry(t, c, Request{DeviceID: "Dother", ChannelID: "ch", DeviceEpoch: 0})

	report, err := c.ClearDeviceBefore(context.Background(), "D", 2)
	require.NoError(t, err)
	require.Equal(t, DeviceCleanupSettled, report.Status)
	require.Equal(t, 1, report.Settled)
	require.Zero(t, report.Pending)
	require.Zero(t, report.Unknown)
	mu.Lock()
	require.Len(t, stopped, 1)
	mu.Unlock()
	_, ok := c.CurrentResult("stream-D-ch")
	require.False(t, ok)
	_, ok = c.currentResultForKey(coordinatorKey{deviceID: "Dother", channelID: "ch"})
	require.True(t, ok, "a different device must not be stopped")
}

func TestCoordinatorClearDeviceBeforePreservesNewerAndReportsIt(t *testing.T) {
	var stops atomic.Int32
	c := knownCleanupCoordinator(t, func(context.Context, *Result) error {
		stops.Add(1)
		return nil
	}, func(Request) int64 { return 3 })
	ensureCleanupEntry(t, c, Request{DeviceID: "D", ChannelID: "ch", DeviceEpoch: 0})

	report, err := c.ClearDeviceBefore(context.Background(), "D", 2)
	require.NoError(t, err)
	require.Equal(t, DeviceCleanupUnknown, report.Status)
	require.Equal(t, 1, report.Newer)
	require.Zero(t, report.Settled)
	require.Zero(t, stops.Load())
	require.True(t, c.hasTrackedKey(coordinatorKey{deviceID: "D", channelID: "ch"}))
}

func TestCoordinatorClearDeviceBeforeTreatsRestoreAsUnknownAndEmptyAsNoEvidence(t *testing.T) {
	c := NewCoordinatorWithStop(func(context.Context, Request) (*Result, error) {
		return &Result{StreamID: "unused", SSRC: "unused", Generation: 1}, nil
	}, func(context.Context, *Result) error {
		t.Fatal("unknown restored entry must not be stopped")
		return nil
	})
	require.True(t, c.Restore(Request{DeviceID: "D", ChannelID: "ch"}, &Result{
		StreamID: "restored", SSRC: "ssrc", Generation: 7,
	}))

	report, err := c.ClearDeviceBefore(context.Background(), "D", 2)
	require.NoError(t, err)
	require.Equal(t, DeviceCleanupUnknown, report.Status)
	require.Equal(t, 1, report.Unknown)
	require.True(t, c.hasTrackedKey(coordinatorKey{deviceID: "D", channelID: "ch"}))

	empty := NewCoordinator(func(context.Context, Request) (*Result, error) { return nil, nil })
	report, err = empty.ClearDeviceBefore(context.Background(), "new-process", 2)
	require.NoError(t, err)
	require.Equal(t, DeviceCleanupNoTrackedEvidence, report.Status)
	require.True(t, report.NoTrackedEvidence)
}

func TestCoordinatorClearDeviceBeforeCallerCancellationJoinsOneBackgroundStop(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	finish := func() { releaseOnce.Do(func() { close(release) }) }
	defer finish()
	var calls atomic.Int32
	c := knownCleanupCoordinator(t, func(ctx context.Context, _ *Result) error {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}, func(Request) int64 { return 1 })
	ensureCleanupEntry(t, c, Request{DeviceID: "D", ChannelID: "ch"})

	callerCtx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	startedAt := time.Now()
	_, err := c.ClearDeviceBefore(callerCtx, "D", 2)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(startedAt), time.Second)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("stop did not start")
	}
	c.mu.Lock()
	job := c.cleanupJobs[deviceCleanupKey{deviceID: "D", targetEpoch: 2}]
	c.mu.Unlock()
	require.NotNil(t, job)
	secondCtx, secondCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer secondCancel()
	secondReport, err := c.ClearDeviceBefore(secondCtx, "D", 2)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, DeviceCleanupPending, secondReport.Status)
	finish()
	select {
	case <-job.done:
	case <-time.After(time.Second):
		t.Fatal("stop did not finish")
	}
	report := c.cleanupJobSnapshot(job)
	require.Equal(t, DeviceCleanupSettled, report.Status)
	require.Equal(t, int32(1), calls.Load())
}

func TestCoordinatorClearDeviceBeforeRetriesCloseFailureAndKeepsTerminalWarning(t *testing.T) {
	var calls atomic.Int32
	c := knownCleanupCoordinator(t, func(context.Context, *Result) error {
		if calls.Add(1) == 1 {
			return errors.New("close failed")
		}
		return completedMediaStop(errors.New("bye warning"))
	}, func(Request) int64 { return 1 })
	ensureCleanupEntry(t, c, Request{DeviceID: "D", ChannelID: "ch"})

	first, err := c.ClearDeviceBefore(context.Background(), "D", 2)
	require.NoError(t, err)
	require.Equal(t, DeviceCleanupPending, first.Status)
	require.Equal(t, 1, first.Pending)
	require.True(t, c.hasTrackedKey(coordinatorKey{deviceID: "D", channelID: "ch"}))

	second, err := c.ClearDeviceBefore(context.Background(), "D", 2)
	require.NoError(t, err)
	require.Equal(t, DeviceCleanupSettled, second.Status)
	require.Equal(t, 1, second.Settled)
	require.NotEmpty(t, second.Warnings)
	require.False(t, c.hasTrackedKey(coordinatorKey{deviceID: "D", channelID: "ch"}))
}

func TestCoordinatorClearDeviceBeforeRunsChannelsConcurrently(t *testing.T) {
	started := make(chan string, 2)
	deadlines := make(chan time.Time, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	finish := func() { releaseOnce.Do(func() { close(release) }) }
	defer finish()
	var calls atomic.Int32
	c := NewCoordinatorWithStop(func(context.Context, Request) (*Result, error) {
		return &Result{StreamID: "stream-" + t.Name(), SSRC: "ssrc", Generation: 1}, nil
	}, func(ctx context.Context, result *Result) error {
		calls.Add(1)
		deadline, ok := ctx.Deadline()
		if !ok {
			return errors.New("cleanup has no deadline")
		}
		deadlines <- deadline
		started <- result.StreamID
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	c.beginOperation = func(ctx context.Context, req Request) (playauth.DeviceOperationLease, error) {
		return &cleanupTestLease{ctx: ctx, epoch: 1}, nil
	}
	ensureCleanupEntry(t, c, Request{DeviceID: "D", ChannelID: "ch1"})
	ensureCleanupEntry(t, c, Request{DeviceID: "D", ChannelID: "ch2"})

	done := make(chan DeviceCleanupReport, 1)
	go func() {
		report, _ := c.ClearDeviceBefore(context.Background(), "D", 2)
		done <- report
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first channel was not stopped")
	}
	select {
	case <-started:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("second channel did not start while first was blocked")
	}
	require.Equal(t, int32(2), calls.Load())
	require.Equal(t, <-deadlines, <-deadlines, "all channels share one absolute device cleanup deadline")
	finish()
	select {
	case report := <-done:
		require.Equal(t, DeviceCleanupSettled, report.Status)
	case <-time.After(time.Second):
		t.Fatal("multi-channel cleanup did not finish")
	}
}

func TestCoordinatorClearDeviceBeforeDoesNotStopReplacementAfterSnapshot(t *testing.T) {
	var stops atomic.Int32
	c := knownCleanupCoordinator(t, func(context.Context, *Result) error {
		stops.Add(1)
		return nil
	}, func(Request) int64 { return 1 })
	ensureCleanupEntry(t, c, Request{DeviceID: "D", ChannelID: "ch"})
	candidates := c.snapshotDeviceCleanupCandidates("D")
	require.Len(t, candidates, 1)
	c.mu.Lock()
	newDone := make(chan struct{})
	close(newDone)
	c.entries[coordinatorKey{deviceID: "D", channelID: "ch"}] = &coordinatorEntry{
		state: LiveStateReady, result: &Result{StreamID: "new", SSRC: "new", Generation: 2},
		operationEpoch: 1, done: newDone,
	}
	c.mu.Unlock()
	result := c.clearDeviceCandidate(context.Background(), 2, candidates[0])
	require.Equal(t, int32(0), stops.Load())
	require.True(t, result.unknown)
	require.True(t, c.hasTrackedKey(coordinatorKey{deviceID: "D", channelID: "ch"}))
}

func (c *Coordinator) currentResultForKey(key coordinatorKey) (*Result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.entries[key]
	if entry == nil {
		return nil, false
	}
	return entry.result, true
}

func (c *Coordinator) hasTrackedKey(key coordinatorKey) bool {
	_, ok := c.currentResultForKey(key)
	return ok
}

func TestCoordinatorClearDeviceBeforeMissingStopIsNotTerminal(t *testing.T) {
	c := knownCleanupCoordinator(t, nil, func(Request) int64 { return 1 })
	ensureCleanupEntry(t, c, Request{DeviceID: "D", ChannelID: "ch"})
	report, err := c.ClearDeviceBefore(context.Background(), "D", 2)
	require.NoError(t, err)
	require.Equal(t, DeviceCleanupPending, report.Status)
	require.True(t, c.hasTrackedKey(coordinatorKey{deviceID: "D", channelID: "ch"}))
}

func TestCoordinatorClearDeviceBeforeRetainsStoppingUntilActualStopReturns(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	finish := func() { releaseOnce.Do(func() { close(release) }) }
	defer finish()
	var calls atomic.Int32
	c := knownCleanupCoordinator(t, func(context.Context, *Result) error {
		calls.Add(1)
		<-release // A transport that has not actually finished on context expiry.
		return nil
	}, func(Request) int64 { return 1 })
	ensureCleanupEntry(t, c, Request{DeviceID: "D", ChannelID: "ch"})
	candidates := c.snapshotDeviceCleanupCandidates("D")
	require.Len(t, candidates, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result := c.clearDeviceCandidate(ctx, 2, candidates[0])
	require.True(t, result.pending)
	c.mu.Lock()
	state, done := candidates[0].entry.state, candidates[0].entry.done
	c.mu.Unlock()
	require.Equal(t, LiveStateStopping, state)
	secondCtx, secondCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer secondCancel()
	report, err := c.ClearDeviceBefore(secondCtx, "D", 2)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, DeviceCleanupPending, report.Status)
	require.EqualValues(t, 1, calls.Load(), "late cleanup must not be dispatched twice")
	finish()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("actual stop result was not published")
	}
	require.False(t, c.hasTrackedKey(coordinatorKey{deviceID: "D", channelID: "ch"}))
}

func TestCoordinatorClearDeviceBeforeWaitsForStartingOwner(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	finish := func() { releaseOnce.Do(func() { close(release) }) }
	defer finish()
	var stops atomic.Int32
	var lease *cleanupTestLease
	c := NewCoordinatorWithStop(func(context.Context, Request) (*Result, error) {
		close(started)
		<-release
		return &Result{StreamID: "D-ch", SSRC: "ssrc", Generation: 1}, nil
	}, func(context.Context, *Result) error { stops.Add(1); return nil })
	c.beginOperation = func(ctx context.Context, _ Request) (playauth.DeviceOperationLease, error) {
		lease = &cleanupTestLease{ctx: ctx, epoch: 1}
		return lease, nil
	}
	ownerDone := make(chan error, 1)
	go func() {
		_, err := c.EnsureLive(context.Background(), Request{DeviceID: "D", ChannelID: "ch"})
		ownerDone <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("start did not begin")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	candidates := c.snapshotDeviceCleanupCandidates("D")
	require.Len(t, candidates, 1)
	require.True(t, c.clearDeviceCandidate(ctx, 2, candidates[0]).pending)
	require.Zero(t, stops.Load())
	require.Zero(t, lease.rel.Load())
	finish()
	select {
	case err := <-ownerDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("start did not finish")
	}
	require.EqualValues(t, 1, lease.rel.Load())
	report, err := c.ClearDeviceBefore(context.Background(), "D", 2)
	require.NoError(t, err)
	require.Equal(t, DeviceCleanupSettled, report.Status)
	require.EqualValues(t, 1, stops.Load())
}

func TestCoordinatorOperationLeaseMustHavePositiveEpoch(t *testing.T) {
	for _, epoch := range []int64{0, -1} {
		var starts atomic.Int32
		c := NewCoordinator(func(context.Context, Request) (*Result, error) {
			starts.Add(1)
			return &Result{StreamID: "must-not-start"}, nil
		})
		lease := &cleanupTestLease{ctx: context.Background(), epoch: epoch}
		c.beginOperation = func(context.Context, Request) (playauth.DeviceOperationLease, error) { return lease, nil }
		_, err := c.EnsureLive(context.Background(), Request{DeviceID: "D", ChannelID: "ch", DeviceEpoch: 99})
		require.ErrorIs(t, err, ErrPlayAuthorizationUnavailable)
		require.Zero(t, starts.Load())
		require.EqualValues(t, 1, lease.rel.Load())
	}
}
