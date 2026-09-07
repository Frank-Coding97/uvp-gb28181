package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"

	"go.uber.org/zap"
)

// resultHandlerMu protects the current handler lifecycle state.
var (
	resultHandlerMu      sync.Mutex
	currentResultHandler *resultHandlerState
)

// ErrResultHandlerStopped means the compatibility stop path returned before
// the producer closed its result channel, so drain completion is unknown.
var ErrResultHandlerStopped = errors.New("result handler stopped before result channel closed")

type resultHandlerState struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

// StartResultHandler 启动任务结果处理器
// 该函数会启动一个后台协程，从调度器的结果通道中读取任务执行结果，
// 并将结果保存到数据库的 sys_job_results 表中
func StartResultHandler() {
	ctx, cancel := context.WithCancel(context.Background())
	state := &resultHandlerState{cancel: cancel, done: make(chan struct{})}
	resultHandlerMu.Lock()
	previous := currentResultHandler
	currentResultHandler = state
	resultHandlerMu.Unlock()
	if previous != nil {
		previous.cancel()
	}

	go func() {
		defer close(state.done)
		resultLogger().Info("任务结果处理器已启动")
		defer resultLogger().Info("任务结果处理器已停止")

		if app.JobScheduler == nil {
			state.err = errors.New("result handler scheduler is nil")
			return
		}
		resultsChan := app.JobScheduler.GetResults()

		for {
			select {
			case <-ctx.Done():
				resultLogger().Info("正在处理剩余的任务结果...")
				err, closed := drainResults(resultsChan)
				if err != nil {
					state.err = err
				}
				if !closed {
					if state.err == nil {
						state.err = ErrResultHandlerStopped
					}
				}
				return

			case result, ok := <-resultsChan:
				if !ok {
					resultLogger().Info("任务结果通道已关闭，处理器退出")
					return
				}

				// 保存结果到数据库
				if err := saveJobResult(result); err != nil {
					state.err = firstResultHandlerError(state.err, err)
					resultLogger().Error("保存任务结果失败",
						zap.String("jobID", result.JobID),
						zap.String("status", result.Status),
						zap.Error(err))
				}
			}
		}
	}()
}

// saveJobResult 将任务执行结果保存到数据库
func saveJobResult(result *schedulerhelper.JobResult) error {
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
	ctx := context.Background()
	if err := jobResult.Create(ctx); err != nil {
		return fmt.Errorf("保存任务结果到数据库失败: %w", err)
	}

	resultLogger().Debug("任务结果已保存到数据库",
		zap.String("jobID", result.JobID),
		zap.String("status", result.Status),
		zap.Duration("duration", result.Duration),
		zap.Int("retryCount", result.RetryCount))

	// 如果是单次执行策略且执行成功，更新 sys_jobs 表的 status 为 0
	if result.ExecutionPolicy == schedulerhelper.PolicyOnce && result.Status == "SUCCESS" {
		job := &models.SysJobs{}
		if err := job.GetByID(ctx, result.JobID); err != nil {
			resultLogger().Error("获取任务信息失败",
				zap.String("jobID", result.JobID),
				zap.Error(err))
			return fmt.Errorf("获取任务信息失败: %w", err)
		} else {
			if job.Status == 1 {
				job.Status = 0
				if err := job.Update(ctx); err != nil {
					resultLogger().Error("更新单次执行任务状态失败",
						zap.String("jobID", result.JobID),
						zap.Error(err))
					return fmt.Errorf("更新单次执行任务状态失败: %w", err)
				} else {
					resultLogger().Info("单次执行任务已完成，已更新数据库状态为禁用",
						zap.String("jobID", result.JobID))
				}
			}

		}
	}

	return nil
}

// StopResultHandler 停止任务结果处理器
// 这是兼容旧调用方的取消操作，不等待生产者关闭通道，也不宣称结果已排空。
func StopResultHandler() {
	resultHandlerMu.Lock()
	state := currentResultHandler
	resultHandlerMu.Unlock()
	if state != nil {
		resultLogger().Info("正在停止任务结果处理器...")
		state.cancel()
	}
}

// WaitResultHandler waits for the result handler to observe the producer's
// closed channel and finish saving every result it received. It returns a
// persistence error, or ErrResultHandlerStopped when the compatibility stop
// path ended before channel closure made draining provable.
func WaitResultHandler(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	resultHandlerMu.Lock()
	state := currentResultHandler
	resultHandlerMu.Unlock()
	if state == nil {
		return nil
	}
	select {
	case <-state.done:
		return state.err
	default:
	}
	select {
	case <-state.done:
		return state.err
	case <-ctx.Done():
		return fmt.Errorf("wait result handler: %w", ctx.Err())
	}
}

// drainResults 处理结果通道中剩余的所有结果
func drainResults(resultsChan <-chan *schedulerhelper.JobResult) (error, bool) {
	count := 0
	var firstErr error
	for {
		select {
		case result, ok := <-resultsChan:
			if !ok {
				resultLogger().Info("所有剩余任务结果已处理完成", zap.Int("count", count))
				return firstErr, true
			}
			count++
			if err := saveJobResult(result); err != nil {
				firstErr = firstResultHandlerError(firstErr, err)
				resultLogger().Error("保存剩余任务结果失败",
					zap.String("jobID", result.JobID),
					zap.Error(err))
			}
		default:
			if count > 0 {
				resultLogger().Info("所有剩余任务结果已处理完成", zap.Int("count", count))
			}
			return firstErr, false
		}
	}
}

func firstResultHandlerError(first, next error) error {
	if first != nil {
		return first
	}
	return next
}

func resultLogger() *zap.Logger {
	if app.ZapLog != nil {
		return app.ZapLog
	}
	return zap.NewNop()
}
