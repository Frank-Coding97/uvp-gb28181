package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/scheduler/executors"
	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

	// 在这里添加更多执行器...
	// app.JobScheduler.RegisterExecutor(&executors.YourExecutor{})
}

func systemJobDefinitions() []*schedulerhelper.Job {
	return []*schedulerhelper.Job{
		{
			ID: "system-session-cleanup", Group: "system", Name: "登录会话终态清理",
			Description:  "每日清理撤销或自然过期超过 30 天的登录会话",
			ExecutorName: executors.SessionCleanupExecutorName, ExecutionPolicy: schedulerhelper.PolicyRepeat,
			Status: schedulerhelper.StatusEnabled, CronExpression: "0 0 3 * * *",
			BlockingPolicy: schedulerhelper.BlockDiscard, Timeout: 10 * time.Minute,
		},
		{
			ID: "system-recording-plan-dispatch", Group: "system", Name: "录像计划调度",
			Description:  "每 5 秒领取到期通道并执行录像计划",
			ExecutorName: executors.RecordingPlanDispatchExecutorName, ExecutionPolicy: schedulerhelper.PolicyRepeat,
			Status: schedulerhelper.StatusEnabled, CronExpression: "*/5 * * * * *",
			BlockingPolicy: schedulerhelper.BlockDiscard, Timeout: 4 * time.Second,
		},
		{
			ID: "system-recording-plan-heal", Group: "system", Name: "录像计划自愈",
			Description:  "每分钟重新请求所有通道对账，修复掉线和漏 Hook",
			ExecutorName: executors.RecordingPlanHealExecutorName, ExecutionPolicy: schedulerhelper.PolicyRepeat,
			Status: schedulerhelper.StatusEnabled, CronExpression: "0 * * * * *",
			BlockingPolicy: schedulerhelper.BlockDiscard, Timeout: 50 * time.Second,
		},
		{
			ID: "system-login-log-cleanup", Group: "system", Name: "登录日志清理",
			Description:  "每日分批清理 180 天前的登录日志",
			ExecutorName: executors.LoginLogCleanupExecutorName, ExecutionPolicy: schedulerhelper.PolicyRepeat,
			Status: schedulerhelper.StatusEnabled, CronExpression: "0 0 3 * * *",
			BlockingPolicy: schedulerhelper.BlockDiscard, Timeout: 10 * time.Minute,
		},
	}
}

// RegisterSystemJobs 将内置任务先幂等写入 sys_jobs，再注册到调度器。
// 内置定义由代码维护，但保留数据库中的启停状态，避免应用重启后重新启用已禁用任务。
func RegisterSystemJobs(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("注册系统任务失败: 数据库连接为空")
	}
	if app.JobScheduler == nil {
		return fmt.Errorf("注册系统任务失败: 调度器未初始化")
	}

	ctx := context.Background()
	for _, job := range systemJobDefinitions() {
		status, err := persistSystemJob(ctx, db, job)
		if err != nil {
			return fmt.Errorf("持久化系统任务 %s 失败: %w", job.ID, err)
		}
		job.Status = schedulerhelper.JobStatus(status)
		if _, err := app.JobScheduler.AddOrUpdateJob(job); err != nil {
			return fmt.Errorf("注册系统任务 %s 失败: %w", job.ID, err)
		}
	}
	return nil
}

func persistSystemJob(ctx context.Context, db *gorm.DB, job *schedulerhelper.Job) (int, error) {
	parameters := "{}"
	if job.Parameters != nil {
		data, err := json.Marshal(job.Parameters)
		if err != nil {
			return 0, fmt.Errorf("序列化任务参数失败: %w", err)
		}
		parameters = string(data)
	}

	now := time.Now()
	record := &models.SysJobs{
		Id:              job.ID,
		Group:           job.Group,
		Name:            job.Name,
		Description:     job.Description,
		ExecutorName:    job.ExecutorName,
		ExecutionPolicy: int(job.ExecutionPolicy),
		Status:          int(job.Status),
		CronExpression:  job.CronExpression,
		Parameters:      parameters,
		BlockingPolicy:  int(job.BlockingPolicy),
		Timeout:         int64(job.Timeout),
		MaxRetry:        job.MaxRetry,
		RetryInterval:   int64(job.RetryInterval),
		ParallelNum:     job.ParallelNum,
		CreatedAt:       &now,
		UpdatedAt:       &now,
	}
	definitionColumns := []string{
		"group", "name", "description", "executor_name", "execution_policy",
		"cron_expression", "parameters", "blocking_policy", "timeout", "max_retry",
		"retry_interval", "parallel_num", "updated_at", "deleted_at",
	}
	if err := db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns(definitionColumns),
	}).Create(record).Error; err != nil {
		return 0, err
	}

	var persisted models.SysJobs
	if err := db.WithContext(ctx).Select("status").First(&persisted, "id = ?", job.ID).Error; err != nil {
		return 0, err
	}
	return persisted.Status, nil
}

// LoadJobsFromDB 从数据库加载启用的任务并注册到调度器
func LoadJobsFromDB() {
	// Attach the result consumer before any database load or job admission.
	// This also keeps later manual or API-triggered results drainable when the
	// initial query fails.
	StartResultHandler()

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
}
