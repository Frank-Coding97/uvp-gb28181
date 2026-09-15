package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/utils/logging"
)

type loggingLoginRecorder struct {
	ctx context.Context
	err error
}

func (r *loggingLoginRecorder) RecordLogin(ctx context.Context, _ app.LoginLogEvent) error {
	r.ctx = ctx
	return r.err
}

func TestLoggingAuditContextLoginFailureUsesScopedSafeEvent(t *testing.T) {
	oldLog := app.ZapLog
	t.Cleanup(func() { app.ZapLog = oldLog })
	core, observed := observer.New(zap.InfoLevel)
	root := zap.New(core)
	app.ZapLog = root
	ctx := logging.WithContext(context.Background(), logging.WithIdentity(root, zap.String("request_id", "login-rid")))
	recorder := &loggingLoginRecorder{err: errors.New("credential must not be logged")}

	RecordLoginAttempt(ctx, recorder, app.LoginLogEvent{Username: "alice", Result: LoginResultFailure})

	require.NotNil(t, recorder.ctx)
	deadline, ok := recorder.ctx.Deadline()
	require.True(t, ok)
	require.WithinDuration(t, time.Now().Add(loginLogWriteTimeout), deadline, 100*time.Millisecond)
	var failure *observer.LoggedEntry
	for _, entry := range observed.All() {
		if entry.Message == "登录日志记录失败" {
			copy := entry
			failure = &copy
			break
		}
	}
	require.NotNil(t, failure)
	require.Equal(t, "auth.login_audit.persist_failed", failure.ContextMap()["event"])
	require.Equal(t, "login-rid", failure.ContextMap()["request_id"])
}

func TestLoggingAuditContextLoginPanicIsIsolatedAndTyped(t *testing.T) {
	oldLog := app.ZapLog
	t.Cleanup(func() { app.ZapLog = oldLog })
	core, observed := observer.New(zap.InfoLevel)
	root := zap.New(core)
	app.ZapLog = root
	ctx := logging.WithContext(context.Background(), logging.WithIdentity(root, zap.String("request_id", "login-panic-rid")))
	recorder := loginAuditPanicRecorder{}

	RecordLoginAttempt(ctx, recorder, app.LoginLogEvent{Username: "alice", Result: LoginResultFailure})

	var panicEntry *observer.LoggedEntry
	for _, entry := range observed.All() {
		if entry.Message == "登录日志记录异常" {
			copy := entry
			panicEntry = &copy
			break
		}
	}
	require.NotNil(t, panicEntry)
	require.Equal(t, "auth.login_audit.panic", panicEntry.ContextMap()["event"])
	require.Equal(t, "login-panic-rid", panicEntry.ContextMap()["request_id"])
	require.NotEmpty(t, panicEntry.ContextMap()["panic_type"])
}

type loginAuditPanicRecorder struct{}

func (loginAuditPanicRecorder) RecordLogin(context.Context, app.LoginLogEvent) error {
	panic("login audit fixture panic")
}
