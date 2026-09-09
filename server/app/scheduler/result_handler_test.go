package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"
)

func TestLoggingResultDrainWaitsForEverySave(t *testing.T) {
	results := make(chan *schedulerhelper.JobResult, 8)
	for i := 0; i < 8; i++ {
		results <- &schedulerhelper.JobResult{JobID: fmt.Sprint(i)}
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var mu sync.Mutex
	saved := map[string]int{}
	h := newResultHandler(results, func(ctx context.Context, r *schedulerhelper.JobResult) error {
		if r.JobID == "0" {
			close(entered)
			<-release
		}
		require.NoError(t, ctx.Err())
		mu.Lock()
		saved[r.JobID]++
		mu.Unlock()
		return nil
	}, zap.NewNop())
	<-entered
	close(results)
	waited := make(chan error, 1)
	go func() { waited <- h.wait(context.Background()) }()
	select {
	case err := <-waited:
		t.Fatalf("returned before save completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-waited)
	require.Len(t, saved, 8)
	for _, n := range saved {
		require.Equal(t, 1, n)
	}
	require.NoError(t, h.wait(context.Background()))
}

func TestLoggingResultDrainWaitsForLateProducer(t *testing.T) {
	results := make(chan *schedulerhelper.JobResult)
	saved := make(chan struct{}, 1)
	h := newResultHandler(results, func(context.Context, *schedulerhelper.JobResult) error { saved <- struct{}{}; return nil }, zap.NewNop())
	waited := make(chan error, 1)
	go func() { waited <- h.wait(context.Background()) }()
	select {
	case <-waited:
		t.Fatal("empty channel is not producer completion")
	case <-time.After(20 * time.Millisecond):
	}
	results <- &schedulerhelper.JobResult{JobID: "late"}
	close(results)
	require.NoError(t, <-waited)
	<-saved
}

func TestLoggingResultDrainTimeoutDoesNotCancelPersistence(t *testing.T) {
	results := make(chan *schedulerhelper.JobResult, 1)
	results <- &schedulerhelper.JobResult{JobID: "slow"}
	close(results)
	entered, release := make(chan struct{}), make(chan struct{})
	savedCtx := make(chan context.Context, 1)
	h := newResultHandler(results, func(ctx context.Context, _ *schedulerhelper.JobResult) error {
		close(entered)
		<-release
		savedCtx <- ctx
		return nil
	}, zap.NewNop())
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, h.wait(ctx), context.DeadlineExceeded)
	close(release)
	require.NoError(t, h.wait(context.Background()))
	require.NoError(t, (<-savedCtx).Err())
}

func TestLoggingResultDrainFailureVisibleAndScopeRetained(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	results := make(chan *schedulerhelper.JobResult, 2)
	results <- &schedulerhelper.JobResult{JobID: "job-1", ExecutionID: "exec-1", Attempt: 2}
	results <- &schedulerhelper.JobResult{JobID: "job-2", ExecutionID: "exec-2", Attempt: 1}
	close(results)
	var count int
	h := newResultHandler(results, func(ctx context.Context, r *schedulerhelper.JobResult) error {
		logging.FromContext(ctx, nil).Named("db").Debug("Fake save", zap.String("event", "test.save"))
		count++
		if r.JobID == "job-1" {
			return errors.New("secret must not be logged")
		}
		return nil
	}, zap.New(core))
	err := h.wait(context.Background())
	require.EqualError(t, err, "1 job results failed to persist")
	require.Equal(t, 2, count)
	saved := logs.FilterMessage("Fake save").All()
	require.Len(t, saved, 2)
	require.Equal(t, "job-1", saved[0].ContextMap()["job_id"])
	require.Equal(t, "exec-1", saved[0].ContextMap()["execution_id"])
	require.Equal(t, int64(2), saved[0].ContextMap()["attempt"])
	failed := logs.FilterMessage("Job result persistence failed").All()
	require.Len(t, failed, 1)
	require.NotContains(t, fmt.Sprint(failed[0].ContextMap()), "secret must not be logged")
}

func TestLoggingResultDrainConcurrentWait(t *testing.T) {
	for round := 0; round < 100; round++ {
		results := make(chan *schedulerhelper.JobResult, 3)
		for i := 0; i < 3; i++ {
			results <- &schedulerhelper.JobResult{}
		}
		var saved int
		h := newResultHandler(results, func(context.Context, *schedulerhelper.JobResult) error { saved++; return nil }, zap.NewNop())
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := h.wait(context.Background()); err != nil {
					t.Error(err)
				}
			}()
		}
		close(results)
		wg.Wait()
		require.Equal(t, 3, saved)
	}
}

type loggingDrainExecutor struct{}

func (loggingDrainExecutor) Name() string                                        { return "drain-noop" }
func (loggingDrainExecutor) Execute(context.Context, *schedulerhelper.Job) error { return nil }

func TestLoggingResultDrainSchedulerBackpressure(t *testing.T) {
	jobs := schedulerhelper.NewJobScheduler(schedulerhelper.WithLogger(schedulerhelper.NewZapJobLogger(zap.NewNop())), schedulerhelper.WithJobResultsBufferSize(1))
	jobs.RegisterExecutor(loggingDrainExecutor{})
	_, err := jobs.AddOrUpdateJob(&schedulerhelper.Job{ID: "drain-job", Name: "drain", Group: "test", ExecutorName: "drain-noop", Status: schedulerhelper.StatusDisabled, CronExpression: "0 0 1 * * *", ExecutionPolicy: schedulerhelper.PolicyRepeat, BlockingPolicy: schedulerhelper.BlockParallel, ParallelNum: 32, Timeout: time.Second})
	require.NoError(t, err)
	entered, release := make(chan struct{}), make(chan struct{})
	saved := map[string]bool{}
	consumer := newResultHandler(jobs.GetResults(), func(_ context.Context, r *schedulerhelper.JobResult) error {
		if len(saved) == 0 {
			close(entered)
			<-release
		}
		if saved[r.ExecutionID] {
			t.Error("duplicate result", r.ExecutionID)
		}
		saved[r.ExecutionID] = true
		return nil
	}, zap.NewNop())
	for i := 0; i < 20; i++ {
		require.NoError(t, jobs.ExecuteNow("drain-job"))
	}
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, jobs.StopContext(ctx), context.DeadlineExceeded)
	close(release)
	require.NoError(t, jobs.StopContext(context.Background()))
	require.NoError(t, consumer.wait(context.Background()))
	require.Len(t, saved, 20)
	require.Error(t, jobs.ExecuteNow("drain-job"))
}
