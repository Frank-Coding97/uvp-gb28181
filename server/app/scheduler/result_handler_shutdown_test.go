package scheduler

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"
)

type shutdownResultExecutor struct {
	started chan<- struct{}
	release <-chan struct{}
}

func (e *shutdownResultExecutor) Name() string { return "shutdown-result" }

func (e *shutdownResultExecutor) Execute(ctx context.Context, _ *schedulerhelper.Job) error {
	if e.started != nil {
		e.started <- struct{}{}
	}
	if e.release == nil {
		return nil
	}
	select {
	case <-e.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func newResultHandlerTestScheduler(t *testing.T) *schedulerhelper.JobScheduler {
	t.Helper()
	logger, err := schedulerhelper.NewFileJobLogger(filepath.Join(t.TempDir(), "scheduler-logs"), schedulerhelper.LevelError)
	if err != nil {
		t.Fatal(err)
	}
	return schedulerhelper.NewJobScheduler(
		schedulerhelper.WithLogger(logger),
		schedulerhelper.WithJobResultsBufferSize(8),
	)
}

func addResultHandlerTestJob(t *testing.T, scheduler *schedulerhelper.JobScheduler, executor schedulerhelper.Executor) {
	t.Helper()
	scheduler.RegisterExecutor(executor)
	_, err := scheduler.AddOrUpdateJob(&schedulerhelper.Job{
		ID:              "shutdown-result-job",
		Group:           "shutdown-result",
		Name:            "shutdown result",
		ExecutorName:    executor.Name(),
		ExecutionPolicy: schedulerhelper.PolicyRepeat,
		CronExpression:  "0 0 0 1 1 *",
		Status:          schedulerhelper.StatusDisabled,
		Timeout:         time.Minute,
		BlockingPolicy:  schedulerhelper.BlockParallel,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func persistResultHandlerTestJob(t *testing.T, db *gorm.DB, executor schedulerhelper.Executor) {
	t.Helper()
	// This helper is intentionally kept separate from scheduler registration:
	// sys_job_results has a foreign key to the persisted sys_jobs row.
	now := time.Now()
	result := db.Create(&models.SysJobs{
		Id:              "shutdown-result-job",
		Group:           "shutdown-result",
		Name:            "shutdown result",
		ExecutorName:    executor.Name(),
		ExecutionPolicy: int(schedulerhelper.PolicyRepeat),
		Status:          int(schedulerhelper.StatusDisabled),
		CronExpression:  "0 0 0 1 1 *",
		Parameters:      "{}",
		BlockingPolicy:  int(schedulerhelper.BlockParallel),
		Timeout:         int64(time.Minute),
		CreatedAt:       &now,
		UpdatedAt:       &now,
	})
	if result.Error != nil {
		t.Fatal(result.Error)
	}
}

func withResultHandlerGlobals(t *testing.T, scheduler *schedulerhelper.JobScheduler) {
	t.Helper()
	oldScheduler, oldLog := app.JobScheduler, app.ZapLog
	app.JobScheduler = scheduler
	app.ZapLog = zap.NewNop()
	t.Cleanup(func() {
		StopResultHandler()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = WaitResultHandler(ctx)
		cancel()
		_ = scheduler.Shutdown(context.Background())
		app.JobScheduler, app.ZapLog = oldScheduler, oldLog
	})
}

func TestWaitResultHandlerDrainsClosedSchedulerResults(t *testing.T) {
	db := newSQLiteSchedulerTestDB(t)
	scheduler := newResultHandlerTestScheduler(t)
	withResultHandlerGlobals(t, scheduler)
	executor := &shutdownResultExecutor{}
	addResultHandlerTestJob(t, scheduler, executor)
	persistResultHandlerTestJob(t, db, executor)

	StartResultHandler()
	if err := scheduler.ExecuteNow("shutdown-result-job"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	if err := scheduler.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := WaitResultHandler(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()

	var count int64
	if err := db.Model(&struct{ JobID string }{}).Table("sys_job_results").Where("job_id = ?", "shutdown-result-job").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("persisted result count = %d, want 1", count)
	}
}

func TestWaitResultHandlerReportsSaveFailure(t *testing.T) {
	db := newSQLiteSchedulerTestDB(t)
	scheduler := newResultHandlerTestScheduler(t)
	withResultHandlerGlobals(t, scheduler)
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	executor := &shutdownResultExecutor{started: started, release: release}
	addResultHandlerTestJob(t, scheduler, executor)
	persistResultHandlerTestJob(t, db, executor)

	StartResultHandler()
	if err := scheduler.ExecuteNow("shutdown-result-job"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("executor did not start")
	}
	raw, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	if err := scheduler.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	err = WaitResultHandler(ctx)
	cancel()
	if err == nil {
		t.Fatal("WaitResultHandler returned success after database save failure")
	}
}

func TestStopResultHandlerDoesNotClaimDrainCompletion(t *testing.T) {
	scheduler := newResultHandlerTestScheduler(t)
	withResultHandlerGlobals(t, scheduler)

	StartResultHandler()
	StopResultHandler()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	err := WaitResultHandler(ctx)
	cancel()
	if !errors.Is(err, ErrResultHandlerStopped) {
		t.Fatalf("WaitResultHandler error = %v, want ErrResultHandlerStopped", err)
	}
}
