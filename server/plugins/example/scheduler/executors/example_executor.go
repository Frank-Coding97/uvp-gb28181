package executors

import (
	"context"
	"time"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"

	"go.uber.org/zap"
)

// ExampleExecutor 示例执行器，用于演示插件如何注册任务执行器
type ExampleExecutor struct{}

// Execute 执行任务
func (e *ExampleExecutor) Execute(ctx context.Context, job *schedulerhelper.Job) error {
	app.Log(ctx).Named("scheduler.example").Info("ExampleExecutor 开始执行任务", zap.String("event", "scheduler.example.started"),
		zap.String("executor", e.Name()),
		zap.String("job_id", job.ID),
		zap.String("job_name", job.Name),
		zap.Int("parameter_count", len(job.Parameters)))

	// 模拟任务执行
	select {
	case <-time.After(1 * time.Second):
		app.Log(ctx).Named("scheduler.example").Info("ExampleExecutor 任务执行完成", zap.String("event", "scheduler.example.completed"),
			zap.String("job_id", job.ID),
			zap.String("job_name", job.Name))
		return nil
	case <-ctx.Done():
		app.Log(ctx).Named("scheduler.example").Warn("ExampleExecutor 任务被取消", zap.String("event", "scheduler.example.canceled"),
			zap.String("job_id", job.ID),
			zap.Error(ctx.Err()))
		return ctx.Err()
	}
}

// Name 返回执行器名称
func (e *ExampleExecutor) Name() string {
	return "example-executor"
}
