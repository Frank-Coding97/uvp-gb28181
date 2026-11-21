package executors

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/logcleanup"
	"uvplatform.com/uvp-gb28181/app/utils/schedulerhelper"
)

const LogCleanupExecutorName = "log-cleanup-executor"

type LogCleanupExecutor struct {
	DB  *gorm.DB
	Now func() time.Time
}

func (e *LogCleanupExecutor) Execute(ctx context.Context, job *schedulerhelper.Job) error {
	if job == nil {
		return errors.New("log cleanup job is nil")
	}
	kindValue, ok := job.Parameters["kind"].(string)
	if !ok || kindValue == "" {
		return errors.New("log cleanup job kind is missing")
	}
	kind := logcleanup.Kind(kindValue)
	release, err := logcleanup.Acquire(ctx, kind)
	if err != nil {
		return err
	}
	defer release()

	config := logcleanup.CurrentConfig()
	days, err := retentionDays(config, kind)
	if err != nil {
		return err
	}
	if !config.Configured && (kind == logcleanup.Operation || kind == logcleanup.Job) {
		// 这两类要靠「页面保存过一次清理策略」才放行（其它四类一直有清理）。
		// 跳过的原因必须能照做：任务状态仍是 SUCCESS，若摘要只写"跳过"，
		// 没人会发现日志其实一条没删 —— 线上机因此把 151 万行操作日志
		// 从装机留到磁盘告急，升级直接因容量不足失败。
		schedulerhelper.ReportSummary(ctx, fmt.Sprintf("日志类型=%s；未启用=清理策略从未保存过，本次跳过（系统设置→日志清理保存一次即启用）", kind))
		return nil
	}

	now := time.Now
	if e.Now != nil {
		now = e.Now
	}
	cutoff := now().Add(-time.Duration(days) * 24 * time.Hour)
	db := e.DB
	if db == nil {
		db = app.DB()
	}
	deleted, cleanErr := logcleanup.Clean(ctx, db, kind, cutoff, 500)
	if cleanErr != nil {
		schedulerhelper.ReportSummary(ctx, fmt.Sprintf("日志类型=%s；保留=%d天；截止=%s；已删除=%d条；错误=%s", kind, days, cutoff.Format(time.RFC3339), deleted, cleanErr))
		return fmt.Errorf("清理%s日志失败（已删除 %d 条）: %w", kind, deleted, cleanErr)
	}
	schedulerhelper.ReportSummary(ctx, fmt.Sprintf("日志类型=%s；保留=%d天；截止=%s；已删除=%d条", kind, days, cutoff.Format(time.RFC3339), deleted))
	return nil
}

func (e *LogCleanupExecutor) Name() string { return LogCleanupExecutorName }

func retentionDays(config logcleanup.Config, kind logcleanup.Kind) (int, error) {
	switch kind {
	case logcleanup.SIP:
		return config.SIPRetentionDays, nil
	case logcleanup.Operation:
		return config.OperationRetentionDays, nil
	case logcleanup.Login:
		return config.LoginRetentionDays, nil
	case logcleanup.Job:
		return config.JobRetentionDays, nil
	case logcleanup.Playback:
		return config.PlaybackRetentionDays, nil
	case logcleanup.Scheduler:
		return config.SchedulerRetentionDays, nil
	default:
		return 0, fmt.Errorf("unknown log cleanup kind %q", kind)
	}
}
