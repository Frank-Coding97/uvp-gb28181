package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"

	"go.uber.org/zap"
)

// A result handler owns its consumer until the producer closes the channel.
// A shutdown deadline only bounds waiting; it does not cancel persistence.
type resultHandler struct {
	done   chan struct{}
	failed int
}

type resultDrainError struct{ failed int }

func (e resultDrainError) Error() string {
	return fmt.Sprintf("%d job results failed to persist", e.failed)
}

var resultHandlers struct {
	sync.Mutex
	current *resultHandler
	results <-chan *schedulerhelper.JobResult
}

func newResultHandler(results <-chan *schedulerhelper.JobResult, save func(context.Context, *schedulerhelper.JobResult) error, root *zap.Logger) *resultHandler {
	h := &resultHandler{done: make(chan struct{})}
	go func() {
		defer close(h.done)
		for result := range results {
			scope := logging.WithIdentity(root, zap.String("job_id", result.JobID), zap.String("execution_id", result.ExecutionID), zap.Int("attempt", result.Attempt))
			ctx := logging.WithContext(context.Background(), scope)
			if err := save(ctx, result); err != nil {
				h.failed++
				scope.Named("scheduler").Error("Job result persistence failed", zap.String("event", "scheduler.result.persist_failed"), logging.Error(err))
			}
		}
	}()
	return h
}

func (h *resultHandler) wait(ctx context.Context) error {
	if h == nil {
		return nil
	}
	// Completion wins over a deadline that has already expired.
	select {
	case <-h.done:
	default:
		select {
		case <-h.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if h.failed != 0 {
		return resultDrainError{failed: h.failed}
	}
	return nil
}

// StartResultHandler attaches one consumer to the scheduler's result channel.
func StartResultHandler() {
	if app.JobScheduler == nil {
		return
	}
	results := app.JobScheduler.GetResults()
	resultHandlers.Lock()
	defer resultHandlers.Unlock()
	if resultHandlers.current != nil && resultHandlers.results == results {
		return
	}
	if resultHandlers.current != nil {
		select {
		case <-resultHandlers.current.done:
		default:
			return
		}
	}
	resultHandlers.results = results
	resultHandlers.current = newResultHandler(results, saveJobResultContext, app.Log(context.Background()))
}

// StopResultHandlerContext waits after the scheduler has stopped all producers
// and closed results. Timeout never claims that queued results were saved.
func StopResultHandlerContext(ctx context.Context) error {
	resultHandlers.Lock()
	h := resultHandlers.current
	resultHandlers.Unlock()
	return h.wait(ctx)
}

func StopResultHandler() {
	if err := StopResultHandlerContext(context.Background()); err != nil {
		app.Log(context.Background()).Named("scheduler").Error("Job result drain failed", zap.String("event", "scheduler.result.drain_failed"), logging.Error(err))
	}
}

// saveJobResultContext 将任务执行结果保存到数据库
func saveJobResultContext(ctx context.Context, result *schedulerhelper.JobResult) error {
	// 转换 JobResult 为 SysJobResults 模型
	jobResult := &models.SysJobResults{
		JobId:      result.JobID,
		Status:     result.Status,
		StartTime:  &result.StartTime,
		EndTime:    &result.EndTime,
		Duration:   result.Duration.Nanoseconds(), // 转换为纳秒
		RetryCount: result.RetryCount,
	}

	// 处理错误信息
	if result.Error != nil {
		jobResult.Error = result.Error.Error()
	}

	// 设置创建时间
	now := time.Now()
	jobResult.CreatedAt = &now

	// 保存到数据库
	if err := jobResult.Create(ctx); err != nil {
		return fmt.Errorf("保存任务结果到数据库失败: %w", err)
	}

	app.Log(ctx).Named("scheduler").Debug("任务结果已保存到数据库", zap.String("event", "scheduler.result.persisted"),
		zap.String("job_id", result.JobID),
		zap.String("status", result.Status),
		zap.Duration("duration", result.Duration),
		zap.Int("retry_count", result.RetryCount))

	// 如果是单次执行策略且执行成功，更新 sys_jobs 表的 status 为 0
	if result.ExecutionPolicy == schedulerhelper.PolicyOnce && result.Status == "SUCCESS" {
		job := &models.SysJobs{}
		if err := job.GetByID(ctx, result.JobID); err != nil {
			app.Log(ctx).Named("scheduler").Error("获取任务信息失败", zap.String("event", "scheduler.once.lookup_failed"),
				zap.String("job_id", result.JobID),
				logging.Error(err))
		} else {
			if job.Status == 1 {
				job.Status = 0
				if err := job.Update(ctx); err != nil {
					app.Log(ctx).Named("scheduler").Error("更新单次执行任务状态失败", zap.String("event", "scheduler.once.disable_failed"),
						zap.String("job_id", result.JobID),
						logging.Error(err))
				} else {
					app.Log(ctx).Named("scheduler").Info("单次执行任务已完成，已更新数据库状态为禁用", zap.String("event", "scheduler.once.disabled"),
						zap.String("job_id", result.JobID))
				}
			}

		}
	}

	return nil
}
