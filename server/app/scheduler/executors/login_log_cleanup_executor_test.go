package executors

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"uvplatform.cn/uvp-gb28181/app/models"
	"uvplatform.cn/uvp-gb28181/app/service"
	sqlite "uvplatform.cn/uvp-gb28181/internal/sqlitedialect"
)

func TestLoginLogCleanupExecutorUsesStrict180DayCutoff(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SysLoginLog{}))
	now := time.Date(2026, 8, 18, 3, 0, 0, 0, time.UTC)
	cutoff := now.Add(-180 * 24 * time.Hour)
	rows := []models.SysLoginLog{
		{Username: "old", Result: service.LoginResultFailure, IP: "10.0.0.1", Location: "内网", UserAgent: "ua", Browser: "x", OS: "x", CreatedAt: cutoff.Add(-time.Second)},
		{Username: "boundary", Result: service.LoginResultFailure, IP: "10.0.0.1", Location: "内网", UserAgent: "ua", Browser: "x", OS: "x", CreatedAt: cutoff},
	}
	require.NoError(t, db.Create(&rows).Error)
	executor := &LoginLogCleanupExecutor{Service: service.NewLoginLogService(db), Now: func() time.Time { return now }}
	require.NoError(t, executor.Execute(context.Background(), nil))
	var remaining []models.SysLoginLog
	require.NoError(t, db.Find(&remaining).Error)
	require.Len(t, remaining, 1)
	require.Equal(t, "boundary", remaining[0].Username)
}
