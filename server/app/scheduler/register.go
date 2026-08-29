package scheduler

import (
	"context"
	"encoding/json"
	"time"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/scheduler/executors"
	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"

	"go.uber.org/zap"
)

// RegisterExecutors 注册所有执行器
// 在这里添加新的执行器注册
func RegisterExecutors() {
	// 注册演示执行器
	app.JobScheduler.RegisterExecutor(&executors.DemoExecutor{})
	app.JobScheduler.RegisterExecutor(&executors.SessionCleanupExecutor{})
	app.JobScheduler.RegisterExecutor(&executors.LoginLogCleanupExecutor{})
	app.JobScheduler.RegisterExecutor(&executors.RecordingPlanDispatchExecutor{})
	app.JobScheduler.RegisterExecutor(&executors.RecordingPlanHealExecutor{})
	if _, err := app.JobScheduler.AddOrUpdateJob(&schedulerhelper.Job{
		ID: "system-session-cleanup", Group: "system", Name: "登录会话终态清理",
		Description:  "每日清理撤销或自然过期超过 30 天的登录会话",
		ExecutorName: executors.SessionCleanupExecutorName, ExecutionPolicy: schedulerhelper.PolicyRepeat,
		Status: schedulerhelper.StatusEnabled, CronExpression: "0 0 3 * * *",
		BlockingPolicy: schedulerhelper.BlockDiscard, Timeout: 10 * time.Minute,
	}); err != nil && app.ZapLog != nil {
		app.ZapLog.Error("注册登录会话清理任务失败", zap.Error(err))
	}
	registerSystemJob(&schedulerhelper.Job{
		ID: "system-recording-plan-dispatch", Group: "system", Name: "录像计划调度",
		Description:  "每 5 秒领取到期通道并执行录像计划",
		ExecutorName: executors.RecordingPlanDispatchExecutorName, ExecutionPolicy: schedulerhelper.PolicyRepeat,
		Status: schedulerhelper.StatusEnabled, CronExpression: "*/5 * * * * *",
		BlockingPolicy: schedulerhelper.BlockDiscard, Timeout: 4 * time.Second,
	})
	registerSystemJob(&schedulerhelper.Job{
		ID: "system-recording-plan-heal", Group: "system", Name: "录像计划自愈",
		Description:  "每分钟重新请求所有通道对账，修复掉线和漏 Hook",
		ExecutorName: executors.RecordingPlanHealExecutorName, ExecutionPolicy: schedulerhelper.PolicyRepeat,
		Status: schedulerhelper.StatusEnabled, CronExpression: "0 * * * * *",
		BlockingPolicy: schedulerhelper.BlockDiscard, Timeout: 50 * time.Second,
	})
	if _, err := app.JobScheduler.AddOrUpdateJob(&schedulerhelper.Job{
		ID: "system-login-log-cleanup", Group: "system", Name: "登录日志清理",
		Description:  "每日分批清理 180 天前的登录日志",
		ExecutorName: executors.LoginLogCleanupExecutorName, ExecutionPolicy: schedulerhelper.PolicyRepeat,
		Status: schedulerhelper.StatusEnabled, CronExpression: "0 0 3 * * *",
		BlockingPolicy: schedulerhelper.BlockDiscard, Timeout: 10 * time.Minute,
	}); err != nil && app.ZapLog != nil {
		app.ZapLog.Error("注册登录日志清理任务失败", zap.Error(err))
	}

	// 在这里添加更多执行器...
	// app.JobScheduler.RegisterExecutor(&executors.YourExecutor{})
}

func registerSystemJob(job *schedulerhelper.Job) {
	if _, err := app.JobScheduler.AddOrUpdateJob(job); err != nil && app.ZapLog != nil {
		app.ZapLog.Error("注册系统任务失败", zap.String("jobID", job.ID), zap.Error(err))
	}
}

// LoadJobsFromDB 从数据库加载启用的任务并注册到调度器
func LoadJobsFromDB() {
	ctx := context.Background()

	// 查询所有启用的任务 (status=1)
	var jobsList models.SysJobsList
	err := app.DB().WithContext(ctx).Model(&models.SysJobs{}).Where("status = ?", 1).Find(&jobsList).Error
	if err != nil {
		app.ZapLog.Error("从数据库加载任务失败", zap.Error(err))
		return
	}

	app.ZapLog.Info("从数据库加载启用的任务", zap.Int("count", len(jobsList)))

	// 遍历任务列表，添加到调度器
	for _, job := range jobsList {
		// 解析任务参数JSON字符串
		var parameters map[string]interface{}
		if job.Parameters != "" {
			if err := json.Unmarshal([]byte(job.Parameters), &parameters); err != nil {
				app.ZapLog.Error("解析任务参数失败",
					zap.String("jobID", job.Id),
					zap.String("name", job.Name),
					zap.Error(err))
				continue
			}
		}

		// 构建调度器Job对象
		schedulerJob := &schedulerhelper.Job{
			ID:              job.Id,
			Group:           job.Group,
			Name:            job.Name,
			Description:     job.Description,
			ExecutorName:    job.ExecutorName,
			ExecutionPolicy: schedulerhelper.ExecutionPolicy(job.ExecutionPolicy),
			Status:          schedulerhelper.JobStatus(job.Status),
			CronExpression:  job.CronExpression,
			Parameters:      parameters,
			BlockingPolicy:  schedulerhelper.BlockingPolicy(job.BlockingPolicy),
			Timeout:         time.Duration(job.Timeout),
			MaxRetry:        job.MaxRetry,
			RetryInterval:   time.Duration(job.RetryInterval),
			ParallelNum:     job.ParallelNum,
		}

		// 添加到调度器
		_, err := app.JobScheduler.AddOrUpdateJob(schedulerJob)
		if err != nil {
			app.ZapLog.Error("添加任务到调度器失败",
				zap.String("jobID", job.Id),
				zap.String("name", job.Name),
				zap.Error(err))
			continue
		}

		app.ZapLog.Info("成功加载任务到调度器",
			zap.String("jobID", job.Id),
			zap.String("name", job.Name),
			zap.String("cron", job.CronExpression))
	}

	// 启动任务结果处理器，将任务执行结果保存到数据库
	StartResultHandler()
}
