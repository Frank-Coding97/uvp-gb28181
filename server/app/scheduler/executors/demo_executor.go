package executors

import (
	"context"
	"time"

	"go.uber.org/zap"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"
)

// DemoExecutor 演示执行器，用于测试和演示调度器功能
type DemoExecutor struct{}

// Execute 执行任务
func (e *DemoExecutor) Execute(ctx context.Context, job *schedulerhelper.Job) error {
	logger := app.Log(ctx).Named("scheduler.demo")
	logger.Info("Demo job started", zap.String("event", "scheduler.demo.started"),
		zap.String("job_id", job.ID), zap.String("job_name", job.Name), zap.Int("parameter_count", len(job.Parameters)))

	// 模拟任务执行
	select {
	case <-time.After(2 * time.Second):
		logger.Info("Demo job completed", zap.String("event", "scheduler.demo.completed"), zap.String("job_id", job.ID))
		return nil
	case <-ctx.Done():
		logger.Info("Demo job canceled", zap.String("event", "scheduler.demo.canceled"), zap.String("job_id", job.ID))
		return ctx.Err()
	}
}

// Name 返回执行器名称
func (e *DemoExecutor) Name() string {
	return "demo-executor"
}
