package media

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"uvplatform.cn/uvp-gb28181/app/gb28181/zlm"
	"uvplatform.cn/uvp-gb28181/app/openapi/models"
)

type deadlineRevocationFactory func(context.Context, string) (RevocationRuntime, error)

func (f deadlineRevocationFactory) Resolve(ctx context.Context, node string) (RevocationRuntime, error) {
	return f(ctx, node)
}

type deadlineRevocationControl struct {
	RevocationMediaControl
	after func(context.Context, string)
}

func (c deadlineRevocationControl) GetRuntimeMediaPlayers(ctx context.Context, target zlm.StreamTarget) (zlm.RuntimePlayers, error) {
	result, err := c.RevocationMediaControl.GetRuntimeMediaPlayers(ctx, target)
	c.after(ctx, "players")
	return result, err
}

func (c deadlineRevocationControl) GetRuntimeSessions(ctx context.Context) (zlm.RuntimeSessions, error) {
	result, err := c.RevocationMediaControl.GetRuntimeSessions(ctx)
	c.after(ctx, "sessions")
	return result, err
}

func (c deadlineRevocationControl) KickSessionIfMatch(ctx context.Context, boot, id string) (zlm.ConditionalKickResult, error) {
	result, err := c.RevocationMediaControl.KickSessionIfMatch(ctx, boot, id)
	c.after(ctx, "kick")
	return result, err
}

func TestRevocationWorkerSharesTotalNetworkDeadline(t *testing.T) {
	for _, budget := range []time.Duration{maximumHookBudget, 100 * time.Millisecond} {
		t.Run(budget.String(), func(t *testing.T) {
			f := newRevocationWorkerFixture(t, budget)
			viewer := f.seed(t, 1, models.ViewerStateRevokePending)
			f.setSnapshot([]string{viewer.Identifier}, []string{viewer.Identifier})
			var resolveDeadline time.Time
			deadlines := make(map[string]time.Time)
			worker := f.worker(1)
			worker.factory = deadlineRevocationFactory(func(ctx context.Context, node string) (RevocationRuntime, error) {
				var ok bool
				resolveDeadline, ok = ctx.Deadline()
				require.True(t, ok)
				runtime, err := f.factory.Resolve(ctx, node)
				runtime.Control = deadlineRevocationControl{runtime.Control, func(ctx context.Context, stage string) {
					deadline, ok := ctx.Deadline()
					require.True(t, ok)
					deadlines[stage] = deadline
				}}
				return runtime, err
			})
			result, err := worker.Tick(context.Background())
			require.NoError(t, err)
			require.Equal(t, 1, result.Pending)
			require.Len(t, deadlines, 3)
			for stage, deadline := range deadlines {
				require.False(t, deadline.After(resolveDeadline), "%s must not reset the resolver budget", stage)
				require.Equal(t, deadlines["players"], deadline)
			}
			if budget < maximumHookBudget {
				require.True(t, deadlines["players"].Before(resolveDeadline))
			}
		})
	}
}

func TestRevocationWorkerRejectsSuccessfulResultsAfterDeadline(t *testing.T) {
	for _, stage := range []string{"players", "sessions", "kick"} {
		t.Run(stage, func(t *testing.T) {
			f := newRevocationWorkerFixture(t, 10*time.Millisecond)
			viewer := f.seed(t, 1, models.ViewerStateRevokePending)
			f.setSnapshot([]string{viewer.Identifier}, []string{viewer.Identifier})
			f.factory.runtime.Control = deadlineRevocationControl{f.control, func(ctx context.Context, current string) {
				if current == stage {
					// Deliberately return a successful response despite expired context.
					<-ctx.Done()
				}
			}}
			result, err := f.worker(1).Tick(context.Background())
			require.NoError(t, err, "pending must be persisted with the live outer context")
			require.Equal(t, 1, result.Pending)
			row := f.loadViewer(t, viewer.ID)
			require.Equal(t, models.ViewerStateRevokePending, row.State)
			require.Equal(t, RevocationErrorNetworkCanceled, row.LastErrorClass)
			_, sessions := f.control.calls()
			if stage == "players" {
				require.Zero(t, sessions)
			}
			if stage != "kick" {
				require.Zero(t, f.control.kickCount())
			}
		})
	}
}

func TestRevocationWorkerDoesNotContinueAfterOuterCancellation(t *testing.T) {
	f := newRevocationWorkerFixture(t, maximumHookBudget)
	viewer := f.seed(t, 1, models.ViewerStateRevokePending)
	f.setSnapshot([]string{viewer.Identifier}, []string{viewer.Identifier})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.control.onPlayers = func(int) { cancel() }
	_, err := f.worker(1).Tick(ctx)
	require.ErrorIs(t, err, context.Canceled)
	_, sessions := f.control.calls()
	require.Zero(t, sessions)
	require.Zero(t, f.control.kickCount())
	require.Equal(t, models.ViewerStateRevokePending, f.loadViewer(t, viewer.ID).State)
}

