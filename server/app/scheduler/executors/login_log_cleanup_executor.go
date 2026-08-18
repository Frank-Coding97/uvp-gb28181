package executors

import (
	"context"
	"errors"
	"time"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/service"
	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"
)

const LoginLogCleanupExecutorName = "login-log-cleanup-executor"

type LoginLogCleanupExecutor struct {
	Service *service.LoginLogService
	Now     func() time.Time
}

func (e *LoginLogCleanupExecutor) Execute(ctx context.Context, _ *schedulerhelper.Job) error {
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
	_, err := loginLogs.CleanupBefore(ctx, now().Add(-180*24*time.Hour), 1000)
	return err
}

func (e *LoginLogCleanupExecutor) Name() string { return LoginLogCleanupExecutorName }
