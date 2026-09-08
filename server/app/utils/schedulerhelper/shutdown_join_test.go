package schedulerhelper

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/require"
)

func TestStopJoinsCronCallbackBeforeItEntersExecutor(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	s := NewJobScheduler(WithLoggerConfig(t.TempDir(), LevelError), WithCronOptions(cron.WithSeconds(), cron.WithChain(func(job cron.Job) cron.Job {
		return cron.FuncJob(func() {
			select {
			case <-entered:
			default:
				close(entered)
			}
			<-release
			job.Run()
		})
	})))
	// Schedule a real cron callback that has not reached executeJob/wg.Add.
	// The stopped callback must join even when it performs no business work.
	_, err := s.cron.AddFunc("@every 1s", func() {})
	require.NoError(t, err)
	s.Start()
	defer unblock()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("cron callback never entered")
	}
	done := make(chan struct{})
	go func() { s.Stop(); close(done) }()
	select {
	case <-done:
		t.Fatal("Stop returned before an already scheduled callback joined")
	case <-time.After(30 * time.Millisecond):
	}
	unblock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop failed to finish after callback exit")
	}
}

func TestStopJoinsManualJobReservedBeforeGoroutineStarts(t *testing.T) {
	s := NewJobScheduler(WithLoggerConfig(t.TempDir(), LevelError))
	executor := NewMockExecutor("manual-join")
	exited := make(chan struct{})
	executor.SetExecuteFn(func(context.Context, *Job) error { close(exited); return nil })
	s.RegisterExecutor(executor)
	job := &Job{Group: "test", Name: "manual", ExecutorName: executor.Name(), CronExpression: "@every 1h", Status: StatusDisabled}
	id, err := s.AddOrUpdateJob(job)
	require.NoError(t, err)
	// ExecuteNow can do its reads, but the spawned goroutine cannot pass
	// incrementRunningCount until this read lock is released.
	s.mu.RLock()
	var once sync.Once
	release := func() { once.Do(s.mu.RUnlock) }
	defer release()
	require.NoError(t, s.ExecuteNow(id))
	done := make(chan struct{})
	go func() { s.Stop(); close(done) }()
	select {
	case <-done:
		t.Fatal("Stop lost the accepted manual job before wg registration")
	case <-time.After(30 * time.Millisecond):
	}
	release()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop failed to join the manual executor")
	}
	select {
	case <-exited:
	default:
		t.Fatal("Stop returned before actual manual execution ended")
	}
}

func TestStopSealsManualAdmissionAndConcurrentStopJoinsOnce(t *testing.T) {
	s := NewJobScheduler(WithLoggerConfig(t.TempDir(), LevelError))
	executor := NewMockExecutor("stop-once")
	entered, release := make(chan struct{}), make(chan struct{})
	executor.SetExecuteFn(func(context.Context, *Job) error {
		close(entered)
		<-release
		return nil
	})
	s.RegisterExecutor(executor)
	id, err := s.AddOrUpdateJob(&Job{Group: "test", Name: "manual", ExecutorName: executor.Name(), CronExpression: "@every 1h", Status: StatusDisabled})
	require.NoError(t, err)
	require.NoError(t, s.ExecuteNow(id))
	<-entered
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	done := make(chan struct{}, 8)
	for i := 0; i < 8; i++ {
		go func() { s.Stop(); done <- struct{}{} }()
	}
	require.Eventually(t, func() bool {
		s.lifecycleMu.Lock()
		defer s.lifecycleMu.Unlock()
		return s.stopped
	}, time.Second, time.Millisecond)
	require.Error(t, s.ExecuteNow(id), "stopping rejects even while original executor is still alive")
	s.Start() // a stopped lifecycle must not revive cron or block on its owner
	select {
	case <-done:
		t.Fatal("Stop returned while the original executor was alive")
	default:
	}
	unblock()
	for i := 0; i < 8; i++ {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("concurrent Stop did not join")
		}
	}
	s.Stop()
	require.Error(t, s.ExecuteNow(id))
	require.EqualValues(t, 1, executor.GetExecCount())
	for range s.GetResults() {
	}
}
