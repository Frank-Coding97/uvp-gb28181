package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/models"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"
)

func TestLoginLogQueryFiltersOrdersAndHidesUserAgentFromList(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SysLoginLog{}))
	created := time.Date(2026, 8, 18, 15, 0, 0, 0, time.Local)
	require.NoError(t, db.Create([]models.SysLoginLog{
		{Username: "alice", Result: LoginResultFailure, FailureReason: LoginFailurePasswordIncorrect, IP: "10.0.0.1", Location: "内网", UserAgent: "secret-ua-1", Browser: "Chrome", OS: "macOS", CreatedAt: created},
		{Username: "alice", Result: LoginResultFailure, FailureReason: LoginFailurePasswordIncorrect, IP: "10.0.0.2", Location: "内网", UserAgent: "secret-ua-2", Browser: "Safari", OS: "macOS", CreatedAt: created},
		{Username: "bob", Result: LoginResultSuccess, IP: "10.0.0.3", Location: "内网", UserAgent: "secret-ua-3", Browser: "Firefox", OS: "Linux", CreatedAt: created.Add(time.Minute)},
	}).Error)
	svc := NewLoginLogQueryService(db)
	items, total, err := svc.List(context.Background(), LoginLogFilter{
		PageNum: 1, PageSize: 20, Username: "alice", Result: LoginResultFailure,
		FailureReason: LoginFailurePasswordIncorrect,
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, items, 2)
	require.Greater(t, items[0].ID, items[1].ID)
	encoded, err := json.Marshal(items[0])
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "userAgent")

	detail, err := svc.Detail(context.Background(), items[0].ID)
	require.NoError(t, err)
	require.Equal(t, "secret-ua-2", detail.UserAgent)
	_, err = svc.Detail(context.Background(), 999)
	require.ErrorIs(t, err, ErrLoginLogNotFound)
}

func TestLoginLogQueryRejectsUnsafePagination(t *testing.T) {
	for _, filter := range []LoginLogFilter{{PageNum: -1, PageSize: 20}, {PageNum: 1, PageSize: -1}, {PageNum: 1, PageSize: 101}} {
		err := filter.Validate()
		require.Error(t, err)
	}
}
