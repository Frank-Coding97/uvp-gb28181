package schedulerhelper

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type shutdownTestLogger struct {
	closeCount int32
}

func (*shutdownTestLogger) Debug(string, string, ...interface{}) {}
func (*shutdownTestLogger) Info(string, string, ...interface{})  {}
func (*shutdownTestLogger) Warn(string, string, ...interface{})  {}
func (*shutdownTestLogger) Error(string, string, ...interface{}) {}
func (*shutdownTestLogger) Fatal(string, string, ...interface{}) {}
func (*shutdownTestLogger) LogJobExecution(*JobResult)           {}
func (*shutdownTestLogger) LogJobLifecycle(*Job, string)         {}
func (l *shutdownTestLogger) Close() error                       { atomic.AddInt32(&l.closeCount, 1); return nil }

type shutdownTestExecutor struct {
	name      string
	started   chan<- struct{}
	release   <-chan struct{}
	ignoreCtx bool
	callCount int32
}

func (e *shutdownTestExecutor) Name() string { return e.name }

func (e *shutdownTestExecutor) Execute(ctx context.Context, _ *Job) error {
	atomic.AddInt32(&e.callCount, 1)
	if e.started != nil {
		e.started <- struct{}{}
	}
	if e.release == nil {
		return nil
	}
	if e.ignoreCtx {
		<-e.release
		return nil
	}
	select {
	case <-e.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func newShutdownTestScheduler(t *testing.T, executor Executor, buffer int) *JobScheduler {
	t.Helper()
	logger := &shutdownTestLogger{}
	scheduler := NewJobScheduler(WithLogger(logger), WithJobResultsBufferSize(buffer))
	scheduler.RegisterExecutor(executor)
	job := &Job{
		ID:             "shutdown-test-job",
		Group:          "shutdown-test",
		Name:           "shutdown test",
		ExecutorName:   executor.Name(),
		CronExpression: "0 0 0 1 1 *",
		Status:         StatusDisabled,
		Timeout:        time.Minute,
		BlockingPolicy: BlockParallel,
		ParallelNum:    64,
	}
	if _, err := scheduler.AddOrUpdateJob(job); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { scheduler.Stop() })
	return scheduler
}

func waitForShutdownTestSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for accepted job")
	}
}

func TestShutdownWaitsForAcceptedExecuteNow(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	executor := &shutdownTestExecutor{name: "shutdown-wait", started: started, release: release}
	scheduler := newShutdownTestScheduler(t, executor, 2)

	if err := scheduler.ExecuteNow("shutdown-test-job"); err != nil {
		t.Fatal(err)
	}
	waitForShutdownTestSignal(t, started)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	err := scheduler.Shutdown(ctx)
	cancel()
	if err == nil {
		t.Fatal("Shutdown returned before accepted job finished")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v, want context deadline", err)
	}

	close(release)
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	if err := scheduler.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()

	result, ok := <-scheduler.GetResults()
	if !ok || result == nil || result.Status != "SUCCESS" {
		t.Fatalf("shutdown result = %#v, open = %v", result, ok)
	}
	if _, ok := <-scheduler.GetResults(); ok {
		t.Fatal("results channel remained open after Shutdown")
	}
	if err := scheduler.ExecuteNow("shutdown-test-job"); err == nil {
		t.Fatal("ExecuteNow succeeded after Shutdown")
	}
}

func TestShutdownWaitsForAcceptedCronExecution(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	executor := &shutdownTestExecutor{name: "shutdown-cron", started: started, release: release}
	scheduler := newShutdownTestScheduler(t, executor, 2)

	scheduler.mu.Lock()
	job := scheduler.jobs["shutdown-test-job"]
	job.CronExpression = "@every 10ms"
	job.BlockingPolicy = BlockDiscard
	scheduler.mu.Unlock()
	if err := scheduler.EnableJob(job.ID); err != nil {
		t.Fatal(err)
	}
	scheduler.Start()
	waitForShutdownTestSignal(t, started)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	err := scheduler.Shutdown(ctx)
	cancel()
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v, want context deadline", err)
	}
	close(release)
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestShutdownRejectsRacingExecuteNowAndClosesOnce(t *testing.T) {
	executor := &shutdownTestExecutor{name: "shutdown-race"}
	const attempts = 64
	scheduler := newShutdownTestScheduler(t, executor, attempts)

	start := make(chan struct{})
	var calls sync.WaitGroup
	var accepted int32
	for i := 0; i < attempts; i++ {
		calls.Add(1)
		go func() {
			defer calls.Done()
			<-start
			if err := scheduler.ExecuteNow("shutdown-test-job"); err == nil {
				atomic.AddInt32(&accepted, 1)
			}
		}()
	}
	shutdownResult := make(chan error, 1)
	go func() {
		<-start
		shutdownResult <- scheduler.Shutdown(context.Background())
	}()
	close(start)
	calls.Wait()
	if err := <-shutdownResult; err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	var results int
	for result := range scheduler.GetResults() {
		if result == nil {
			t.Fatal("received nil job result")
		}
		results++
	}
	if results != int(atomic.LoadInt32(&accepted)) {
		t.Fatalf("results = %d, accepted ExecuteNow calls = %d", results, accepted)
	}
	if err := scheduler.ExecuteNow("shutdown-test-job"); err == nil {
		t.Fatal("ExecuteNow succeeded after repeated Shutdown")
	}
}

func TestShutdownDeadlineDoesNotPretendCompletion(t *testing.T) {
	release := make(chan struct{})
	executor := &shutdownTestExecutor{name: "shutdown-deadline", release: release, ignoreCtx: true}
	scheduler := newShutdownTestScheduler(t, executor, 1)
	if err := scheduler.ExecuteNow("shutdown-test-job"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	err := scheduler.Shutdown(ctx)
	cancel()
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v, want context deadline", err)
	}
	close(release)
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestShutdownDoesNotDropResultsWhenConsumerIsBehind(t *testing.T) {
	executor := &shutdownTestExecutor{name: "shutdown-result-backpressure"}
	scheduler := newShutdownTestScheduler(t, executor, 1)
	if err := scheduler.ExecuteNow("shutdown-test-job"); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.ExecuteNow("shutdown-test-job"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	err := scheduler.Shutdown(ctx)
	cancel()
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error = %v, want context deadline while result consumer is behind", err)
	}
	for result := range scheduler.GetResults() {
		if result == nil {
			t.Fatal("received nil job result")
		}
	}
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}
