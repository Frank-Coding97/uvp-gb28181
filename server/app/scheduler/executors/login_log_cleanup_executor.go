package executors

import (
	"context"
	"errors"
	"fmt"
	"time"

	"uvplatform.com/uvp-gb28181/app/global/app"
	"uvplatform.com/uvp-gb28181/app/logcleanup"
	"uvplatform.com/uvp-gb28181/app/service"
	"uvplatform.com/uvp-gb28181/app/utils/schedulerhelper"
)

const LoginLogCleanupExecutorName = "login-log-cleanup-executor"

type LoginLogCleanupExecutor struct {
	Service *service.LoginLogService
	Now     func() time.Time
}

func (e *LoginLogCleanupExecutor) Execute(ctx context.Context, _ *schedulerhelper.Job) error {
	release, err := logcleanup.Acquire(ctx, logcleanup.Login)
	if err != nil {
		return err
	}
	defer release()
	loginLogs := e.Service
	if loginLogs == nil {
		loginLogs, _ = app.LoginLogRecorder.(*service.LoginLogService)
	}
	if loginLogs == nil {
		return errors.New("login log service unavailable")
	}
	now := time.Now
	if e.Now != nil {
		now = e.Now
	}
	days := logcleanup.CurrentConfig().LoginRetentionDays
	cutoff := now().Add(-time.Duration(days) * 24 * time.Hour)
	deleted, err := loginLogs.CleanupBefore(ctx, cutoff, 500)
	schedulerhelper.ReportSummary(ctx, fmt.Sprintf("日志类型=login；保留=%d天；截止=%s；已删除=%d条", days, cutoff.Format(time.RFC3339), deleted))
	return err
}

func (e *LoginLogCleanupExecutor) Name() string { return LoginLogCleanupExecutorName }