func TestRevocationWorkerExpiredAbsenceCannotClose(t *testing.T) {
	f := newRevocationWorkerFixture(t, 10*time.Millisecond)
	viewer := f.seed(t, 1, models.ViewerStateRevokePending)
	// Preserve the fixture's microsecond timestamp; GORM's automatic wall
	// clock update would introduce nanoseconds absent from production SQL.
	require.NoError(t, f.db.Model(&viewer).UpdateColumn("last_error_class", RevocationErrorShutdownScheduled).Error)
	f.factory.runtime.Control = deadlineRevocationControl{f.control, func(ctx context.Context, stage string) {
		if stage == "sessions" {
			<-ctx.Done()
		}
	}}
	result, err := f.worker(1).Tick(context.Background())
	require.NoError(t, err)
	require.Zero(t, result.Closed)
	require.Equal(t, 1, result.Pending)
	row := f.loadViewer(t, viewer.ID)
	require.Equal(t, models.ViewerStateRevokePending, row.State)
	require.Equal(t, RevocationErrorNetworkCanceled, row.LastErrorClass)
}

func TestRevocationWorkerLateResolverCannotDispatchControl(t *testing.T) {
	f := newRevocationWorkerFixture(t, maximumHookBudget)
	viewer := f.seed(t, 1, models.ViewerStateRevokePending)
	worker := f.worker(1)
	var resolveDeadline time.Time
	worker.factory = deadlineRevocationFactory(func(ctx context.Context, node string) (RevocationRuntime, error) {
		resolveDeadline, _ = ctx.Deadline()
		<-ctx.Done()
		return f.factory.runtime, nil // Non-cooperative resolver returns late success.
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	deadline, _ := ctx.Deadline()
	_, err := worker.Tick(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, deadline, resolveDeadline, "inherit the caller's earlier deadline")
	players, sessions := f.control.calls()
	require.Zero(t, players)
	require.Zero(t, sessions)
	require.Zero(t, f.control.kickCount())
	require.Equal(t, models.ViewerStateRevokePending, f.loadViewer(t, viewer.ID).State)
}

func TestRevocationWorkerExpiredNonCooperativeCallCannotOverwriteReclaim(t *testing.T) {
	f := newRevocationWorkerFixture(t, 10*time.Millisecond)
	viewer := f.seed(t, 1, models.ViewerStateRevokePending)
	f.setSnapshot([]string{viewer.Identifier}, []string{viewer.Identifier})
	expired := make(chan struct{})
	release := make(chan struct{})
	worker := f.worker(1)
	worker.factory = deadlineRevocationFactory(func(ctx context.Context, node string) (RevocationRuntime, error) {
		runtime, err := f.factory.Resolve(ctx, node)
		runtime.Control = deadlineRevocationControl{runtime.Control, func(ctx context.Context, stage string) {
			if stage == "players" {
				<-ctx.Done()
				close(expired)
				<-release // Context cannot force a non-cooperative adapter to return.
			}
		}}
		return runtime, err
	})
	type tickResult struct {
		result RevocationTickResult
		err    error
	}
	done := make(chan tickResult, 1)
	joined := false
	go func() {
		result, err := worker.Tick(context.Background())
		done <- tickResult{result, err}
	}()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
		if !joined {
			<-done
		}
	}()
	select {
	case <-expired:
	case <-time.After(time.Second):
		t.Fatal("first control call never reached its real deadline")
	}
	select {
	case <-done:
		joined = true
		t.Fatal("worker detached a still-running control call")
	default:
	}
	// Advance only the second worker's persisted lease clock; real context
	// expiration above is independent of this injected timestamp.
	later := f.clock.Add(defaultRevocationLease + time.Second)
	second := NewRevocationWorker(f.db, f.factory, func() time.Time { return later }, 1)
	result, err := second.Tick(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, result.Claimed)
	newer := f.loadViewer(t, viewer.ID)
	require.Equal(t, 2, newer.Attempts)
	require.Equal(t, RevocationErrorShutdownScheduled, newer.LastErrorClass)
	close(release)
	first := <-done
	joined = true
	require.NoError(t, first.err)
	require.Equal(t, 1, first.result.Stale)
	require.Zero(t, first.result.Closed)
	require.Equal(t, newer, f.loadViewer(t, viewer.ID))
	players, sessions := f.control.calls()
	require.Equal(t, 2, players)
	require.Equal(t, 1, sessions, "expired first call must not continue")
	require.Equal(t, 1, f.control.kickCount())
}
