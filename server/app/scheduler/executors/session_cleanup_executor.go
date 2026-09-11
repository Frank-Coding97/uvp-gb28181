package executors

import (
	"context"
	"errors"
	"time"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/service"
	"uvplatform.cn/uvp-gb28181/app/utils/schedulerhelper"
)

const SessionCleanupExecutorName = "session-cleanup-executor"

type SessionCleanupExecutor struct {
	Service *service.AuthSessionService
	Now     func() time.Time
}

func (e *SessionCleanupExecutor) Execute(ctx context.Context, _ *schedulerhelper.Job) error {
	sessions := e.Service
	if sessions == nil {
		sessions, _ = app.SessionValidator.(*service.AuthSessionService)
	}
	if sessions == nil {
		return errors.New("authentication session service unavailable")
	}
	now := time.Now
	if e.Now != nil {
		now = e.Now
	}
	_, err := sessions.CleanupTerminal(ctx, now().Add(-30*24*time.Hour))
	return err
}

func (e *SessionCleanupExecutor) Name() string { return SessionCleanupExecutorName }
