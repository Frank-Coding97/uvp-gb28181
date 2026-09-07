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

func TestSessionCleanupExecutorDeletesOnlyTerminalRowsOlderThanThirtyDays(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SysUserSession{}))
	now := time.Date(2026, 8, 17, 16, 0, 0, 0, time.UTC)
	old := now.Add(-31 * 24 * time.Hour)
	row := models.SysUserSession{
		SID: "old", UserID: 7, ClientIP: "10.0.0.1", LoginLocation: "内网", UserAgent: "test",
		Browser: "test", OS: "test", LoginAt: old, LastActiveAt: old, SessionExpiresAt: old,
	}
	require.NoError(t, db.Create(&row).Error)
	executor := &SessionCleanupExecutor{Service: service.NewAuthSessionService(db), Now: func() time.Time { return now }}
	require.NoError(t, executor.Execute(context.Background(), nil))
	var count int64
	require.NoError(t, db.Model(&models.SysUserSession{}).Count(&count).Error)
	require.Zero(t, count)
}
