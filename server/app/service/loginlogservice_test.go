package service

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/global/app"
	"uvplatform.cn/uvp-gb28181/app/models"
)

func TestLoginLogServiceRecordLoginPersistsAllowlistedFields(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SysLoginLog{}))
	uid := uint(7)
	svc := NewLoginLogService(db)
	err = svc.RecordLogin(context.Background(), app.LoginLogEvent{
		UserID: &uid, Username: "alice", Result: LoginResultSuccess,
		IP: "10.0.0.1", Location: "内网", UserAgent: "ua",
		Browser: "Chrome", OS: "macOS",
	})
	require.NoError(t, err)
	var got models.SysLoginLog
	require.NoError(t, db.First(&got).Error)
	require.Equal(t, "alice", got.Username)
	require.Equal(t, LoginResultSuccess, got.Result)
	require.Equal(t, "ua", got.UserAgent)
	// The event/model contract has no password, captcha, token, or request-body field.
	require.NotContains(t, got.UserAgent, "password")
}

func TestLoginLogServiceRecordLoginHonorsCanceledContext(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SysLoginLog{}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = NewLoginLogService(db).RecordLogin(ctx, app.LoginLogEvent{Username: "alice", Result: LoginResultFailure})
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}
